package settlementhttp

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/takalawang/corporate-catering-system/services/api/internal/identity"
	idhttp "github.com/takalawang/corporate-catering-system/services/api/internal/identity/http"
	"github.com/takalawang/corporate-catering-system/services/api/internal/settlement"
)

// API exposes vendor-settlement endpoints: merchant-facing reconciliation +
// settlement reads (vendor_id resolved from session) and admin-facing close /
// void / overview (welfare_admin only).
type API struct {
	Svc *settlement.Service
}

type settlementDTO struct {
	ID           string  `json:"id"`
	VendorID     string  `json:"vendor_id"`
	PeriodStart  string  `json:"period_start"`
	PeriodEnd    string  `json:"period_end"`
	OrderCount   int     `json:"order_count"`
	PortionCount int     `json:"portion_count"`
	GrossMinor   int64   `json:"gross_minor"`
	Status       string  `json:"status"`
	ClosedAt     string  `json:"closed_at"`
	ClosedBy     *string `json:"closed_by,omitempty"`
}

type orderLineDTO struct {
	OrderID         string `json:"order_id"`
	SupplyDate      string `json:"supply_date"`
	Status          string `json:"status"`
	TotalPriceMinor int64  `json:"total_price_minor"`
	PortionCount    int    `json:"portion_count"`
}

type statusBreakdownDTO struct {
	PickedUp  int `json:"picked_up"`
	NoShow    int `json:"no_show"`
	Cancelled int `json:"cancelled"`
	Refunded  int `json:"refunded"`
}

type reconciliationDTO struct {
	VendorID     string             `json:"vendor_id"`
	PeriodStart  string             `json:"period_start"`
	PeriodEnd    string             `json:"period_end"`
	OrderCount   int                `json:"order_count"`
	PortionCount int                `json:"portion_count"`
	GrossMinor   int64              `json:"gross_minor"`
	Breakdown    statusBreakdownDTO `json:"breakdown"`
}

func toSettlementDTO(s *settlement.Settlement) settlementDTO {
	d := settlementDTO{
		ID:           s.ID,
		VendorID:     s.VendorID,
		PeriodStart:  s.PeriodStart.UTC().Format("2006-01-02"),
		PeriodEnd:    s.PeriodEnd.UTC().Format("2006-01-02"),
		OrderCount:   s.OrderCount,
		PortionCount: s.PortionCount,
		GrossMinor:   s.GrossMinor,
		Status:       string(s.Status),
		ClosedAt:     s.ClosedAt.UTC().Format(time.RFC3339),
	}
	if s.ClosedBy != nil {
		v := *s.ClosedBy
		d.ClosedBy = &v
	}
	return d
}

func toOrderLineDTO(l *settlement.SettlementOrderLine) orderLineDTO {
	return orderLineDTO{
		OrderID:         l.OrderID,
		SupplyDate:      l.SupplyDate.UTC().Format("2006-01-02"),
		Status:          l.Status,
		TotalPriceMinor: l.TotalPriceMinor,
		PortionCount:    l.PortionCount,
	}
}

func toReconciliationDTO(r *settlement.Reconciliation) reconciliationDTO {
	return reconciliationDTO{
		VendorID:     r.VendorID,
		PeriodStart:  r.PeriodStart.UTC().Format("2006-01-02"),
		PeriodEnd:    r.PeriodEnd.UTC().Format("2006-01-02"),
		OrderCount:   r.OrderCount,
		PortionCount: r.PortionCount,
		GrossMinor:   r.GrossMinor,
		Breakdown: statusBreakdownDTO{
			PickedUp:  r.Breakdown.PickedUp,
			NoShow:    r.Breakdown.NoShow,
			Cancelled: r.Breakdown.Cancelled,
			Refunded:  r.Breakdown.Refunded,
		},
	}
}

type periodQueryInput struct {
	Period string `query:"period" doc:"Month to summarise, YYYY-MM" example:"2026-04"`
}

type reconciliationOutput struct {
	Body struct {
		Reconciliation reconciliationDTO `json:"reconciliation"`
	}
}

type listSettlementsOutput struct {
	Body struct {
		Items []settlementDTO `json:"items"`
	}
}

type settlementIDInput struct {
	ID string `path:"id" format:"uuid"`
}

type settlementDetailOutput struct {
	Body struct {
		Settlement settlementDTO  `json:"settlement"`
		Orders     []orderLineDTO `json:"orders"`
	}
}

type closeSettlementInput struct {
	Body struct {
		PeriodStart string `json:"period_start" doc:"YYYY-MM-DD"`
		PeriodEnd   string `json:"period_end" doc:"YYYY-MM-DD"`
	}
}

type closeSettlementOutput struct {
	Body struct {
		Items []settlementDTO `json:"items"`
	}
}

// Register wires every settlement endpoint onto the huma API. main.go calls
// (&API{Svc: svc}).Register as one of its apiBuilders.
func (a *API) Register(api huma.API) {
	huma.Register(api, huma.Operation{
		OperationID: "getMerchantReconciliation",
		Method:      http.MethodGet,
		Path:        "/api/merchant/reconciliation",
		Summary:     "Live monthly reconciliation summary for the calling vendor",
		Tags:        []string{"merchant", "settlement"},
		Security:    []map[string][]string{{"bearer": {}}},
	}, a.getReconciliation)

	huma.Register(api, huma.Operation{
		OperationID: "listMerchantSettlements",
		Method:      http.MethodGet,
		Path:        "/api/merchant/settlements",
		Summary:     "List the calling vendor's closed settlements",
		Tags:        []string{"merchant", "settlement"},
		Security:    []map[string][]string{{"bearer": {}}},
	}, a.listMerchantSettlements)

	huma.Register(api, huma.Operation{
		OperationID: "getMerchantSettlement",
		Method:      http.MethodGet,
		Path:        "/api/merchant/settlements/{id}",
		Summary:     "Get one settlement with order-level detail",
		Tags:        []string{"merchant", "settlement"},
		Security:    []map[string][]string{{"bearer": {}}},
	}, a.getMerchantSettlement)

	huma.Register(api, huma.Operation{
		OperationID: "listVendorSettlements",
		Method:      http.MethodGet,
		Path:        "/api/admin/vendor-settlements",
		Summary:     "All-vendor settlement overview for a period",
		Tags:        []string{"admin", "settlement"},
		Security:    []map[string][]string{{"bearer": {}}},
	}, a.listVendorSettlements)

	huma.Register(api, huma.Operation{
		OperationID:   "closeVendorSettlement",
		Method:        http.MethodPost,
		Path:          "/api/admin/vendor-settlements/close",
		Summary:       "Close a period: cut one settlement per vendor with orders",
		Tags:          []string{"admin", "settlement"},
		Security:      []map[string][]string{{"bearer": {}}},
		DefaultStatus: http.StatusCreated,
	}, a.closeSettlement)

	huma.Register(api, huma.Operation{
		OperationID:   "voidVendorSettlement",
		Method:        http.MethodPost,
		Path:          "/api/admin/vendor-settlements/{id}/void",
		Summary:       "Void a closed settlement so the period can be re-closed",
		Tags:          []string{"admin", "settlement"},
		Security:      []map[string][]string{{"bearer": {}}},
		DefaultStatus: http.StatusNoContent,
	}, a.voidSettlement)
}

func (a *API) requireVendor(ctx context.Context) (*identity.User, string, error) {
	return idhttp.RequireVendor(ctx)
}

func (a *API) requireAdmin(ctx context.Context) (*identity.User, error) {
	return idhttp.RequireAdmin(ctx)
}

// parseMonth turns "YYYY-MM" into the [first day, last day] of that month (UTC).
func parseMonth(s string) (time.Time, time.Time, error) {
	first, err := time.ParseInLocation("2006-01", s, time.UTC)
	if err != nil {
		return time.Time{}, time.Time{}, err
	}
	last := first.AddDate(0, 1, -1)
	return first, last, nil
}

// parseDay parses YYYY-MM-DD into UTC midnight.
func parseDay(s string) (time.Time, error) {
	return time.ParseInLocation("2006-01-02", s, time.UTC)
}

func (a *API) getReconciliation(ctx context.Context, in *periodQueryInput) (*reconciliationOutput, error) {
	_, vendorID, err := a.requireVendor(ctx)
	if err != nil {
		return nil, err
	}
	start, end, err := parseMonth(in.Period)
	if err != nil {
		return nil, huma.Error400BadRequest("invalid period (YYYY-MM)")
	}
	rec, err := a.Svc.Reconciliation(ctx, vendorID, start, end)
	if err != nil {
		return nil, mapErr(err)
	}
	var resp reconciliationOutput
	resp.Body.Reconciliation = toReconciliationDTO(rec)
	return &resp, nil
}

func (a *API) listMerchantSettlements(ctx context.Context, _ *struct{}) (*listSettlementsOutput, error) {
	_, vendorID, err := a.requireVendor(ctx)
	if err != nil {
		return nil, err
	}
	items, err := a.Svc.ListVendorSettlements(ctx, vendorID)
	if err != nil {
		return nil, mapErr(err)
	}
	var resp listSettlementsOutput
	resp.Body.Items = make([]settlementDTO, 0, len(items))
	for _, s := range items {
		resp.Body.Items = append(resp.Body.Items, toSettlementDTO(s))
	}
	return &resp, nil
}

func (a *API) getMerchantSettlement(ctx context.Context, in *settlementIDInput) (*settlementDetailOutput, error) {
	_, vendorID, err := a.requireVendor(ctx)
	if err != nil {
		return nil, err
	}
	st, lines, err := a.Svc.GetVendorSettlement(ctx, vendorID, in.ID)
	if err != nil {
		return nil, mapErr(err)
	}
	var resp settlementDetailOutput
	resp.Body.Settlement = toSettlementDTO(st)
	resp.Body.Orders = make([]orderLineDTO, 0, len(lines))
	for _, l := range lines {
		resp.Body.Orders = append(resp.Body.Orders, toOrderLineDTO(l))
	}
	return &resp, nil
}

func (a *API) listVendorSettlements(ctx context.Context, in *periodQueryInput) (*listSettlementsOutput, error) {
	if _, err := a.requireAdmin(ctx); err != nil {
		return nil, err
	}
	start, end, err := parseMonth(in.Period)
	if err != nil {
		return nil, huma.Error400BadRequest("invalid period (YYYY-MM)")
	}
	items, err := a.Svc.ListSettlementsByPeriod(ctx, start, end)
	if err != nil {
		return nil, mapErr(err)
	}
	var resp listSettlementsOutput
	resp.Body.Items = make([]settlementDTO, 0, len(items))
	for _, s := range items {
		resp.Body.Items = append(resp.Body.Items, toSettlementDTO(s))
	}
	return &resp, nil
}

func (a *API) closeSettlement(ctx context.Context, in *closeSettlementInput) (*closeSettlementOutput, error) {
	u, err := a.requireAdmin(ctx)
	if err != nil {
		return nil, err
	}
	start, err := parseDay(in.Body.PeriodStart)
	if err != nil {
		return nil, huma.Error400BadRequest("invalid period_start (YYYY-MM-DD)")
	}
	end, err := parseDay(in.Body.PeriodEnd)
	if err != nil {
		return nil, huma.Error400BadRequest("invalid period_end (YYYY-MM-DD)")
	}
	items, err := a.Svc.CloseSettlement(ctx, settlement.CloseSettlementInput{
		PeriodStart: start,
		PeriodEnd:   end,
		ClosedBy:    u.ID,
	})
	if err != nil {
		return nil, mapErr(err)
	}
	var resp closeSettlementOutput
	resp.Body.Items = make([]settlementDTO, 0, len(items))
	for _, s := range items {
		resp.Body.Items = append(resp.Body.Items, toSettlementDTO(s))
	}
	return &resp, nil
}

func (a *API) voidSettlement(ctx context.Context, in *settlementIDInput) (*struct{}, error) {
	u, err := a.requireAdmin(ctx)
	if err != nil {
		return nil, err
	}
	if err := a.Svc.VoidSettlement(ctx, in.ID, u.ID); err != nil {
		return nil, mapErr(err)
	}
	return &struct{}{}, nil
}

// mapErr translates settlement sentinels to huma HTTP errors.
func mapErr(err error) error {
	switch {
	case errors.Is(err, settlement.ErrSettlementNotFound):
		return huma.Error404NotFound(err.Error())
	case errors.Is(err, settlement.ErrForbidden):
		return huma.Error403Forbidden(err.Error())
	case errors.Is(err, settlement.ErrInvalidPeriod),
		errors.Is(err, settlement.ErrNoOrdersInPeriod):
		return huma.Error400BadRequest(err.Error())
	case errors.Is(err, settlement.ErrPeriodAlreadyClosed),
		errors.Is(err, settlement.ErrInvalidTransition):
		return huma.Error409Conflict(err.Error())
	}
	return huma.Error500InternalServerError("internal", err)
}
