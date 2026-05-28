package ohttp

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"github.com/Agentic-Build/corporate-catering-system/services/api/internal/identity"
	idhttp "github.com/Agentic-Build/corporate-catering-system/services/api/internal/identity/http"
	"github.com/Agentic-Build/corporate-catering-system/services/api/internal/order"
)

// ReorderAPI exposes POST /api/employee/orders/reorder. Kept separate from
// API so the new endpoint can be wired alongside the existing order handlers
// without modifying handlers.go (P9 scope constraint).
type ReorderAPI struct {
	Svc *order.ReorderService
}

type reorderInput struct {
	Body struct {
		SourceOrderID string `json:"source_order_id" format:"uuid"`
		SupplyDate    string `json:"supply_date"` // YYYY-MM-DD
	}
}

type unavailableItemDTO struct {
	MenuItemID string `json:"menu_item_id"`
	Name       string `json:"name"`
	Reason     string `json:"reason"`
}

type reorderOutputBody struct {
	NewOrderID       string               `json:"new_order_id"`
	UnavailableItems []unavailableItemDTO `json:"unavailable_items"`
}

type reorderOutput struct {
	Body reorderOutputBody
}

func (a *ReorderAPI) Register(api huma.API) {
	huma.Register(api, huma.Operation{
		OperationID:   "reorderMyOrder",
		Method:        http.MethodPost,
		Path:          "/api/employee/orders/reorder",
		Summary:       "Clone a past order onto a new supply date (partial fallback)",
		Tags:          []string{"employee", "order"},
		Security:      []map[string][]string{{"bearer": {}}},
		DefaultStatus: http.StatusCreated,
	}, a.reorder)
}

func (a *ReorderAPI) reorder(ctx context.Context, in *reorderInput) (*reorderOutput, error) {
	u, ok := idhttp.UserFromContext(ctx)
	if !ok {
		return nil, huma.Error401Unauthorized("not authenticated")
	}
	if u.Role != identity.RoleEmployee {
		return nil, huma.Error403Forbidden("employee role required")
	}
	if u.Plant == nil || *u.Plant == "" {
		return nil, huma.Error400BadRequest("user has no plant assignment")
	}
	if in.Body.SourceOrderID == "" {
		return nil, huma.Error400BadRequest("source_order_id required")
	}
	if in.Body.SupplyDate == "" {
		return nil, huma.Error400BadRequest("supply_date required")
	}

	res, err := a.Svc.Reorder(ctx, order.ReorderInput{
		UserID:        u.ID,
		SourceOrderID: in.Body.SourceOrderID,
		SupplyDate:    in.Body.SupplyDate,
		Plant:         *u.Plant,
	})
	if err != nil {
		return nil, reorderMapErr(err)
	}

	unavailable := toUnavailableDTOs(res.UnavailableItems)
	// Zero survivors → 409 with the unavailable list so the client can show a
	// "nothing in this past order is available today" message.
	if res.NewOrderID == "" {
		return nil, huma.Error409Conflict("all_items_unavailable", &allUnavailableDetail{Items: unavailable})
	}
	return &reorderOutput{Body: reorderOutputBody{
		NewOrderID:       res.NewOrderID,
		UnavailableItems: unavailable,
	}}, nil
}

// allUnavailableDetail is the error-detail payload attached to the 409 when
// no items survived. huma surfaces details via the "errors" array on the
// response body.
type allUnavailableDetail struct {
	Items []unavailableItemDTO `json:"unavailable_items"`
}

func (d *allUnavailableDetail) Error() string { return "all_items_unavailable" }

// ErrorDetail satisfies huma.ErrorDetailer so the unavailable list is rendered
// in the standard "errors[].value" slot.
func (d *allUnavailableDetail) ErrorDetail() *huma.ErrorDetail {
	return &huma.ErrorDetail{
		Message:  "all_items_unavailable",
		Location: "body.items",
		Value:    d.Items,
	}
}

func toUnavailableDTOs(items []order.UnavailableItem) []unavailableItemDTO {
	out := make([]unavailableItemDTO, 0, len(items))
	for _, it := range items {
		out = append(out, unavailableItemDTO{
			MenuItemID: it.MenuItemID,
			Name:       it.Name,
			Reason:     it.Reason,
		})
	}
	return out
}

// reorderMapErr delegates to the shared order errRules — the reorder paths
// surface the same sentinels as Place (cutoff/stock/state/plant).
func reorderMapErr(err error) error { return mapErr(err) }
