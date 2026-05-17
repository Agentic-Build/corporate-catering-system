package ohttp

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/takalawang/corporate-catering-system/services/api/internal/identity"
	idhttp "github.com/takalawang/corporate-catering-system/services/api/internal/identity/http"
	"github.com/takalawang/corporate-catering-system/services/api/internal/order"
	totp "github.com/takalawang/corporate-catering-system/services/api/internal/pickup/totp"
	"github.com/takalawang/corporate-catering-system/services/api/internal/quota"
)

// API exposes employee-facing order endpoints: place / list / get / cancel.
// All endpoints require the employee role.
type API struct {
	Svc *order.Service
}

// ----- DTOs -----

type orderItemDTO struct {
	ID             string `json:"id"`
	MenuItemID     string `json:"menu_item_id"`
	Qty            int    `json:"qty"`
	UnitPriceMinor int64  `json:"unit_price_minor"`
}

type orderDTO struct {
	ID              string         `json:"id"`
	VendorID        string         `json:"vendor_id"`
	Plant           string         `json:"plant"`
	SupplyDate      string         `json:"supply_date"`
	Status          string         `json:"status"`
	TotalPriceMinor int64          `json:"total_price_minor"`
	PlacedAt        *string        `json:"placed_at,omitempty"`
	CutoffAt        string         `json:"cutoff_at"`
	CancelledAt     *string        `json:"cancelled_at,omitempty"`
	Items           []orderItemDTO `json:"items"`
}

func toDTO(o *order.Order) orderDTO {
	d := orderDTO{
		ID:              o.ID,
		VendorID:        o.VendorID,
		Plant:           o.Plant,
		SupplyDate:      o.SupplyDate.Format("2006-01-02"),
		Status:          string(o.Status),
		TotalPriceMinor: o.TotalPriceMinor,
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
			Qty:            it.Qty,
			UnitPriceMinor: it.UnitPriceMinor,
		})
	}
	return d
}

// ----- Inputs / Outputs -----

type placeOrderInput struct {
	Body struct {
		Plant      string `json:"plant"`
		SupplyDate string `json:"supply_date"` // YYYY-MM-DD
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

type pickupCodeOutput struct {
	Body struct {
		OrderID          string `json:"order_id"`
		Code             string `json:"code"`
		ExpiresInSeconds int    `json:"expires_in_seconds"`
	}
}

type verifyPickupInput struct {
	ID   string `path:"id" format:"uuid"`
	Body struct {
		Code string `json:"code" minLength:"6" maxLength:"6"`
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
	Plant           string         `json:"plant"`
	Status          string         `json:"status"`
	TotalPriceMinor int64          `json:"total_price_minor"`
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
		Plant:           o.Plant,
		Status:          string(o.Status),
		TotalPriceMinor: o.TotalPriceMinor,
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
			Qty:            it.Qty,
			UnitPriceMinor: it.UnitPriceMinor,
		})
	}
	return d
}

// ----- Registration -----

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
		OperationID: "getPickupCode",
		Method:      http.MethodGet,
		Path:        "/api/employee/orders/{id}/pickup-code",
		Summary:     "Get current TOTP pickup code for an order (owner, ready only)",
		Tags:        []string{"employee", "order"},
		Security:    []map[string][]string{{"bearer": {}}},
	}, a.getPickupCode)

	huma.Register(api, huma.Operation{
		OperationID:   "verifyPickup",
		Method:        http.MethodPost,
		Path:          "/api/merchant/orders/{id}/verify-pickup",
		Summary:       "Verify TOTP code and mark order picked up",
		Tags:          []string{"merchant", "order"},
		Security:      []map[string][]string{{"bearer": {}}},
		DefaultStatus: http.StatusNoContent,
	}, a.verifyPickup)

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
}

// ----- Auth helper -----

func (a *API) requireEmployee(ctx context.Context) (*identity.User, error) {
	u, ok := idhttp.UserFromContext(ctx)
	if !ok {
		return nil, huma.Error401Unauthorized("not authenticated")
	}
	if u.Role != identity.RoleEmployee {
		return nil, huma.Error403Forbidden("employee role required")
	}
	return u, nil
}

func (a *API) requireVendor(ctx context.Context) (*identity.User, string, error) {
	u, ok := idhttp.UserFromContext(ctx)
	if !ok {
		return nil, "", huma.Error401Unauthorized("not authenticated")
	}
	if u.Role != identity.RoleVendorOperator {
		return nil, "", huma.Error403Forbidden("vendor operator required")
	}
	if u.VendorID == nil || *u.VendorID == "" {
		return nil, "", huma.Error403Forbidden("user not bound to a vendor")
	}
	return u, *u.VendorID, nil
}

// ----- Handlers -----

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

func (a *API) getPickupCode(ctx context.Context, in *orderIDInput) (*pickupCodeOutput, error) {
	u, err := a.requireEmployee(ctx)
	if err != nil {
		return nil, err
	}
	o, err := a.Svc.Get(ctx, in.ID, u.ID)
	if err != nil {
		return nil, mapErr(err)
	}
	if o.Status != order.StatusReady {
		return nil, huma.Error409Conflict("order is not ready for pickup")
	}
	now := time.Now()
	code := totp.Generate(o.TOTPSecret, now)
	var resp pickupCodeOutput
	resp.Body.OrderID = o.ID
	resp.Body.Code = code
	resp.Body.ExpiresInSeconds = totp.StepSeconds - int(now.Unix()%int64(totp.StepSeconds))
	return &resp, nil
}

func (a *API) verifyPickup(ctx context.Context, in *verifyPickupInput) (*struct{}, error) {
	u, _, err := a.requireVendor(ctx)
	if err != nil {
		return nil, err
	}
	if err := a.Svc.VerifyPickup(ctx, in.ID, in.Body.Code, u.ID); err != nil {
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

// mapErr translates domain errors to huma HTTP errors.
// Conflict (409) for state / cutoff / stock / invalid pickup code; 400 for
// bad input; 403 for ownership; 404 for missing; 500 fallback.
func mapErr(err error) error {
	switch {
	case errors.Is(err, order.ErrOrderNotFound):
		return huma.Error404NotFound(err.Error())
	case errors.Is(err, order.ErrForbidden):
		return huma.Error403Forbidden(err.Error())
	case errors.Is(err, order.ErrInvalidTransition),
		errors.Is(err, order.ErrCutoffPassed),
		errors.Is(err, order.ErrInvalidPickupCode),
		errors.Is(err, quota.ErrOutOfStock),
		errors.Is(err, quota.ErrSupplyNotFound):
		return huma.Error409Conflict(err.Error())
	case errors.Is(err, order.ErrEmptyOrder),
		errors.Is(err, order.ErrMultiVendor),
		errors.Is(err, order.ErrVendorPlantMismatch),
		errors.Is(err, order.ErrPlantMismatch):
		return huma.Error400BadRequest(err.Error())
	}
	return huma.Error500InternalServerError("internal", err)
}
