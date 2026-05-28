package qhttp

import (
	"context"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/Agentic-Build/corporate-catering-system/services/api/internal/identity"
	idhttp "github.com/Agentic-Build/corporate-catering-system/services/api/internal/identity/http"
	"github.com/Agentic-Build/corporate-catering-system/services/api/internal/quota"
)

const dateLayoutISO = "2006-01-02"

// API exposes vendor-facing quota endpoints (set/list supply for the merchant).
// Decrement/Restore are not surfaced here — order placement (P3) consumes the
// repo directly. Employee `remain` is already served by GET /api/employee/menu.
type API struct {
	Svc *quota.Service
}

type supplyDTO struct {
	ID           string `json:"id"`
	MenuItemID   string `json:"menu_item_id"`
	SupplyDate   string `json:"supply_date"` // YYYY-MM-DD
	Capacity     int    `json:"capacity"`
	Remain       int    `json:"remain"`
	SoldOut      bool   `json:"sold_out"`
	PickupWindow string `json:"pickup_window"`
	ETALabel     string `json:"eta_label"`
	CutoffAt     string `json:"cutoff_at"`
}

func toDTO(s *quota.Supply) supplyDTO {
	return supplyDTO{
		ID:           s.ID,
		MenuItemID:   s.MenuItemID,
		SupplyDate:   s.SupplyDate.Format(dateLayoutISO),
		Capacity:     s.Capacity,
		Remain:       s.Remain,
		SoldOut:      s.SoldOut,
		PickupWindow: s.PickupWindow,
		ETALabel:     s.ETALabel,
		CutoffAt:     s.CutoffAt.UTC().Format(time.RFC3339),
	}
}

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

type setSoldOutInput struct {
	ItemID string `path:"itemID" format:"uuid"`
	Date   string `path:"date"` // YYYY-MM-DD
	Body   struct {
		SoldOut bool `json:"sold_out"`
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

	huma.Register(api, huma.Operation{
		OperationID: "setMerchantSupplySoldOut",
		Method:      http.MethodPost,
		Path:        "/api/merchant/supply/{itemID}/{date}/sold-out",
		Summary:     "Mark a supply temporarily sold out (or back in stock)",
		Tags:        []string{"merchant", "quota"},
		Security:    []map[string][]string{{"bearer": {}}},
	}, a.setSoldOut)
}

func (a *API) requireVendor(ctx context.Context) (*identity.User, string, error) {
	return idhttp.RequireVendor(ctx)
}

func parseDate(s string, fallback time.Time) (time.Time, error) {
	if s == "" {
		return fallback, nil
	}
	t, err := time.Parse(dateLayoutISO, s)
	if err != nil {
		return time.Time{}, huma.Error400BadRequest("invalid date (want YYYY-MM-DD)")
	}
	return t.UTC(), nil
}

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

func (a *API) setSoldOut(ctx context.Context, in *setSoldOutInput) (*supplyOutput, error) {
	_, vendorID, err := a.requireVendor(ctx)
	if err != nil {
		return nil, err
	}
	day, err := parseDate(in.Date, time.Time{})
	if err != nil {
		return nil, err
	}
	sp, err := a.Svc.SetSoldOut(ctx, vendorID, in.ItemID, day, in.Body.SoldOut)
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
	resp.Body.Date = day.Format(dateLayoutISO)
	resp.Body.Items = make([]supplyDTO, 0, len(supplies))
	for _, s := range supplies {
		resp.Body.Items = append(resp.Body.Items, toDTO(s))
	}
	return &resp, nil
}
