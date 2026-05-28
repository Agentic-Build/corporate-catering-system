package ohttp

import (
	"context"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/sse"

	"github.com/Agentic-Build/corporate-catering-system/services/api/internal/httpserver"
	"github.com/Agentic-Build/corporate-catering-system/services/api/internal/identity"
	idhttp "github.com/Agentic-Build/corporate-catering-system/services/api/internal/identity/http"
	"github.com/Agentic-Build/corporate-catering-system/services/api/internal/order"
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
	d.PlacedAt = httpserver.FormatNullableTimePtr(o.PlacedAt)
	d.CancelledAt = httpserver.FormatNullableTimePtr(o.CancelledAt)
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
