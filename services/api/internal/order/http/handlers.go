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

type orderOutput struct {
	Body struct {
		Order orderDTO `json:"order"`
	}
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
		OperationID:   "cancelMyOrder",
		Method:        http.MethodPost,
		Path:          "/api/employee/orders/{id}/cancel",
		Summary:       "Cancel my order",
		Tags:          []string{"employee", "order"},
		Security:      []map[string][]string{{"bearer": {}}},
		DefaultStatus: http.StatusNoContent,
	}, a.cancel)
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

// mapErr translates domain errors to huma HTTP errors.
// Conflict (409) for state / cutoff / stock; 400 for bad input; 403 for
// ownership; 404 for missing; 500 fallback.
func mapErr(err error) error {
	switch {
	case errors.Is(err, order.ErrOrderNotFound):
		return huma.Error404NotFound(err.Error())
	case errors.Is(err, order.ErrForbidden):
		return huma.Error403Forbidden(err.Error())
	case errors.Is(err, order.ErrInvalidTransition),
		errors.Is(err, order.ErrCutoffPassed),
		errors.Is(err, quota.ErrOutOfStock),
		errors.Is(err, quota.ErrSupplyNotFound):
		return huma.Error409Conflict(err.Error())
	case errors.Is(err, order.ErrEmptyOrder),
		errors.Is(err, order.ErrVendorPlantMismatch),
		errors.Is(err, order.ErrPlantMismatch):
		return huma.Error400BadRequest(err.Error())
	}
	return huma.Error500InternalServerError("internal", err)
}
