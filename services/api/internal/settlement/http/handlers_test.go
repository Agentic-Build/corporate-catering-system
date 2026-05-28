package settlementhttp_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humachi"
	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Agentic-Build/corporate-catering-system/services/api/internal/identity"
	idhttp "github.com/Agentic-Build/corporate-catering-system/services/api/internal/identity/http"
	"github.com/Agentic-Build/corporate-catering-system/services/api/internal/settlement"
	settlementhttp "github.com/Agentic-Build/corporate-catering-system/services/api/internal/settlement/http"
)

const (
	settlementID = "11111111-1111-1111-1111-111111111111"
	otherID      = "22222222-2222-2222-2222-222222222222"
	vendorID     = "v-owner"
)

// ----- Fakes (settlementhttp_test can't import the settlement_test fakes) -----

type fakeSettlementRepo struct {
	byID        map[string]*settlement.Settlement
	byVendor    []*settlement.Settlement
	byPeriod    []*settlement.Settlement
	getErr      error
	listVendErr error
	listPerErr  error
}

func (r *fakeSettlementRepo) CreateTx(context.Context, pgx.Tx, *settlement.Settlement) error {
	return nil
}
func (r *fakeSettlementRepo) GetByID(_ context.Context, id string) (*settlement.Settlement, error) {
	if r.getErr != nil {
		return nil, r.getErr
	}
	if s, ok := r.byID[id]; ok {
		clone := *s
		return &clone, nil
	}
	return nil, settlement.ErrSettlementNotFound
}
func (r *fakeSettlementRepo) ListByVendor(context.Context, string) ([]*settlement.Settlement, error) {
	if r.listVendErr != nil {
		return nil, r.listVendErr
	}
	return r.byVendor, nil
}
func (r *fakeSettlementRepo) ListByPeriod(context.Context, time.Time, time.Time) ([]*settlement.Settlement, error) {
	if r.listPerErr != nil {
		return nil, r.listPerErr
	}
	return r.byPeriod, nil
}
func (r *fakeSettlementRepo) VoidTx(context.Context, pgx.Tx, string) error { return nil }

type fakeOrderRepo struct {
	aggByVendor  []*settlement.VendorAggregate
	aggForVendor *settlement.VendorAggregate
	breakdown    settlement.StatusBreakdown
	lines        []*settlement.SettlementOrderLine
	aggByErr     error
	aggForErr    error
	breakdownErr error
	linesErr     error
}

func (r *fakeOrderRepo) AggregateByVendor(context.Context, time.Time, time.Time) ([]*settlement.VendorAggregate, error) {
	if r.aggByErr != nil {
		return nil, r.aggByErr
	}
	return r.aggByVendor, nil
}
func (r *fakeOrderRepo) AggregateForVendor(context.Context, string, time.Time, time.Time) (*settlement.VendorAggregate, error) {
	if r.aggForErr != nil {
		return nil, r.aggForErr
	}
	if r.aggForVendor != nil {
		return r.aggForVendor, nil
	}
	return &settlement.VendorAggregate{}, nil
}
func (r *fakeOrderRepo) StatusBreakdownForVendor(context.Context, string, time.Time, time.Time) (settlement.StatusBreakdown, error) {
	if r.breakdownErr != nil {
		return settlement.StatusBreakdown{}, r.breakdownErr
	}
	return r.breakdown, nil
}
func (r *fakeOrderRepo) OrderLinesByIDs(context.Context, []string) ([]*settlement.SettlementOrderLine, error) {
	if r.linesErr != nil {
		return nil, r.linesErr
	}
	return r.lines, nil
}

type fakeAudit struct{}

func (fakeAudit) WriteTx(context.Context, pgx.Tx, *string, *string, string, string, string, map[string]any, string) error {
	return nil
}

// fakeBeginner stands in for *pgxpool.Pool. It hands the write closure a no-op
// pgx.Tx; the repo fakes ignore the tx, so close/void happy paths run without a
// real DB. Set beginErr to exercise the tx-open failure path.
type fakeBeginner struct{ beginErr error }

func (b fakeBeginner) Begin(context.Context) (pgx.Tx, error) {
	if b.beginErr != nil {
		return nil, b.beginErr
	}
	return fakeTx{}, nil
}

type fakeTx struct{ pgx.Tx }

func (fakeTx) Commit(context.Context) error   { return nil }
func (fakeTx) Rollback(context.Context) error { return nil }

// ----- Harness -----

func vendorUser() *identity.User {
	v := vendorID
	return &identity.User{ID: "u-1", Role: identity.RoleVendorOperator, VendorID: &v}
}

func adminUser() *identity.User {
	return &identity.User{ID: "a-1", Role: identity.RoleWelfareAdmin}
}

// buildHandler wires the settlement API onto a chi router. When user != nil a
// middleware injects it into the request context exactly like AuthMiddleware does.
// Service.Pool is a fakeBeginner so the close/void write paths can be exercised
// end-to-end (the repo fakes ignore the tx it hands them).
func buildHandler(t *testing.T, user *identity.User) (*httptest.Server, *fakeSettlementRepo, *fakeOrderRepo) {
	t.Helper()
	sr := &fakeSettlementRepo{byID: map[string]*settlement.Settlement{}}
	or := &fakeOrderRepo{}
	api := &settlementhttp.API{Svc: &settlement.Service{
		Pool:        fakeBeginner{},
		Settlements: sr,
		Orders:      or,
		Audit:       fakeAudit{},
	}}

	r := chi.NewRouter()
	if user != nil {
		r.Use(func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				next.ServeHTTP(w, req.WithContext(idhttp.ContextWithUser(req.Context(), user)))
			})
		})
	}
	h := humachi.New(r, huma.DefaultConfig("test", "0.0.0"))
	api.Register(h)

	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)
	return srv, sr, or
}

func do(t *testing.T, method, url, body string) *http.Response {
	t.Helper()
	rdr := strings.NewReader(body)
	req, err := http.NewRequest(method, url, rdr)
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	return resp
}

func aprilSettlement(id, vendor string, status settlement.Status) *settlement.Settlement {
	start := time.Date(2026, time.April, 1, 0, 0, 0, 0, time.UTC)
	closedBy := "a-1"
	return &settlement.Settlement{
		ID:           id,
		VendorID:     vendor,
		PeriodStart:  start,
		PeriodEnd:    start.AddDate(0, 1, -1),
		OrderCount:   3,
		PortionCount: 7,
		GrossMinor:   26000,
		OrderIDs:     []string{"o-1"},
		Status:       status,
		ClosedAt:     time.Date(2026, time.May, 2, 9, 0, 0, 0, time.UTC),
		ClosedBy:     &closedBy,
	}
}

// =========================================================================
// GET /api/merchant/reconciliation
// =========================================================================

func TestGetReconciliation_Unauthenticated(t *testing.T) {
	srv, _, _ := buildHandler(t, nil)
	resp := do(t, http.MethodGet, srv.URL+"/api/merchant/reconciliation?period=2026-04", "")
	defer resp.Body.Close()
	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
}

func TestGetReconciliation_WrongRole(t *testing.T) {
	srv, _, _ := buildHandler(t, adminUser()) // admin is not a vendor operator
	resp := do(t, http.MethodGet, srv.URL+"/api/merchant/reconciliation?period=2026-04", "")
	defer resp.Body.Close()
	assert.Equal(t, http.StatusForbidden, resp.StatusCode)
}

func TestGetReconciliation_NoVendorBinding(t *testing.T) {
	srv, _, _ := buildHandler(t, &identity.User{ID: "u-1", Role: identity.RoleVendorOperator})
	resp := do(t, http.MethodGet, srv.URL+"/api/merchant/reconciliation?period=2026-04", "")
	defer resp.Body.Close()
	assert.Equal(t, http.StatusForbidden, resp.StatusCode)
}

func TestGetReconciliation_BadPeriod(t *testing.T) {
	srv, _, _ := buildHandler(t, vendorUser())
	resp := do(t, http.MethodGet, srv.URL+"/api/merchant/reconciliation?period=nope", "")
	defer resp.Body.Close()
	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
}

func TestGetReconciliation_OK(t *testing.T) {
	srv, _, or := buildHandler(t, vendorUser())
	or.aggForVendor = &settlement.VendorAggregate{OrderCount: 3, PortionCount: 7, GrossMinor: 26000}
	or.breakdown = settlement.StatusBreakdown{PickedUp: 2, NoShow: 1, Cancelled: 4, Refunded: 5}

	resp := do(t, http.MethodGet, srv.URL+"/api/merchant/reconciliation?period=2026-04", "")
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	var out struct {
		Reconciliation struct {
			VendorID     string `json:"vendor_id"`
			PeriodStart  string `json:"period_start"`
			PeriodEnd    string `json:"period_end"`
			OrderCount   int    `json:"order_count"`
			PortionCount int    `json:"portion_count"`
			GrossMinor   int64  `json:"gross_minor"`
			Breakdown    struct {
				PickedUp  int `json:"picked_up"`
				NoShow    int `json:"no_show"`
				Cancelled int `json:"cancelled"`
				Refunded  int `json:"refunded"`
			} `json:"breakdown"`
		} `json:"reconciliation"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&out))
	assert.Equal(t, vendorID, out.Reconciliation.VendorID)
	assert.Equal(t, "2026-04-01", out.Reconciliation.PeriodStart)
	assert.Equal(t, "2026-04-30", out.Reconciliation.PeriodEnd)
	assert.Equal(t, 3, out.Reconciliation.OrderCount)
	assert.Equal(t, 7, out.Reconciliation.PortionCount)
	assert.Equal(t, int64(26000), out.Reconciliation.GrossMinor) // whole NTD, no /100
	assert.Equal(t, 2, out.Reconciliation.Breakdown.PickedUp)
	assert.Equal(t, 1, out.Reconciliation.Breakdown.NoShow)
	assert.Equal(t, 4, out.Reconciliation.Breakdown.Cancelled)
	assert.Equal(t, 5, out.Reconciliation.Breakdown.Refunded)
}

func TestGetReconciliation_RepoError_500(t *testing.T) {
	srv, _, or := buildHandler(t, vendorUser())
	or.aggForErr = errors.New("db down")
	resp := do(t, http.MethodGet, srv.URL+"/api/merchant/reconciliation?period=2026-04", "")
	defer resp.Body.Close()
	assert.Equal(t, http.StatusInternalServerError, resp.StatusCode)
}

// =========================================================================
// GET /api/merchant/settlements
// =========================================================================

func TestListMerchantSettlements_Unauthenticated(t *testing.T) {
	srv, _, _ := buildHandler(t, nil)
	resp := do(t, http.MethodGet, srv.URL+"/api/merchant/settlements", "")
	defer resp.Body.Close()
	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
}

func TestListMerchantSettlements_WrongRole(t *testing.T) {
	srv, _, _ := buildHandler(t, &identity.User{ID: "e-1", Role: identity.RoleEmployee})
	resp := do(t, http.MethodGet, srv.URL+"/api/merchant/settlements", "")
	defer resp.Body.Close()
	assert.Equal(t, http.StatusForbidden, resp.StatusCode)
}

func TestListMerchantSettlements_OK(t *testing.T) {
	srv, sr, _ := buildHandler(t, vendorUser())
	sr.byVendor = []*settlement.Settlement{
		aprilSettlement(settlementID, vendorID, settlement.StatusClosed),
	}
	resp := do(t, http.MethodGet, srv.URL+"/api/merchant/settlements", "")
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	var out struct {
		Items []struct {
			ID         string `json:"id"`
			VendorID   string `json:"vendor_id"`
			Status     string `json:"status"`
			GrossMinor int64  `json:"gross_minor"`
			ClosedBy   string `json:"closed_by"`
		} `json:"items"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&out))
	require.Len(t, out.Items, 1)
	assert.Equal(t, settlementID, out.Items[0].ID)
	assert.Equal(t, vendorID, out.Items[0].VendorID)
	assert.Equal(t, "closed", out.Items[0].Status)
	assert.Equal(t, int64(26000), out.Items[0].GrossMinor)
	assert.Equal(t, "a-1", out.Items[0].ClosedBy)
}

func TestListMerchantSettlements_Empty(t *testing.T) {
	srv, _, _ := buildHandler(t, vendorUser())
	resp := do(t, http.MethodGet, srv.URL+"/api/merchant/settlements", "")
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	var out struct {
		Items []any `json:"items"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&out))
	assert.Empty(t, out.Items)
}

func TestListMerchantSettlements_RepoError_500(t *testing.T) {
	srv, sr, _ := buildHandler(t, vendorUser())
	sr.listVendErr = errors.New("db down")
	resp := do(t, http.MethodGet, srv.URL+"/api/merchant/settlements", "")
	defer resp.Body.Close()
	assert.Equal(t, http.StatusInternalServerError, resp.StatusCode)
}

// =========================================================================
// GET /api/merchant/settlements/{id}
// =========================================================================

func TestGetMerchantSettlement_Unauthenticated(t *testing.T) {
	srv, _, _ := buildHandler(t, nil)
	resp := do(t, http.MethodGet, srv.URL+"/api/merchant/settlements/"+settlementID, "")
	defer resp.Body.Close()
	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
}

func TestGetMerchantSettlement_InvalidUUID_422(t *testing.T) {
	srv, _, _ := buildHandler(t, vendorUser())
	resp := do(t, http.MethodGet, srv.URL+"/api/merchant/settlements/not-a-uuid", "")
	defer resp.Body.Close()
	assert.Equal(t, http.StatusUnprocessableEntity, resp.StatusCode)
}

func TestGetMerchantSettlement_NotFound(t *testing.T) {
	srv, _, _ := buildHandler(t, vendorUser()) // settlement not seeded
	resp := do(t, http.MethodGet, srv.URL+"/api/merchant/settlements/"+settlementID, "")
	defer resp.Body.Close()
	assert.Equal(t, http.StatusNotFound, resp.StatusCode)
}

func TestGetMerchantSettlement_WrongVendor_NotFound(t *testing.T) {
	srv, sr, _ := buildHandler(t, vendorUser())
	// Settlement exists but belongs to a different vendor → service maps the
	// ownership mismatch to ErrSettlementNotFound (404), not 403.
	sr.byID[settlementID] = aprilSettlement(settlementID, "someone-else", settlement.StatusClosed)
	resp := do(t, http.MethodGet, srv.URL+"/api/merchant/settlements/"+settlementID, "")
	defer resp.Body.Close()
	assert.Equal(t, http.StatusNotFound, resp.StatusCode)
}

func TestGetMerchantSettlement_OK(t *testing.T) {
	srv, sr, or := buildHandler(t, vendorUser())
	sr.byID[settlementID] = aprilSettlement(settlementID, vendorID, settlement.StatusClosed)
	or.lines = []*settlement.SettlementOrderLine{
		{
			OrderID:         "o-1",
			SupplyDate:      time.Date(2026, time.April, 3, 0, 0, 0, 0, time.UTC),
			Status:          "picked_up",
			TotalPriceMinor: 12000,
			PortionCount:    2,
		},
	}
	resp := do(t, http.MethodGet, srv.URL+"/api/merchant/settlements/"+settlementID, "")
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	var out struct {
		Settlement struct {
			ID         string `json:"id"`
			VendorID   string `json:"vendor_id"`
			GrossMinor int64  `json:"gross_minor"`
		} `json:"settlement"`
		Orders []struct {
			OrderID         string `json:"order_id"`
			SupplyDate      string `json:"supply_date"`
			Status          string `json:"status"`
			TotalPriceMinor int64  `json:"total_price_minor"`
			PortionCount    int    `json:"portion_count"`
		} `json:"orders"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&out))
	assert.Equal(t, settlementID, out.Settlement.ID)
	assert.Equal(t, vendorID, out.Settlement.VendorID)
	assert.Equal(t, int64(26000), out.Settlement.GrossMinor)
	require.Len(t, out.Orders, 1)
	assert.Equal(t, "o-1", out.Orders[0].OrderID)
	assert.Equal(t, "2026-04-03", out.Orders[0].SupplyDate)
	assert.Equal(t, "picked_up", out.Orders[0].Status)
	assert.Equal(t, int64(12000), out.Orders[0].TotalPriceMinor) // whole NTD
	assert.Equal(t, 2, out.Orders[0].PortionCount)
}

func TestGetMerchantSettlement_LinesRepoError_500(t *testing.T) {
	srv, sr, or := buildHandler(t, vendorUser())
	sr.byID[settlementID] = aprilSettlement(settlementID, vendorID, settlement.StatusClosed)
	or.linesErr = errors.New("db down")
	resp := do(t, http.MethodGet, srv.URL+"/api/merchant/settlements/"+settlementID, "")
	defer resp.Body.Close()
	assert.Equal(t, http.StatusInternalServerError, resp.StatusCode)
}

// =========================================================================
// GET /api/admin/vendor-settlements
// =========================================================================

func TestListVendorSettlements_Unauthenticated(t *testing.T) {
	srv, _, _ := buildHandler(t, nil)
	resp := do(t, http.MethodGet, srv.URL+"/api/admin/vendor-settlements?period=2026-04", "")
	defer resp.Body.Close()
	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
}

func TestListVendorSettlements_WrongRole(t *testing.T) {
	srv, _, _ := buildHandler(t, vendorUser()) // vendor operator is not admin
	resp := do(t, http.MethodGet, srv.URL+"/api/admin/vendor-settlements?period=2026-04", "")
	defer resp.Body.Close()
	assert.Equal(t, http.StatusForbidden, resp.StatusCode)
}

func TestListVendorSettlements_BadPeriod(t *testing.T) {
	srv, _, _ := buildHandler(t, adminUser())
	resp := do(t, http.MethodGet, srv.URL+"/api/admin/vendor-settlements?period=2026-13-99", "")
	defer resp.Body.Close()
	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
}

func TestListVendorSettlements_OK(t *testing.T) {
	srv, sr, _ := buildHandler(t, adminUser())
	sr.byPeriod = []*settlement.Settlement{
		aprilSettlement(settlementID, vendorID, settlement.StatusClosed),
		aprilSettlement(otherID, "v-other", settlement.StatusVoid),
	}
	resp := do(t, http.MethodGet, srv.URL+"/api/admin/vendor-settlements?period=2026-04", "")
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	var out struct {
		Items []struct {
			ID       string `json:"id"`
			VendorID string `json:"vendor_id"`
			Status   string `json:"status"`
		} `json:"items"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&out))
	require.Len(t, out.Items, 2)
	assert.Equal(t, "closed", out.Items[0].Status)
	assert.Equal(t, "void", out.Items[1].Status)
}

func TestListVendorSettlements_RepoError_500(t *testing.T) {
	srv, sr, _ := buildHandler(t, adminUser())
	sr.listPerErr = errors.New("db down")
	resp := do(t, http.MethodGet, srv.URL+"/api/admin/vendor-settlements?period=2026-04", "")
	defer resp.Body.Close()
	assert.Equal(t, http.StatusInternalServerError, resp.StatusCode)
}

// =========================================================================
// POST /api/admin/vendor-settlements/close
// =========================================================================

func TestCloseSettlement_Unauthenticated(t *testing.T) {
	srv, _, _ := buildHandler(t, nil)
	resp := do(t, http.MethodPost, srv.URL+"/api/admin/vendor-settlements/close",
		`{"period_start":"2026-04-01","period_end":"2026-04-30"}`)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
}

func TestCloseSettlement_WrongRole(t *testing.T) {
	srv, _, _ := buildHandler(t, vendorUser())
	resp := do(t, http.MethodPost, srv.URL+"/api/admin/vendor-settlements/close",
		`{"period_start":"2026-04-01","period_end":"2026-04-30"}`)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusForbidden, resp.StatusCode)
}

func TestCloseSettlement_BadPeriodStart(t *testing.T) {
	srv, _, _ := buildHandler(t, adminUser())
	resp := do(t, http.MethodPost, srv.URL+"/api/admin/vendor-settlements/close",
		`{"period_start":"nope","period_end":"2026-04-30"}`)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
}

func TestCloseSettlement_BadPeriodEnd(t *testing.T) {
	srv, _, _ := buildHandler(t, adminUser())
	resp := do(t, http.MethodPost, srv.URL+"/api/admin/vendor-settlements/close",
		`{"period_start":"2026-04-01","period_end":"nope"}`)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
}

func TestCloseSettlement_InvalidPeriod_400(t *testing.T) {
	srv, _, _ := buildHandler(t, adminUser())
	// start > end → service returns ErrInvalidPeriod → 400 (before any pool use)
	resp := do(t, http.MethodPost, srv.URL+"/api/admin/vendor-settlements/close",
		`{"period_start":"2026-04-30","period_end":"2026-04-01"}`)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
}

func TestCloseSettlement_NoOrders_400(t *testing.T) {
	srv, _, or := buildHandler(t, adminUser())
	or.aggByVendor = nil // no vendors with orders → ErrNoOrdersInPeriod (400)
	resp := do(t, http.MethodPost, srv.URL+"/api/admin/vendor-settlements/close",
		`{"period_start":"2026-04-01","period_end":"2026-04-30"}`)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
}

func TestCloseSettlement_AggregateRepoError_500(t *testing.T) {
	srv, _, or := buildHandler(t, adminUser())
	or.aggByErr = errors.New("db down") // returns before pgx.BeginFunc → 500
	resp := do(t, http.MethodPost, srv.URL+"/api/admin/vendor-settlements/close",
		`{"period_start":"2026-04-01","period_end":"2026-04-30"}`)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusInternalServerError, resp.StatusCode)
}

func TestCloseSettlement_OK_201(t *testing.T) {
	srv, _, or := buildHandler(t, adminUser())
	or.aggByVendor = []*settlement.VendorAggregate{
		{VendorID: vendorID, OrderCount: 3, PortionCount: 7, GrossMinor: 26000, OrderIDs: []string{"o-1"}},
		{VendorID: "v-other", OrderCount: 1, PortionCount: 2, GrossMinor: 8000, OrderIDs: []string{"o-2"}},
	}
	resp := do(t, http.MethodPost, srv.URL+"/api/admin/vendor-settlements/close",
		`{"period_start":"2026-04-01","period_end":"2026-04-30"}`)
	defer resp.Body.Close()
	require.Equal(t, http.StatusCreated, resp.StatusCode)

	var out struct {
		Items []struct {
			VendorID   string `json:"vendor_id"`
			Status     string `json:"status"`
			GrossMinor int64  `json:"gross_minor"`
		} `json:"items"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&out))
	require.Len(t, out.Items, 2, "one settlement row per vendor with orders")
	assert.Equal(t, "closed", out.Items[0].Status)
	assert.Equal(t, vendorID, out.Items[0].VendorID)
	assert.Equal(t, int64(26000), out.Items[0].GrossMinor) // whole NTD
}

// =========================================================================
// POST /api/admin/vendor-settlements/{id}/void
// =========================================================================

func TestVoidSettlement_Unauthenticated(t *testing.T) {
	srv, _, _ := buildHandler(t, nil)
	resp := do(t, http.MethodPost, srv.URL+"/api/admin/vendor-settlements/"+settlementID+"/void", "")
	defer resp.Body.Close()
	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
}

func TestVoidSettlement_WrongRole(t *testing.T) {
	srv, _, _ := buildHandler(t, vendorUser())
	resp := do(t, http.MethodPost, srv.URL+"/api/admin/vendor-settlements/"+settlementID+"/void", "")
	defer resp.Body.Close()
	assert.Equal(t, http.StatusForbidden, resp.StatusCode)
}

func TestVoidSettlement_InvalidUUID_422(t *testing.T) {
	srv, _, _ := buildHandler(t, adminUser())
	resp := do(t, http.MethodPost, srv.URL+"/api/admin/vendor-settlements/not-a-uuid/void", "")
	defer resp.Body.Close()
	assert.Equal(t, http.StatusUnprocessableEntity, resp.StatusCode)
}

func TestVoidSettlement_NotFound(t *testing.T) {
	srv, _, _ := buildHandler(t, adminUser()) // settlement not seeded → ErrSettlementNotFound (404)
	resp := do(t, http.MethodPost, srv.URL+"/api/admin/vendor-settlements/"+settlementID+"/void", "")
	defer resp.Body.Close()
	assert.Equal(t, http.StatusNotFound, resp.StatusCode)
}

func TestVoidSettlement_GetRepoError_500(t *testing.T) {
	srv, sr, _ := buildHandler(t, adminUser())
	sr.getErr = errors.New("db down")
	resp := do(t, http.MethodPost, srv.URL+"/api/admin/vendor-settlements/"+settlementID+"/void", "")
	defer resp.Body.Close()
	assert.Equal(t, http.StatusInternalServerError, resp.StatusCode)
}

func TestVoidSettlement_NotClosed_409(t *testing.T) {
	srv, sr, _ := buildHandler(t, adminUser())
	// Already void → ErrInvalidTransition → 409 (returns before pgx.BeginFunc).
	sr.byID[settlementID] = aprilSettlement(settlementID, vendorID, settlement.StatusVoid)
	resp := do(t, http.MethodPost, srv.URL+"/api/admin/vendor-settlements/"+settlementID+"/void", "")
	defer resp.Body.Close()
	assert.Equal(t, http.StatusConflict, resp.StatusCode)
}

func TestVoidSettlement_OK_204(t *testing.T) {
	srv, sr, _ := buildHandler(t, adminUser())
	sr.byID[settlementID] = aprilSettlement(settlementID, vendorID, settlement.StatusClosed)
	resp := do(t, http.MethodPost, srv.URL+"/api/admin/vendor-settlements/"+settlementID+"/void", "")
	defer resp.Body.Close()
	assert.Equal(t, http.StatusNoContent, resp.StatusCode)
}
