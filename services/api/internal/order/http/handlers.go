package ohttp

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/sse"

	"github.com/takalawang/corporate-catering-system/services/api/internal/identity"
	idhttp "github.com/takalawang/corporate-catering-system/services/api/internal/identity/http"
	"github.com/takalawang/corporate-catering-system/services/api/internal/order"
	"github.com/takalawang/corporate-catering-system/services/api/internal/quota"
)

// API exposes employee-facing order endpoints: place / list / get / cancel.
// All endpoints require the employee role.
type API struct {
	Svc *order.Service
	// Board fans live order events to the merchant prep board over SSE. It is
	// optional: when nil (NATS not configured) the SSE endpoint stays open but
	// emits only keep-alive pings.
	Board *order.BoardHub
	// MenuHub broadcasts a "menu changed" signal to employee menu views so
	// they refetch when stock moves. Optional, same as Board.
	MenuHub *order.MenuHub
}

type orderItemDTO struct {
	ID             string `json:"id"`
	MenuItemID     string `json:"menu_item_id"`
	Name           string `json:"name"`
	Qty            int    `json:"qty"`
	UnitPriceMinor int64  `json:"unit_price_minor"`
}

type orderDTO struct {
	ID              string         `json:"id"`
	OrderNumber     int64          `json:"order_number"`
	VendorID        string         `json:"vendor_id"`
	Plant           string         `json:"plant"`
	SupplyDate      string         `json:"supply_date"`
	Status          string         `json:"status"`
	TotalPriceMinor int64          `json:"total_price_minor"`
	Notes           string         `json:"notes"`
	PlacedAt        *string        `json:"placed_at,omitempty"`
	CutoffAt        string         `json:"cutoff_at"`
	CancelledAt     *string        `json:"cancelled_at,omitempty"`
	Items           []orderItemDTO `json:"items"`
}

func toDTO(o *order.Order) orderDTO {
	d := orderDTO{
		ID:              o.ID,
		OrderNumber:     o.OrderNumber,
		VendorID:        o.VendorID,
		Plant:           o.Plant,
		SupplyDate:      o.SupplyDate.Format("2006-01-02"),
		Status:          string(o.Status),
		TotalPriceMinor: o.TotalPriceMinor,
		Notes:           o.Notes,
		CutoffAt:        o.CutoffAt.UTC().Format(time.RFC3339),
		Items:           make([]orderItemDTO, 0, len(o.Items)),
	}
	if o.PlacedAt != nil {
		s := o.PlacedAt.UTC().Format(time.RFC3339)
		d.PlacedAt = &s
	}
	if o.CancelledAt != nil {
		s := o.CancelledAt.UTC().Format(time.RFC3339)
		d.CancelledAt = &s
	}
	for _, it := range o.Items {
		d.Items = append(d.Items, orderItemDTO{
			ID:             it.ID,
			MenuItemID:     it.MenuItemID,
			Name:           it.Name,
			Qty:            it.Qty,
			UnitPriceMinor: it.UnitPriceMinor,
		})
	}
	return d
}

type placeOrderInput struct {
	Body struct {
		Plant      string `json:"plant"`
		SupplyDate string `json:"supply_date"` // YYYY-MM-DD
		Notes      string `json:"notes,omitempty" maxLength:"500" doc:"Free-text special requirements shown on the merchant prep board"`
		Items      []struct {
			MenuItemID string `json:"menu_item_id" format:"uuid"`
			Qty        int    `json:"qty" minimum:"1"`
		} `json:"items"`
	}
}

type placeOrderOutput struct {
	Body struct {
		Order orderDTO `json:"order"`
	}
}

type listOrdersOutput struct {
	Body struct {
		Items []orderDTO `json:"items"`
	}
}

type orderIDInput struct {
	ID string `path:"id" format:"uuid"`
}

type modifyOrderInput struct {
	ID   string `path:"id" format:"uuid"`
	Body struct {
		Notes string `json:"notes,omitempty" maxLength:"500" doc:"Free-text special requirements shown on the merchant prep board"`
		Items []struct {
			MenuItemID string `json:"menu_item_id" format:"uuid"`
			Qty        int    `json:"qty" minimum:"1"`
		} `json:"items"`
	}
}

type orderOutput struct {
	Body struct {
		Order orderDTO `json:"order"`
	}
}

type markReadyInput struct {
	Body struct {
		OrderIDs []string `json:"order_ids" minItems:"1"`
	}
}

type listMerchantOrdersInput struct {
	Date   string `query:"date"`
	Status string `query:"status" enum:"placed,cutoff,ready,picked_up,no_show,cancelled,"`
	Plant  string `query:"plant"`
}

type merchantOrderDTO struct {
	ID              string         `json:"id"`
	OrderNumber     int64          `json:"order_number"`
	Plant           string         `json:"plant"`
	Status          string         `json:"status"`
	TotalPriceMinor int64          `json:"total_price_minor"`
	Notes           string         `json:"notes"`
	PlacedAt        *string        `json:"placed_at,omitempty"`
	ReadyAt         *string        `json:"ready_at,omitempty"`
	PickedUpAt      *string        `json:"picked_up_at,omitempty"`
	Items           []orderItemDTO `json:"items"`
}

type listMerchantOrdersOutput struct {
	Body struct {
		Date  string             `json:"date"`
		Items []merchantOrderDTO `json:"items"`
	}
}

func toMerchantDTO(o *order.Order) merchantOrderDTO {
	d := merchantOrderDTO{
		ID:              o.ID,
		OrderNumber:     o.OrderNumber,
		Plant:           o.Plant,
		Status:          string(o.Status),
		TotalPriceMinor: o.TotalPriceMinor,
		Notes:           o.Notes,
		Items:           make([]orderItemDTO, 0, len(o.Items)),
	}
	if o.PlacedAt != nil {
		s := o.PlacedAt.UTC().Format(time.RFC3339)
		d.PlacedAt = &s
	}
	if o.ReadyAt != nil {
		s := o.ReadyAt.UTC().Format(time.RFC3339)
		d.ReadyAt = &s
	}
	if o.PickedUpAt != nil {
		s := o.PickedUpAt.UTC().Format(time.RFC3339)
		d.PickedUpAt = &s
	}
	for _, it := range o.Items {
		d.Items = append(d.Items, orderItemDTO{
			ID:             it.ID,
			MenuItemID:     it.MenuItemID,
			Name:           it.Name,
			Qty:            it.Qty,
			UnitPriceMinor: it.UnitPriceMinor,
		})
	}
	return d
}

type prepSheetItemDTO struct {
	MenuItemID string `json:"menu_item_id"`
	Name       string `json:"name"`
	Qty        int    `json:"qty"`
}

type prepSheetOrderDTO struct {
	OrderID         string             `json:"order_id"`
	OrderNumber     int64              `json:"order_number"`
	TotalPriceMinor int64              `json:"total_price_minor"`
	Notes           string             `json:"notes"`
	Items           []prepSheetItemDTO `json:"items"`
}

type prepSheetPlantDTO struct {
	Plant        string              `json:"plant"`
	OrderCount   int                 `json:"order_count"`
	PortionCount int                 `json:"portion_count"`
	Items        []prepSheetItemDTO  `json:"items"`
	Orders       []prepSheetOrderDTO `json:"orders"`
}

type prepSheetInput struct {
	Date string `query:"date" doc:"YYYY-MM-DD; defaults to today UTC"`
}

type prepSheetOutput struct {
	Body struct {
		Date          string              `json:"date"`
		TotalOrders   int                 `json:"total_orders"`
		TotalPortions int                 `json:"total_portions"`
		Plants        []prepSheetPlantDTO `json:"plants"`
	}
}

func prepItemDTOs(items []order.PrepSheetItem) []prepSheetItemDTO {
	out := make([]prepSheetItemDTO, 0, len(items))
	for _, it := range items {
		out = append(out, prepSheetItemDTO{MenuItemID: it.MenuItemID, Name: it.Name, Qty: it.Qty})
	}
	return out
}

func (a *API) Register(api huma.API) {
	huma.Register(api, huma.Operation{
		OperationID:   "placeOrder",
		Method:        http.MethodPost,
		Path:          "/api/employee/orders",
		Summary:       "Place a new order",
		Tags:          []string{"employee", "order"},
		Security:      []map[string][]string{{"bearer": {}}},
		DefaultStatus: http.StatusCreated,
	}, a.place)

	huma.Register(api, huma.Operation{
		OperationID: "listMyOrders",
		Method:      http.MethodGet,
		Path:        "/api/employee/orders",
		Summary:     "List my orders",
		Tags:        []string{"employee", "order"},
		Security:    []map[string][]string{{"bearer": {}}},
	}, a.list)

	huma.Register(api, huma.Operation{
		OperationID: "getMyOrder",
		Method:      http.MethodGet,
		Path:        "/api/employee/orders/{id}",
		Summary:     "Get an order by ID (owner only)",
		Tags:        []string{"employee", "order"},
		Security:    []map[string][]string{{"bearer": {}}},
	}, a.get)

	huma.Register(api, huma.Operation{
		OperationID: "modifyMyOrder",
		Method:      http.MethodPut,
		Path:        "/api/employee/orders/{id}",
		Summary:     "Modify my order's items before cutoff (owner only)",
		Tags:        []string{"employee", "order"},
		Security:    []map[string][]string{{"bearer": {}}},
	}, a.modify)

	huma.Register(api, huma.Operation{
		OperationID:   "cancelMyOrder",
		Method:        http.MethodPost,
		Path:          "/api/employee/orders/{id}/cancel",
		Summary:       "Cancel my order",
		Tags:          []string{"employee", "order"},
		Security:      []map[string][]string{{"bearer": {}}},
		DefaultStatus: http.StatusNoContent,
	}, a.cancel)

	huma.Register(api, huma.Operation{
		OperationID:   "pickupOrder",
		Method:        http.MethodPost,
		Path:          "/api/employee/orders/{id}/pickup",
		Summary:       "Self-service pickup: scan the meal QR to mark your order picked up",
		Tags:          []string{"employee", "order"},
		Security:      []map[string][]string{{"bearer": {}}},
		DefaultStatus: http.StatusNoContent,
	}, a.pickup)

	huma.Register(api, huma.Operation{
		OperationID:   "markOrdersReady",
		Method:        http.MethodPost,
		Path:          "/api/merchant/orders/mark-ready",
		Summary:       "Mark one or more orders as ready for pickup",
		Tags:          []string{"merchant", "order"},
		Security:      []map[string][]string{{"bearer": {}}},
		DefaultStatus: http.StatusNoContent,
	}, a.markReady)

	huma.Register(api, huma.Operation{
		OperationID: "listMerchantOrders",
		Method:      http.MethodGet,
		Path:        "/api/merchant/orders",
		Summary:     "List orders for the vendor on a given date",
		Tags:        []string{"merchant", "order"},
		Security:    []map[string][]string{{"bearer": {}}},
	}, a.listMerchantOrders)

	huma.Register(api, huma.Operation{
		OperationID: "merchantPrepSheet",
		Method:      http.MethodGet,
		Path:        "/api/merchant/prep-sheet",
		Summary:     "Prep & delivery output for a day: plant breakdown, labels, baskets",
		Tags:        []string{"merchant", "order"},
		Security:    []map[string][]string{{"bearer": {}}},
	}, a.prepSheet)

	sse.Register(api, huma.Operation{
		OperationID: "streamMerchantOrderEvents",
		Method:      http.MethodGet,
		Path:        "/api/merchant/orders/events",
		Summary:     "Live order events for the merchant prep board (Server-Sent Events)",
		Tags:        []string{"merchant", "order"},
		Security:    []map[string][]string{{"bearer": {}}},
	}, map[string]any{
		"message": order.BoardEvent{},
	}, a.streamMerchantOrderEvents)

	sse.Register(api, huma.Operation{
		OperationID: "streamEmployeeMenuEvents",
		Method:      http.MethodGet,
		Path:        "/api/employee/menu/events",
		Summary:     "Live menu-changed signal so the employee menu refetches stock (SSE)",
		Tags:        []string{"employee", "menu"},
		Security:    []map[string][]string{{"bearer": {}}},
	}, map[string]any{
		"message": order.BoardEvent{},
	}, a.streamEmployeeMenuEvents)
}

func (a *API) requireEmployee(ctx context.Context) (*identity.User, error) {
	return idhttp.RequireEmployee(ctx)
}

func (a *API) requireVendor(ctx context.Context) (*identity.User, string, error) {
	return idhttp.RequireVendor(ctx)
}

func (a *API) place(ctx context.Context, in *placeOrderInput) (*placeOrderOutput, error) {
	u, err := a.requireEmployee(ctx)
	if err != nil {
		return nil, err
	}
	if len(in.Body.Items) == 0 {
		return nil, huma.Error400BadRequest("items required")
	}
	day, err := time.Parse("2006-01-02", in.Body.SupplyDate)
	if err != nil {
		return nil, huma.Error400BadRequest("invalid supply_date (YYYY-MM-DD)")
	}
	items := make([]order.PlaceItem, 0, len(in.Body.Items))
	for _, it := range in.Body.Items {
		items = append(items, order.PlaceItem{MenuItemID: it.MenuItemID, Qty: it.Qty})
	}
	o, err := a.Svc.Place(ctx, order.PlaceOrderInput{
		UserID:     u.ID,
		Plant:      in.Body.Plant,
		SupplyDate: day,
		Items:      items,
		Notes:      in.Body.Notes,
	})
	if err != nil {
		return nil, mapErr(err)
	}
	var resp placeOrderOutput
	resp.Body.Order = toDTO(o)
	return &resp, nil
}

func (a *API) list(ctx context.Context, _ *struct{}) (*listOrdersOutput, error) {
	u, err := a.requireEmployee(ctx)
	if err != nil {
		return nil, err
	}
	orders, err := a.Svc.ListByUser(ctx, u.ID)
	if err != nil {
		return nil, mapErr(err)
	}
	var resp listOrdersOutput
	resp.Body.Items = make([]orderDTO, 0, len(orders))
	for _, o := range orders {
		resp.Body.Items = append(resp.Body.Items, toDTO(o))
	}
	return &resp, nil
}

func (a *API) get(ctx context.Context, in *orderIDInput) (*orderOutput, error) {
	u, err := a.requireEmployee(ctx)
	if err != nil {
		return nil, err
	}
	o, err := a.Svc.Get(ctx, in.ID, u.ID)
	if err != nil {
		return nil, mapErr(err)
	}
	var resp orderOutput
	resp.Body.Order = toDTO(o)
	return &resp, nil
}

func (a *API) modify(ctx context.Context, in *modifyOrderInput) (*orderOutput, error) {
	u, err := a.requireEmployee(ctx)
	if err != nil {
		return nil, err
	}
	if len(in.Body.Items) == 0 {
		return nil, huma.Error400BadRequest("items required")
	}
	items := make([]order.PlaceItem, 0, len(in.Body.Items))
	for _, it := range in.Body.Items {
		items = append(items, order.PlaceItem{MenuItemID: it.MenuItemID, Qty: it.Qty})
	}
	o, err := a.Svc.Modify(ctx, order.ModifyOrderInput{
		OrderID: in.ID,
		UserID:  u.ID,
		Items:   items,
		Notes:   in.Body.Notes,
	})
	if err != nil {
		return nil, mapErr(err)
	}
	var resp orderOutput
	resp.Body.Order = toDTO(o)
	return &resp, nil
}

func (a *API) cancel(ctx context.Context, in *orderIDInput) (*struct{}, error) {
	u, err := a.requireEmployee(ctx)
	if err != nil {
		return nil, err
	}
	if err := a.Svc.Cancel(ctx, in.ID, u.ID); err != nil {
		return nil, mapErr(err)
	}
	return &struct{}{}, nil
}

func (a *API) pickup(ctx context.Context, in *orderIDInput) (*struct{}, error) {
	u, err := a.requireEmployee(ctx)
	if err != nil {
		return nil, err
	}
	if err := a.Svc.Pickup(ctx, in.ID, u.ID); err != nil {
		return nil, mapErr(err)
	}
	return &struct{}{}, nil
}

func (a *API) markReady(ctx context.Context, in *markReadyInput) (*struct{}, error) {
	u, vendorID, err := a.requireVendor(ctx)
	if err != nil {
		return nil, err
	}
	if err := a.Svc.MarkReady(ctx, vendorID, in.Body.OrderIDs, u.ID); err != nil {
		return nil, mapErr(err)
	}
	return &struct{}{}, nil
}

func (a *API) listMerchantOrders(ctx context.Context, in *listMerchantOrdersInput) (*listMerchantOrdersOutput, error) {
	_, vendorID, err := a.requireVendor(ctx)
	if err != nil {
		return nil, err
	}
	day := time.Now().UTC().Truncate(24 * time.Hour)
	if in.Date != "" {
		d, perr := time.Parse("2006-01-02", in.Date)
		if perr != nil {
			return nil, huma.Error400BadRequest("invalid date")
		}
		day = d.UTC()
	}
	var statuses []order.Status
	if in.Status != "" {
		statuses = []order.Status{order.Status(in.Status)}
	}
	orders, err := a.Svc.ListByVendorDay(ctx, vendorID, day, statuses)
	if err != nil {
		return nil, mapErr(err)
	}
	var resp listMerchantOrdersOutput
	resp.Body.Date = day.Format("2006-01-02")
	resp.Body.Items = make([]merchantOrderDTO, 0, len(orders))
	for _, o := range orders {
		if in.Plant != "" && o.Plant != in.Plant {
			continue
		}
		resp.Body.Items = append(resp.Body.Items, toMerchantDTO(o))
	}
	return &resp, nil
}

func (a *API) prepSheet(ctx context.Context, in *prepSheetInput) (*prepSheetOutput, error) {
	_, vendorID, err := a.requireVendor(ctx)
	if err != nil {
		return nil, err
	}
	day := time.Now().UTC().Truncate(24 * time.Hour)
	if in.Date != "" {
		d, perr := time.Parse("2006-01-02", in.Date)
		if perr != nil {
			return nil, huma.Error400BadRequest("invalid date (want YYYY-MM-DD)")
		}
		day = d.UTC()
	}
	sheet, err := a.Svc.PrepSheet(ctx, vendorID, day)
	if err != nil {
		return nil, mapErr(err)
	}
	var resp prepSheetOutput
	resp.Body.Date = sheet.Date.Format("2006-01-02")
	resp.Body.TotalOrders = sheet.TotalOrders
	resp.Body.TotalPortions = sheet.TotalPortions
	resp.Body.Plants = make([]prepSheetPlantDTO, 0, len(sheet.Plants))
	for _, p := range sheet.Plants {
		pd := prepSheetPlantDTO{
			Plant:        p.Plant,
			OrderCount:   p.OrderCount,
			PortionCount: p.PortionCount,
			Items:        prepItemDTOs(p.Items),
			Orders:       make([]prepSheetOrderDTO, 0, len(p.Orders)),
		}
		for _, o := range p.Orders {
			pd.Orders = append(pd.Orders, prepSheetOrderDTO{
				OrderID:         o.OrderID,
				OrderNumber:     o.OrderNumber,
				TotalPriceMinor: o.TotalPriceMinor,
				Notes:           o.Notes,
				Items:           prepItemDTOs(o.Items),
			})
		}
		resp.Body.Plants = append(resp.Body.Plants, pd)
	}
	return &resp, nil
}

// streamSSE runs the shared SSE keep-alive loop: it forwards each item from ch
// (mapped to a payload via toPayload) and emits a 20s ping, returning when the
// context is cancelled, the channel closes, or a send fails.
func streamSSE[T any](ctx context.Context, send sse.Sender, ch <-chan T, toPayload func(T) any) {
	heartbeat := time.NewTicker(20 * time.Second)
	defer heartbeat.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-heartbeat.C:
			if send.Data(order.BoardEvent{Kind: "ping"}) != nil {
				return
			}
		case ev, open := <-ch:
			if !open {
				return
			}
			if send.Data(toPayload(ev)) != nil {
				return
			}
		}
	}
}

// streamMerchantOrderEvents streams live order events for the caller's vendor
// over SSE. The merchant prep board re-fetches its data on each event, so the
// payload stays minimal. A 20s keep-alive ping holds the connection open.
func (a *API) streamMerchantOrderEvents(ctx context.Context, _ *struct{}, send sse.Sender) {
	u, ok := idhttp.UserFromContext(ctx)
	if !ok || u.Role != identity.RoleVendorOperator || u.VendorID == nil || *u.VendorID == "" {
		return
	}
	if a.Board == nil {
		<-ctx.Done()
		return
	}
	ch, unsub := a.Board.Subscribe(*u.VendorID)
	defer unsub()
	streamSSE(ctx, send, ch, func(ev order.BoardEvent) any { return ev })
}

// streamEmployeeMenuEvents streams a "menu changed" signal to the employee
// menu view so it refetches when an order shifts available stock.
func (a *API) streamEmployeeMenuEvents(ctx context.Context, _ *struct{}, send sse.Sender) {
	u, ok := idhttp.UserFromContext(ctx)
	if !ok || u.Role != identity.RoleEmployee {
		return
	}
	if a.MenuHub == nil {
		<-ctx.Done()
		return
	}
	ch, unsub := a.MenuHub.Subscribe()
	defer unsub()
	streamSSE(ctx, send, ch, func(struct{}) any { return order.BoardEvent{Kind: "changed"} })
}

// mapErr translates domain errors to huma HTTP errors.
// Conflict (409) for state / cutoff / stock; 400 for
// bad input; 403 for ownership; 404 for missing; 500 fallback.
func mapErr(err error) error {
	switch {
	case errors.Is(err, order.ErrOrderNotFound):
		return huma.Error404NotFound(err.Error())
	case errors.Is(err, order.ErrForbidden):
		return huma.Error403Forbidden(err.Error())
	case errors.Is(err, order.ErrInvalidTransition),
		errors.Is(err, order.ErrCutoffPassed),
		errors.Is(err, order.ErrConcurrentModification),
		errors.Is(err, quota.ErrOutOfStock),
		errors.Is(err, quota.ErrSupplyNotFound):
		return huma.Error409Conflict(err.Error())
	case errors.Is(err, order.ErrEmptyOrder),
		errors.Is(err, order.ErrMultiVendor),
		errors.Is(err, order.ErrVendorPlantMismatch),
		errors.Is(err, order.ErrPlantMismatch),
		errors.Is(err, order.ErrOutsidePreorderWindow):
		return huma.Error400BadRequest(err.Error())
	}
	// Diagnostic: surface the unmapped error class so we can spot
	// race-condition / leaked-tx errors in production logs. Kept permanent —
	// a 500 from an unmapped error is always a bug we want to fix.
	slog.Error("order http unmapped error → 500",
		"err", err.Error(),
		"type", fmt.Sprintf("%T", err),
	)
	return huma.Error500InternalServerError("internal", err)
}
