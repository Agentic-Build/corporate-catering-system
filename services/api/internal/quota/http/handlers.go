package qhttp

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/takalawang/corporate-catering-system/services/api/internal/identity"
	idhttp "github.com/takalawang/corporate-catering-system/services/api/internal/identity/http"
	"github.com/takalawang/corporate-catering-system/services/api/internal/menu"
	"github.com/takalawang/corporate-catering-system/services/api/internal/quota"
)

// API exposes vendor-facing quota endpoints (set/list supply for the merchant).
// Decrement/Restore are not surfaced here — order placement (P3) consumes the
// repo directly. Employee `remain` is already served by GET /api/employee/menu.
type API struct {
	Svc *quota.Service
}

// ----- DTOs -----

type supplyDTO struct {
	ID           string `json:"id"`
	MenuItemID   string `json:"menu_item_id"`
	SupplyDate   string `json:"supply_date"` // YYYY-MM-DD
	Capacity     int    `json:"capacity"`
	Remain       int    `json:"remain"`
	PickupWindow string `json:"pickup_window"`
	ETALabel     string `json:"eta_label"`
	CutoffAt     string `json:"cutoff_at"`
}

func toDTO(s *quota.Supply) supplyDTO {
	return supplyDTO{
		ID:           s.ID,
		MenuItemID:   s.MenuItemID,
		SupplyDate:   s.SupplyDate.Format("2006-01-02"),
		Capacity:     s.Capacity,
		Remain:       s.Remain,
		PickupWindow: s.PickupWindow,
		ETALabel:     s.ETALabel,
		CutoffAt:     s.CutoffAt.UTC().Format(time.RFC3339),
	}
}

// ----- Inputs / Outputs -----

type setCapacityInput struct {
	ItemID string `path:"itemID" format:"uuid"`
	Date   string `path:"date"` // YYYY-MM-DD
	Body   struct {
		Capacity     int    `json:"capacity" minimum:"0"`
		PickupWindow string `json:"pickup_window"`
		ETALabel     string `json:"eta_label"`
		CutoffAt     string `json:"cutoff_at" format:"date-time"`
	}
}

type supplyOutput struct {
	Body struct {
		Supply supplyDTO `json:"supply"`
	}
}

type listSupplyInput struct {
	Date string `query:"date" doc:"YYYY-MM-DD; defaults to today UTC"`
}

type listSupplyOutput struct {
	Body struct {
		Date  string      `json:"date"`
		Items []supplyDTO `json:"items"`
	}
}

// ----- Registration -----

func (a *API) Register(api huma.API) {
	huma.Register(api, huma.Operation{
		OperationID: "setMerchantSupply",
		Method:      http.MethodPut,
		Path:        "/api/merchant/supply/{itemID}/{date}",
		Summary:     "Set or update capacity for a menu item on a given date",
		Tags:        []string{"merchant", "quota"},
		Security:    []map[string][]string{{"bearer": {}}},
	}, a.setCapacity)

	huma.Register(api, huma.Operation{
		OperationID: "listMerchantSupply",
		Method:      http.MethodGet,
		Path:        "/api/merchant/supply",
		Summary:     "List supplies for the current vendor on a date",
		Tags:        []string{"merchant", "quota"},
		Security:    []map[string][]string{{"bearer": {}}},
	}, a.list)
}

// ----- Auth helper -----

func (a *API) requireVendor(ctx context.Context) (*identity.User, string, error) {
	u, ok := idhttp.UserFromContext(ctx)
	if !ok {
		return nil, "", huma.Error401Unauthorized("not authenticated")
	}
	if u.Role != identity.RoleVendorOperator {
		return nil, "", huma.Error403Forbidden("vendor operator required")
	}
	if u.VendorID == nil || *u.VendorID == "" {
		return nil, "", huma.Error403Forbidden("user is not bound to a vendor")
	}
	return u, *u.VendorID, nil
}

func parseDate(s string, fallback time.Time) (time.Time, error) {
	if s == "" {
		return fallback, nil
	}
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		return time.Time{}, huma.Error400BadRequest("invalid date (want YYYY-MM-DD)")
	}
	return t.UTC(), nil
}

// ----- Handlers -----

func (a *API) setCapacity(ctx context.Context, in *setCapacityInput) (*supplyOutput, error) {
	_, vendorID, err := a.requireVendor(ctx)
	if err != nil {
		return nil, err
	}
	day, err := parseDate(in.Date, time.Time{})
	if err != nil {
		return nil, err
	}
	cutoff, err := time.Parse(time.RFC3339, in.Body.CutoffAt)
	if err != nil {
		return nil, huma.Error400BadRequest("invalid cutoff_at (want RFC3339)")
	}
	sp, err := a.Svc.SetCapacity(ctx, vendorID, quota.SetCapacityInput{
		MenuItemID:   in.ItemID,
		Date:         day,
		Capacity:     in.Body.Capacity,
		PickupWindow: in.Body.PickupWindow,
		ETALabel:     in.Body.ETALabel,
		CutoffAt:     cutoff,
	})
	if err != nil {
		return nil, mapErr(err)
	}
	var resp supplyOutput
	resp.Body.Supply = toDTO(sp)
	return &resp, nil
}

func (a *API) list(ctx context.Context, in *listSupplyInput) (*listSupplyOutput, error) {
	_, vendorID, err := a.requireVendor(ctx)
	if err != nil {
		return nil, err
	}
	today := time.Now().UTC().Truncate(24 * time.Hour)
	day, err := parseDate(in.Date, today)
	if err != nil {
		return nil, err
	}
	supplies, err := a.Svc.ListForVendor(ctx, vendorID, day)
	if err != nil {
		return nil, mapErr(err)
	}
	var resp listSupplyOutput
	resp.Body.Date = day.Format("2006-01-02")
	resp.Body.Items = make([]supplyDTO, 0, len(supplies))
	for _, s := range supplies {
		resp.Body.Items = append(resp.Body.Items, toDTO(s))
	}
	return &resp, nil
}

func mapErr(err error) error {
	switch {
	case errors.Is(err, quota.ErrSupplyNotFound), errors.Is(err, menu.ErrItemNotFound):
		return huma.Error404NotFound(err.Error())
	case errors.Is(err, menu.ErrForbidden):
		return huma.Error403Forbidden(err.Error())
	}
	return huma.Error500InternalServerError("internal", err)
}
