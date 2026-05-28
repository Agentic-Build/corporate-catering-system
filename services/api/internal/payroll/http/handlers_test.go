package payrollhttp_test

import (
	"context"
	"encoding/json"
	"errors"
	audit "github.com/Agentic-Build/corporate-catering-system/services/api/internal/platform/audit"
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
	"github.com/Agentic-Build/corporate-catering-system/services/api/internal/order"
	"github.com/Agentic-Build/corporate-catering-system/services/api/internal/payroll"
	payrollhttp "github.com/Agentic-Build/corporate-catering-system/services/api/internal/payroll/http"
)

const (
	batchID    = "11111111-1111-1111-1111-111111111111"
	entryID    = "22222222-2222-2222-2222-222222222222"
	disputeID  = "33333333-3333-3333-3333-333333333333"
	orderID    = "44444444-4444-4444-4444-444444444444"
	excID      = "55555555-5555-5555-5555-555555555555"
	adminUser  = "admin-1"
	employeeID = "emp-1"
)

func sp(s string) *string { return &s }

// Fakes cover the four payroll repos plus CurrentLinesLister, the order
// repos, audit + outbox. Pool=fakeBeginner so the pgx.BeginFunc write paths
// (BuildDraft/Lock/ResolveDispute/FlagException/ResolveException) run without
// a real DB.

type fakeBatchRepo struct {
	byID    map[string]*payroll.Batch
	byPer   *payroll.Batch
	perErr  error
	list    []*payroll.Batch
	listErr error
	getErr  error
}

func (r *fakeBatchRepo) Create(context.Context, *payroll.Batch) error           { return nil }
func (r *fakeBatchRepo) CreateTx(context.Context, pgx.Tx, *payroll.Batch) error { return nil }
func (r *fakeBatchRepo) GetByID(_ context.Context, id string) (*payroll.Batch, error) {
	if r.getErr != nil {
		return nil, r.getErr
	}
	if b, ok := r.byID[id]; ok {
		return b, nil
	}
	return nil, payroll.ErrBatchNotFound
}
func (r *fakeBatchRepo) GetByPeriod(context.Context, time.Time, time.Time) (*payroll.Batch, error) {
	if r.perErr != nil {
		return nil, r.perErr
	}
	if r.byPer != nil {
		return r.byPer, nil
	}
	return nil, payroll.ErrBatchNotFound
}
func (r *fakeBatchRepo) UpdateStatusTx(context.Context, pgx.Tx, string, payroll.BatchStatus, payroll.BatchStatus, *string) error {
	return nil
}
func (r *fakeBatchRepo) SetExportInfoTx(context.Context, pgx.Tx, string, string, time.Time) error {
	return nil
}
func (r *fakeBatchRepo) List(context.Context, []payroll.BatchStatus) ([]*payroll.Batch, error) {
	return r.list, r.listErr
}

type fakeEntryRepo struct {
	byID         map[string]*payroll.Entry
	listByBatch  []*payroll.Entry
	batchErr     error
	byUser       []*payroll.EmployeeEntry
	userErr      error
	findOrderID  string
	findOrderErr error
}

func (r *fakeEntryRepo) CreateTx(context.Context, pgx.Tx, *payroll.Entry) error { return nil }
func (r *fakeEntryRepo) GetByID(_ context.Context, id string) (*payroll.Entry, error) {
	if e, ok := r.byID[id]; ok {
		return e, nil
	}
	return nil, payroll.ErrEntryNotFound
}
func (r *fakeEntryRepo) ListByBatch(context.Context, string) ([]*payroll.Entry, error) {
	return r.listByBatch, r.batchErr
}
func (r *fakeEntryRepo) IncrementRefundedTx(context.Context, pgx.Tx, string, int64) error {
	return nil
}
func (r *fakeEntryRepo) FindByOrderForUser(context.Context, string, string) (string, error) {
	if r.findOrderErr != nil {
		return "", r.findOrderErr
	}
	return r.findOrderID, nil
}
func (r *fakeEntryRepo) ListByUser(context.Context, string) ([]*payroll.EmployeeEntry, error) {
	return r.byUser, r.userErr
}

type fakeDisputeRepo struct {
	byID      map[string]*payroll.Dispute
	createErr error
	byStatus  []*payroll.Dispute
	statusErr error
	byUser    []*payroll.Dispute
	userErr   error
	created   *payroll.Dispute
}

func (r *fakeDisputeRepo) Create(_ context.Context, d *payroll.Dispute) error {
	if r.createErr != nil {
		return r.createErr
	}
	d.ID = disputeID
	d.Status = payroll.DisputeStatusOpen
	r.created = d
	return nil
}
func (r *fakeDisputeRepo) GetByID(_ context.Context, id string) (*payroll.Dispute, error) {
	if d, ok := r.byID[id]; ok {
		return d, nil
	}
	return nil, payroll.ErrDisputeNotFound
}
func (r *fakeDisputeRepo) UpdateStatusTx(context.Context, pgx.Tx, string, payroll.DisputeStatus, *string, string, int64) error {
	return nil
}
func (r *fakeDisputeRepo) ListByStatus(context.Context, []payroll.DisputeStatus) ([]*payroll.Dispute, error) {
	return r.byStatus, r.statusErr
}
func (r *fakeDisputeRepo) ListByUser(context.Context, string) ([]*payroll.Dispute, error) {
	return r.byUser, r.userErr
}

type fakeExceptionRepo struct {
	byID        map[string]*payroll.Exception
	listByBatch []*payroll.Exception
	listErr     error
	upsertErr   error
}

func (r *fakeExceptionRepo) UpsertDepartedTx(context.Context, pgx.Tx, string) error { return nil }
func (r *fakeExceptionRepo) UpsertDeparted(context.Context, string) error           { return r.upsertErr }
func (r *fakeExceptionRepo) Create(context.Context, *payroll.Exception) error       { return nil }
func (r *fakeExceptionRepo) GetByID(_ context.Context, id string) (*payroll.Exception, error) {
	if e, ok := r.byID[id]; ok {
		return e, nil
	}
	return nil, payroll.ErrExceptionNotFound
}
func (r *fakeExceptionRepo) ListByBatch(context.Context, string) ([]*payroll.Exception, error) {
	return r.listByBatch, r.listErr
}
func (r *fakeExceptionRepo) Resolve(context.Context, string, payroll.ExceptionStatus, string, string) error {
	return nil
}

type fakeCurrentLinesRepo struct {
	lines []payroll.CurrentPayrollLine
	err   error
}

func (r *fakeCurrentLinesRepo) ListCurrentLines(context.Context, string) ([]payroll.CurrentPayrollLine, error) {
	return r.lines, r.err
}

// fakeOrderRepo backs the BuildDraft aggregation and the ResolveDispute refund
// branch (GetByID). UpdateStatusTx (OrderTx) ignores the tx.
type fakeOrderRepo struct {
	byID    map[string]*order.Order
	picked  []*order.Order
	listErr error
}

func (r *fakeOrderRepo) GetByID(_ context.Context, id string) (*order.Order, error) {
	if o, ok := r.byID[id]; ok {
		return o, nil
	}
	return nil, order.ErrOrderNotFound
}
func (r *fakeOrderRepo) ListPickedOrNoShowInPeriod(context.Context, time.Time, time.Time) ([]*order.Order, error) {
	return r.picked, r.listErr
}
func (r *fakeOrderRepo) UpdateStatusTx(context.Context, pgx.Tx, string, order.Status, order.Status) error {
	return nil
}

type fakeAudit struct{}

func (fakeAudit) WriteTx(context.Context, pgx.Tx, audit.Entry) error {
	return nil
}

type fakeOutbox struct{}

func (fakeOutbox) AppendTx(context.Context, pgx.Tx, string, string, string, map[string]any, map[string]any) error {
	return nil
}

// fakeBeginner stands in for *pgxpool.Pool. It hands the write closure a no-op
// pgx.Tx; the repo fakes ignore the tx, so the BeginFunc write paths run without
// a real DB. Query satisfies the interface for compilation only — handler tests
// set the CurrentLines fake repo, which short-circuits this fallback.
type fakeBeginner struct{}

func (fakeBeginner) Begin(context.Context) (pgx.Tx, error) { return fakeTx{}, nil }
func (fakeBeginner) Query(context.Context, string, ...any) (pgx.Rows, error) {
	return nil, nil
}

type fakeTx struct{ pgx.Tx }

func (fakeTx) Commit(context.Context) error   { return nil }
func (fakeTx) Rollback(context.Context) error { return nil }

type fakes struct {
	batches  *fakeBatchRepo
	entries  *fakeEntryRepo
	disputes *fakeDisputeRepo
	excs     *fakeExceptionRepo
	lines    *fakeCurrentLinesRepo
	orders   *fakeOrderRepo
}

func adminUserObj() *identity.User {
	return &identity.User{ID: adminUser, Role: identity.RoleWelfareAdmin}
}

func employeeUserObj() *identity.User {
	return &identity.User{ID: employeeID, Role: identity.RoleEmployee}
}

func buildHandler(t *testing.T, user *identity.User) (*httptest.Server, *fakes) {
	t.Helper()
	f := &fakes{
		batches:  &fakeBatchRepo{byID: map[string]*payroll.Batch{}},
		entries:  &fakeEntryRepo{byID: map[string]*payroll.Entry{}},
		disputes: &fakeDisputeRepo{byID: map[string]*payroll.Dispute{}},
		excs:     &fakeExceptionRepo{byID: map[string]*payroll.Exception{}},
		lines:    &fakeCurrentLinesRepo{},
		orders:   &fakeOrderRepo{byID: map[string]*order.Order{}},
	}
	api := &payrollhttp.API{Svc: &payroll.Service{
		Pool:         fakeBeginner{},
		Batches:      f.batches,
		Entries:      f.entries,
		Disputes:     f.disputes,
		Exceptions:   f.excs,
		CurrentLines: f.lines,
		Orders:       f.orders,
		OrderTx:      f.orders,
		Audit:        fakeAudit{},
		Outbox:       fakeOutbox{},
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
	return srv, f
}

func do(t *testing.T, method, url, body string) *http.Response {
	t.Helper()
	req, err := http.NewRequest(method, url, strings.NewReader(body))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	return resp
}

// === createBatch  POST /api/admin/payroll/batches ===

func TestCreateBatch_Unauthenticated(t *testing.T) {
	srv, _ := buildHandler(t, nil)
	resp := do(t, http.MethodPost, srv.URL+"/api/admin/payroll/batches",
		`{"period_start":"2026-04-01","period_end":"2026-04-30"}`)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
}

func TestCreateBatch_WrongRole(t *testing.T) {
	srv, _ := buildHandler(t, employeeUserObj())
	resp := do(t, http.MethodPost, srv.URL+"/api/admin/payroll/batches",
		`{"period_start":"2026-04-01","period_end":"2026-04-30"}`)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusForbidden, resp.StatusCode)
}

func TestCreateBatch_InvalidPeriodStart_400(t *testing.T) {
	srv, _ := buildHandler(t, adminUserObj())
	resp := do(t, http.MethodPost, srv.URL+"/api/admin/payroll/batches",
		`{"period_start":"not-a-date","period_end":"2026-04-30"}`)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
}

func TestCreateBatch_InvalidPeriodEnd_400(t *testing.T) {
	srv, _ := buildHandler(t, adminUserObj())
	resp := do(t, http.MethodPost, srv.URL+"/api/admin/payroll/batches",
		`{"period_start":"2026-04-01","period_end":"nope"}`)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
}

func TestCreateBatch_StartAfterEnd_500(t *testing.T) {
	// period_start > period_end returns a generic error (not a sentinel) → 500.
	srv, _ := buildHandler(t, adminUserObj())
	resp := do(t, http.MethodPost, srv.URL+"/api/admin/payroll/batches",
		`{"period_start":"2026-04-30","period_end":"2026-04-01"}`)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusInternalServerError, resp.StatusCode)
}

func TestCreateBatch_DuplicatePeriod_409(t *testing.T) {
	srv, f := buildHandler(t, adminUserObj())
	f.batches.byPer = &payroll.Batch{ID: batchID, Status: payroll.BatchStatusDraft}
	resp := do(t, http.MethodPost, srv.URL+"/api/admin/payroll/batches",
		`{"period_start":"2026-04-01","period_end":"2026-04-30"}`)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusConflict, resp.StatusCode)
}

func TestCreateBatch_GetByPeriodError_500(t *testing.T) {
	srv, f := buildHandler(t, adminUserObj())
	f.batches.perErr = errors.New("db down")
	resp := do(t, http.MethodPost, srv.URL+"/api/admin/payroll/batches",
		`{"period_start":"2026-04-01","period_end":"2026-04-30"}`)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusInternalServerError, resp.StatusCode)
}

func TestCreateBatch_OK_201(t *testing.T) {
	srv, f := buildHandler(t, adminUserObj())
	f.orders.picked = []*order.Order{
		{ID: orderID, UserID: employeeID, TotalPriceMinor: 12000, Status: order.StatusPickedUp},
	}
	resp := do(t, http.MethodPost, srv.URL+"/api/admin/payroll/batches",
		`{"period_start":"2026-04-01","period_end":"2026-04-30"}`)
	defer resp.Body.Close()
	require.Equal(t, http.StatusCreated, resp.StatusCode)

	var out struct {
		Batch struct {
			PeriodStart string `json:"period_start"`
			PeriodEnd   string `json:"period_end"`
			Status      string `json:"status"`
		} `json:"batch"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&out))
	assert.Equal(t, "2026-04-01", out.Batch.PeriodStart)
	assert.Equal(t, "2026-04-30", out.Batch.PeriodEnd)
	assert.Equal(t, "draft", out.Batch.Status)
}

// === listBatches  GET /api/admin/payroll/batches ===

func TestListBatches_Unauthenticated(t *testing.T) {
	srv, _ := buildHandler(t, nil)
	resp := do(t, http.MethodGet, srv.URL+"/api/admin/payroll/batches", "")
	defer resp.Body.Close()
	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
}

func TestListBatches_WrongRole(t *testing.T) {
	srv, _ := buildHandler(t, employeeUserObj())
	resp := do(t, http.MethodGet, srv.URL+"/api/admin/payroll/batches", "")
	defer resp.Body.Close()
	assert.Equal(t, http.StatusForbidden, resp.StatusCode)
}

func TestListBatches_OK(t *testing.T) {
	srv, f := buildHandler(t, adminUserObj())
	lockedAt := time.Date(2026, 5, 1, 8, 0, 0, 0, time.UTC)
	lockedBy := adminUser
	f.batches.list = []*payroll.Batch{
		{
			ID:          batchID,
			PeriodStart: time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC),
			PeriodEnd:   time.Date(2026, 4, 30, 0, 0, 0, 0, time.UTC),
			Status:      payroll.BatchStatusLocked,
			LockedAt:    &lockedAt,
			LockedBy:    &lockedBy,
		},
	}
	resp := do(t, http.MethodGet, srv.URL+"/api/admin/payroll/batches?status=locked", "")
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	var out struct {
		Items []struct {
			ID          string  `json:"id"`
			PeriodStart string  `json:"period_start"`
			PeriodEnd   string  `json:"period_end"`
			Status      string  `json:"status"`
			LockedBy    *string `json:"locked_by"`
		} `json:"items"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&out))
	require.Len(t, out.Items, 1)
	assert.Equal(t, batchID, out.Items[0].ID)
	assert.Equal(t, "2026-04-01", out.Items[0].PeriodStart)
	assert.Equal(t, "2026-04-30", out.Items[0].PeriodEnd)
	assert.Equal(t, "locked", out.Items[0].Status)
	require.NotNil(t, out.Items[0].LockedBy)
	assert.Equal(t, adminUser, *out.Items[0].LockedBy)
}

func TestListBatches_Empty(t *testing.T) {
	srv, _ := buildHandler(t, adminUserObj())
	resp := do(t, http.MethodGet, srv.URL+"/api/admin/payroll/batches", "")
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	var out struct {
		Items []any `json:"items"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&out))
	assert.Empty(t, out.Items)
}

func TestListBatches_RepoError_500(t *testing.T) {
	srv, f := buildHandler(t, adminUserObj())
	f.batches.listErr = errors.New("db down")
	resp := do(t, http.MethodGet, srv.URL+"/api/admin/payroll/batches", "")
	defer resp.Body.Close()
	assert.Equal(t, http.StatusInternalServerError, resp.StatusCode)
}

// === getBatch  GET /api/admin/payroll/batches/{id} ===

func TestGetBatch_Unauthenticated(t *testing.T) {
	srv, _ := buildHandler(t, nil)
	resp := do(t, http.MethodGet, srv.URL+"/api/admin/payroll/batches/"+batchID, "")
	defer resp.Body.Close()
	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
}

func TestGetBatch_InvalidUUID_422(t *testing.T) {
	// path param has format:"uuid" → huma validates before handler.
	srv, _ := buildHandler(t, adminUserObj())
	resp := do(t, http.MethodGet, srv.URL+"/api/admin/payroll/batches/not-a-uuid", "")
	defer resp.Body.Close()
	assert.Equal(t, http.StatusUnprocessableEntity, resp.StatusCode)
}

func TestGetBatch_NotFound_404(t *testing.T) {
	srv, _ := buildHandler(t, adminUserObj()) // batch not seeded
	resp := do(t, http.MethodGet, srv.URL+"/api/admin/payroll/batches/"+batchID, "")
	defer resp.Body.Close()
	assert.Equal(t, http.StatusNotFound, resp.StatusCode)
}

func TestGetBatch_OK(t *testing.T) {
	srv, f := buildHandler(t, adminUserObj())
	f.batches.byID[batchID] = &payroll.Batch{
		ID:          batchID,
		PeriodStart: time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC),
		PeriodEnd:   time.Date(2026, 4, 30, 0, 0, 0, 0, time.UTC),
		Status:      payroll.BatchStatusDraft,
	}
	f.entries.listByBatch = []*payroll.Entry{
		{ID: entryID, BatchID: batchID, UserID: employeeID, OrderIDs: []string{orderID}, AmountMinor: 12000, RefundedMinor: 0},
	}
	resp := do(t, http.MethodGet, srv.URL+"/api/admin/payroll/batches/"+batchID, "")
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	var out struct {
		Batch struct {
			ID     string `json:"id"`
			Status string `json:"status"`
		} `json:"batch"`
		Entries []struct {
			ID          string   `json:"id"`
			UserID      string   `json:"user_id"`
			OrderIDs    []string `json:"order_ids"`
			AmountMinor int64    `json:"amount_minor"`
		} `json:"entries"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&out))
	assert.Equal(t, batchID, out.Batch.ID)
	assert.Equal(t, "draft", out.Batch.Status)
	require.Len(t, out.Entries, 1)
	assert.Equal(t, entryID, out.Entries[0].ID)
	assert.Equal(t, int64(12000), out.Entries[0].AmountMinor)
	assert.Equal(t, []string{orderID}, out.Entries[0].OrderIDs)
}

func TestGetBatch_EntriesError_500(t *testing.T) {
	srv, f := buildHandler(t, adminUserObj())
	f.batches.byID[batchID] = &payroll.Batch{ID: batchID, Status: payroll.BatchStatusDraft}
	f.entries.batchErr = errors.New("db down")
	resp := do(t, http.MethodGet, srv.URL+"/api/admin/payroll/batches/"+batchID, "")
	defer resp.Body.Close()
	assert.Equal(t, http.StatusInternalServerError, resp.StatusCode)
}

// === lockBatch  POST /api/admin/payroll/batches/{id}/lock ===

func TestLockBatch_Unauthenticated(t *testing.T) {
	srv, _ := buildHandler(t, nil)
	resp := do(t, http.MethodPost, srv.URL+"/api/admin/payroll/batches/"+batchID+"/lock", "")
	defer resp.Body.Close()
	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
}

func TestLockBatch_WrongRole(t *testing.T) {
	srv, _ := buildHandler(t, employeeUserObj())
	resp := do(t, http.MethodPost, srv.URL+"/api/admin/payroll/batches/"+batchID+"/lock", "")
	defer resp.Body.Close()
	assert.Equal(t, http.StatusForbidden, resp.StatusCode)
}

func TestLockBatch_InvalidUUID_422(t *testing.T) {
	srv, _ := buildHandler(t, adminUserObj())
	resp := do(t, http.MethodPost, srv.URL+"/api/admin/payroll/batches/not-a-uuid/lock", "")
	defer resp.Body.Close()
	assert.Equal(t, http.StatusUnprocessableEntity, resp.StatusCode)
}

func TestLockBatch_NotFound_404(t *testing.T) {
	srv, _ := buildHandler(t, adminUserObj()) // batch not seeded
	resp := do(t, http.MethodPost, srv.URL+"/api/admin/payroll/batches/"+batchID+"/lock", "")
	defer resp.Body.Close()
	assert.Equal(t, http.StatusNotFound, resp.StatusCode)
}

func TestLockBatch_AlreadyLocked_409(t *testing.T) {
	srv, f := buildHandler(t, adminUserObj())
	f.batches.byID[batchID] = &payroll.Batch{ID: batchID, Status: payroll.BatchStatusLocked}
	resp := do(t, http.MethodPost, srv.URL+"/api/admin/payroll/batches/"+batchID+"/lock", "")
	defer resp.Body.Close()
	assert.Equal(t, http.StatusConflict, resp.StatusCode)
}

func TestLockBatch_OK_204(t *testing.T) {
	srv, f := buildHandler(t, adminUserObj())
	f.batches.byID[batchID] = &payroll.Batch{
		ID:          batchID,
		PeriodStart: time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC),
		PeriodEnd:   time.Date(2026, 4, 30, 0, 0, 0, 0, time.UTC),
		Status:      payroll.BatchStatusDraft,
	}
	resp := do(t, http.MethodPost, srv.URL+"/api/admin/payroll/batches/"+batchID+"/lock", "")
	defer resp.Body.Close()
	assert.Equal(t, http.StatusNoContent, resp.StatusCode)
}

// === listDisputes  GET /api/admin/payroll/disputes ===

func TestListDisputes_Unauthenticated(t *testing.T) {
	srv, _ := buildHandler(t, nil)
	resp := do(t, http.MethodGet, srv.URL+"/api/admin/payroll/disputes", "")
	defer resp.Body.Close()
	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
}

func TestListDisputes_WrongRole(t *testing.T) {
	srv, _ := buildHandler(t, employeeUserObj())
	resp := do(t, http.MethodGet, srv.URL+"/api/admin/payroll/disputes", "")
	defer resp.Body.Close()
	assert.Equal(t, http.StatusForbidden, resp.StatusCode)
}

func TestListDisputes_OK(t *testing.T) {
	srv, f := buildHandler(t, adminUserObj())
	resolvedBy := adminUser
	resolvedAt := time.Date(2026, 5, 2, 9, 0, 0, 0, time.UTC)
	f.disputes.byStatus = []*payroll.Dispute{
		{
			ID:          disputeID,
			EntryID:     sp(entryID),
			OrderID:     orderID,
			OpenedBy:    employeeID,
			Reason:      "missing dessert",
			Status:      payroll.DisputeStatusResolvedRefund,
			Resolution:  "verified",
			ResolvedBy:  &resolvedBy,
			ResolvedAt:  &resolvedAt,
			RefundMinor: 12000,
		},
	}
	resp := do(t, http.MethodGet, srv.URL+"/api/admin/payroll/disputes?status=resolved_refund", "")
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	var out struct {
		Items []struct {
			ID          string  `json:"id"`
			Status      string  `json:"status"`
			RefundMinor int64   `json:"refund_minor"`
			ResolvedBy  *string `json:"resolved_by"`
		} `json:"items"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&out))
	require.Len(t, out.Items, 1)
	assert.Equal(t, disputeID, out.Items[0].ID)
	assert.Equal(t, "resolved_refund", out.Items[0].Status)
	assert.Equal(t, int64(12000), out.Items[0].RefundMinor)
	require.NotNil(t, out.Items[0].ResolvedBy)
	assert.Equal(t, adminUser, *out.Items[0].ResolvedBy)
}

func TestListDisputes_RepoError_500(t *testing.T) {
	srv, f := buildHandler(t, adminUserObj())
	f.disputes.statusErr = errors.New("db down")
	resp := do(t, http.MethodGet, srv.URL+"/api/admin/payroll/disputes", "")
	defer resp.Body.Close()
	assert.Equal(t, http.StatusInternalServerError, resp.StatusCode)
}

// === resolveDispute  POST /api/admin/payroll/disputes/{id}/resolve ===

func TestResolveDispute_Unauthenticated(t *testing.T) {
	srv, _ := buildHandler(t, nil)
	resp := do(t, http.MethodPost, srv.URL+"/api/admin/payroll/disputes/"+disputeID+"/resolve",
		`{"status":"resolved_reject","resolution":"no","refund_minor":0}`)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
}

func TestResolveDispute_WrongRole(t *testing.T) {
	srv, _ := buildHandler(t, employeeUserObj())
	resp := do(t, http.MethodPost, srv.URL+"/api/admin/payroll/disputes/"+disputeID+"/resolve",
		`{"status":"resolved_reject","resolution":"no","refund_minor":0}`)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusForbidden, resp.StatusCode)
}

func TestResolveDispute_InvalidUUID_422(t *testing.T) {
	srv, _ := buildHandler(t, adminUserObj())
	resp := do(t, http.MethodPost, srv.URL+"/api/admin/payroll/disputes/not-a-uuid/resolve",
		`{"status":"resolved_reject","resolution":"no","refund_minor":0}`)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusUnprocessableEntity, resp.StatusCode)
}

func TestResolveDispute_InvalidStatusEnum_422(t *testing.T) {
	// status enum is resolved_refund|resolved_reject → huma rejects others.
	srv, _ := buildHandler(t, adminUserObj())
	resp := do(t, http.MethodPost, srv.URL+"/api/admin/payroll/disputes/"+disputeID+"/resolve",
		`{"status":"cancelled","resolution":"x"}`)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusUnprocessableEntity, resp.StatusCode)
}

func TestResolveDispute_NotFound_404(t *testing.T) {
	srv, _ := buildHandler(t, adminUserObj()) // dispute not seeded
	resp := do(t, http.MethodPost, srv.URL+"/api/admin/payroll/disputes/"+disputeID+"/resolve",
		`{"status":"resolved_reject","resolution":"no","refund_minor":0}`)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusNotFound, resp.StatusCode)
}

func TestResolveDispute_NotOpen_409(t *testing.T) {
	srv, f := buildHandler(t, adminUserObj())
	f.disputes.byID[disputeID] = &payroll.Dispute{ID: disputeID, Status: payroll.DisputeStatusResolvedReject}
	resp := do(t, http.MethodPost, srv.URL+"/api/admin/payroll/disputes/"+disputeID+"/resolve",
		`{"status":"resolved_reject","resolution":"no","refund_minor":0}`)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusConflict, resp.StatusCode)
}

func TestResolveDispute_Reject_OK_204(t *testing.T) {
	srv, f := buildHandler(t, adminUserObj())
	f.disputes.byID[disputeID] = &payroll.Dispute{
		ID: disputeID, EntryID: sp(entryID), OrderID: orderID, Status: payroll.DisputeStatusOpen,
	}
	resp := do(t, http.MethodPost, srv.URL+"/api/admin/payroll/disputes/"+disputeID+"/resolve",
		`{"status":"resolved_reject","resolution":"not substantiated","refund_minor":0}`)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusNoContent, resp.StatusCode)
}

func TestResolveDispute_Refund_OK_204(t *testing.T) {
	srv, f := buildHandler(t, adminUserObj())
	f.disputes.byID[disputeID] = &payroll.Dispute{
		ID: disputeID, EntryID: sp(entryID), OrderID: orderID, Status: payroll.DisputeStatusOpen,
	}
	// Order picked_up with 12000 NTD; refund 5000 (whole NTD) is within bounds.
	f.orders.byID[orderID] = &order.Order{ID: orderID, TotalPriceMinor: 12000, Status: order.StatusPickedUp}
	resp := do(t, http.MethodPost, srv.URL+"/api/admin/payroll/disputes/"+disputeID+"/resolve",
		`{"status":"resolved_refund","resolution":"verified","refund_minor":5000}`)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusNoContent, resp.StatusCode)
}

func TestResolveDispute_RefundExceedsOrder_400(t *testing.T) {
	srv, f := buildHandler(t, adminUserObj())
	f.disputes.byID[disputeID] = &payroll.Dispute{
		ID: disputeID, EntryID: sp(entryID), OrderID: orderID, Status: payroll.DisputeStatusOpen,
	}
	f.orders.byID[orderID] = &order.Order{ID: orderID, TotalPriceMinor: 12000, Status: order.StatusPickedUp}
	resp := do(t, http.MethodPost, srv.URL+"/api/admin/payroll/disputes/"+disputeID+"/resolve",
		`{"status":"resolved_refund","resolution":"verified","refund_minor":99999}`)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
}

// === openDispute  POST /api/employee/disputes ===

func TestOpenDispute_Unauthenticated(t *testing.T) {
	srv, _ := buildHandler(t, nil)
	resp := do(t, http.MethodPost, srv.URL+"/api/employee/disputes",
		`{"order_id":"`+orderID+`","reason":"missing dessert"}`)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
}

func TestOpenDispute_WrongRole(t *testing.T) {
	srv, _ := buildHandler(t, adminUserObj()) // admin is not employee
	resp := do(t, http.MethodPost, srv.URL+"/api/employee/disputes",
		`{"order_id":"`+orderID+`","reason":"missing dessert"}`)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusForbidden, resp.StatusCode)
}

func TestOpenDispute_InvalidOrderUUID_422(t *testing.T) {
	// body order_id has format:"uuid" → huma rejects before handler.
	srv, _ := buildHandler(t, employeeUserObj())
	resp := do(t, http.MethodPost, srv.URL+"/api/employee/disputes",
		`{"order_id":"not-a-uuid","reason":"x"}`)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusUnprocessableEntity, resp.StatusCode)
}

func TestOpenDispute_EmptyReason_422(t *testing.T) {
	// reason has minLength:1 → huma rejects empty.
	srv, _ := buildHandler(t, employeeUserObj())
	resp := do(t, http.MethodPost, srv.URL+"/api/employee/disputes",
		`{"order_id":"`+orderID+`","reason":""}`)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusUnprocessableEntity, resp.StatusCode)
}

func TestOpenDispute_OrderNotInEntry_404(t *testing.T) {
	// No entry yet (ErrEntryNotFound) → falls back to order lookup; the order is
	// absent (ErrOrderNotFound) → mapped to 404.
	srv, f := buildHandler(t, employeeUserObj())
	f.entries.findOrderErr = payroll.ErrEntryNotFound
	resp := do(t, http.MethodPost, srv.URL+"/api/employee/disputes",
		`{"order_id":"`+orderID+`","reason":"missing dessert"}`)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusNotFound, resp.StatusCode)
}

func TestOpenDispute_NotOwner_403(t *testing.T) {
	// Entry resolves but is owned by another user → ErrForbidden → 403.
	srv, f := buildHandler(t, employeeUserObj())
	f.entries.findOrderID = entryID
	f.entries.byID[entryID] = &payroll.Entry{
		ID: entryID, BatchID: batchID, UserID: "someone-else", OrderIDs: []string{orderID},
	}
	resp := do(t, http.MethodPost, srv.URL+"/api/employee/disputes",
		`{"order_id":"`+orderID+`","reason":"missing dessert"}`)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusForbidden, resp.StatusCode)
}

func TestOpenDispute_OrderNotPartOfEntry_500(t *testing.T) {
	// Entry owned by caller but order_id not in OrderIDs → generic error → 500.
	srv, f := buildHandler(t, employeeUserObj())
	f.entries.findOrderID = entryID
	f.entries.byID[entryID] = &payroll.Entry{
		ID: entryID, BatchID: batchID, UserID: employeeID, OrderIDs: []string{"00000000-0000-0000-0000-000000000099"},
	}
	resp := do(t, http.MethodPost, srv.URL+"/api/employee/disputes",
		`{"order_id":"`+orderID+`","reason":"missing dessert"}`)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusInternalServerError, resp.StatusCode)
}

func TestOpenDispute_OK(t *testing.T) {
	srv, f := buildHandler(t, employeeUserObj())
	f.entries.findOrderID = entryID
	f.entries.byID[entryID] = &payroll.Entry{
		ID: entryID, BatchID: batchID, UserID: employeeID, OrderIDs: []string{orderID},
	}
	resp := do(t, http.MethodPost, srv.URL+"/api/employee/disputes",
		`{"order_id":"`+orderID+`","reason":"missing dessert"}`)
	defer resp.Body.Close()
	require.Equal(t, http.StatusCreated, resp.StatusCode)

	var out struct {
		Dispute struct {
			ID       string `json:"id"`
			EntryID  string `json:"entry_id"`
			OrderID  string `json:"order_id"`
			OpenedBy string `json:"opened_by"`
			Reason   string `json:"reason"`
			Status   string `json:"status"`
		} `json:"dispute"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&out))
	assert.Equal(t, disputeID, out.Dispute.ID)
	assert.Equal(t, entryID, out.Dispute.EntryID)
	assert.Equal(t, orderID, out.Dispute.OrderID)
	assert.Equal(t, employeeID, out.Dispute.OpenedBy)
	assert.Equal(t, "missing dessert", out.Dispute.Reason)
	assert.Equal(t, "open", out.Dispute.Status)
}

func TestOpenDispute_CreateError_500(t *testing.T) {
	srv, f := buildHandler(t, employeeUserObj())
	f.entries.findOrderID = entryID
	f.entries.byID[entryID] = &payroll.Entry{
		ID: entryID, BatchID: batchID, UserID: employeeID, OrderIDs: []string{orderID},
	}
	f.disputes.createErr = errors.New("db down")
	resp := do(t, http.MethodPost, srv.URL+"/api/employee/disputes",
		`{"order_id":"`+orderID+`","reason":"missing dessert"}`)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusInternalServerError, resp.StatusCode)
}

// === listMyDisputes  GET /api/employee/disputes ===

func TestListMyDisputes_Unauthenticated(t *testing.T) {
	srv, _ := buildHandler(t, nil)
	resp := do(t, http.MethodGet, srv.URL+"/api/employee/disputes", "")
	defer resp.Body.Close()
	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
}

func TestListMyDisputes_WrongRole(t *testing.T) {
	srv, _ := buildHandler(t, adminUserObj())
	resp := do(t, http.MethodGet, srv.URL+"/api/employee/disputes", "")
	defer resp.Body.Close()
	assert.Equal(t, http.StatusForbidden, resp.StatusCode)
}

func TestListMyDisputes_OK(t *testing.T) {
	srv, f := buildHandler(t, employeeUserObj())
	f.disputes.byUser = []*payroll.Dispute{
		{ID: disputeID, EntryID: sp(entryID), OrderID: orderID, OpenedBy: employeeID, Reason: "r", Status: payroll.DisputeStatusOpen},
	}
	resp := do(t, http.MethodGet, srv.URL+"/api/employee/disputes", "")
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	var out struct {
		Items []struct {
			ID     string `json:"id"`
			Status string `json:"status"`
		} `json:"items"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&out))
	require.Len(t, out.Items, 1)
	assert.Equal(t, disputeID, out.Items[0].ID)
	assert.Equal(t, "open", out.Items[0].Status)
}

func TestListMyDisputes_RepoError_500(t *testing.T) {
	srv, f := buildHandler(t, employeeUserObj())
	f.disputes.userErr = errors.New("db down")
	resp := do(t, http.MethodGet, srv.URL+"/api/employee/disputes", "")
	defer resp.Body.Close()
	assert.Equal(t, http.StatusInternalServerError, resp.StatusCode)
}

// === listMyEntries  GET /api/employee/payroll ===

func TestListMyEntries_Unauthenticated(t *testing.T) {
	srv, _ := buildHandler(t, nil)
	resp := do(t, http.MethodGet, srv.URL+"/api/employee/payroll", "")
	defer resp.Body.Close()
	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
}

func TestListMyEntries_WrongRole(t *testing.T) {
	srv, _ := buildHandler(t, adminUserObj())
	resp := do(t, http.MethodGet, srv.URL+"/api/employee/payroll", "")
	defer resp.Body.Close()
	assert.Equal(t, http.StatusForbidden, resp.StatusCode)
}

func TestListMyEntries_OK(t *testing.T) {
	srv, f := buildHandler(t, employeeUserObj())
	f.entries.byUser = []*payroll.EmployeeEntry{
		{
			EntryID:       entryID,
			BatchID:       batchID,
			PeriodStart:   time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC),
			PeriodEnd:     time.Date(2026, 4, 30, 0, 0, 0, 0, time.UTC),
			BatchStatus:   payroll.BatchStatusDraft,
			OrderCount:    3,
			AmountMinor:   20000,
			RefundedMinor: 5000,
		},
	}
	resp := do(t, http.MethodGet, srv.URL+"/api/employee/payroll", "")
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	var out struct {
		Items []struct {
			EntryID       string `json:"entry_id"`
			PeriodStart   string `json:"period_start"`
			OrderCount    int    `json:"order_count"`
			AmountMinor   int64  `json:"amount_minor"`
			RefundedMinor int64  `json:"refunded_minor"`
			NetMinor      int64  `json:"net_minor"`
		} `json:"items"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&out))
	require.Len(t, out.Items, 1)
	assert.Equal(t, entryID, out.Items[0].EntryID)
	assert.Equal(t, "2026-04-01", out.Items[0].PeriodStart)
	assert.Equal(t, 3, out.Items[0].OrderCount)
	assert.Equal(t, int64(20000), out.Items[0].AmountMinor)
	assert.Equal(t, int64(5000), out.Items[0].RefundedMinor)
	assert.Equal(t, int64(15000), out.Items[0].NetMinor, "net = amount - refunded")
}

func TestListMyEntries_RepoError_500(t *testing.T) {
	srv, f := buildHandler(t, employeeUserObj())
	f.entries.userErr = errors.New("db down")
	resp := do(t, http.MethodGet, srv.URL+"/api/employee/payroll", "")
	defer resp.Body.Close()
	assert.Equal(t, http.StatusInternalServerError, resp.StatusCode)
}

// === getEmployeeCurrentPayroll  GET /api/employee/payroll/current ===

func TestGetCurrentPayroll_Unauthenticated(t *testing.T) {
	srv, _ := buildHandler(t, nil)
	resp := do(t, http.MethodGet, srv.URL+"/api/employee/payroll/current", "")
	defer resp.Body.Close()
	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
}

func TestGetCurrentPayroll_WrongRole(t *testing.T) {
	srv, _ := buildHandler(t, adminUserObj())
	resp := do(t, http.MethodGet, srv.URL+"/api/employee/payroll/current", "")
	defer resp.Body.Close()
	assert.Equal(t, http.StatusForbidden, resp.StatusCode)
}

func TestGetCurrentPayroll_OK_TotalsOnlyCharged(t *testing.T) {
	srv, f := buildHandler(t, employeeUserObj())
	complaint := "c-1"
	f.lines.lines = []payroll.CurrentPayrollLine{
		{OrderID: "o1", SupplyDate: "2026-05-12", VendorName: "Vendor A", ItemsSummary: "1x 便當", AmountMinor: 9000, Status: "charged", Rated: true, ComplaintID: &complaint},
		{OrderID: "o2", SupplyDate: "2026-05-11", VendorName: "Vendor B", ItemsSummary: "1x 麵", AmountMinor: 5000, Status: "no_show"},
		{OrderID: "o3", SupplyDate: "2026-05-10", VendorName: "Vendor C", ItemsSummary: "1x 飯", AmountMinor: 7000, Status: "reversed"},
		{OrderID: "o4", SupplyDate: "2026-05-09", VendorName: "Vendor D", ItemsSummary: "2x 湯", AmountMinor: 3000, Status: "charged"},
	}
	resp := do(t, http.MethodGet, srv.URL+"/api/employee/payroll/current", "")
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	var out struct {
		Lines []struct {
			OrderID     string  `json:"order_id"`
			Status      string  `json:"status"`
			AmountMinor int64   `json:"amount_minor"`
			Rated       bool    `json:"rated"`
			ComplaintID *string `json:"complaint_id"`
		} `json:"lines"`
		TotalMinor int64 `json:"total_minor"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&out))
	require.Len(t, out.Lines, 4)
	// Only the two "charged" lines (9000 + 3000) count toward the total.
	assert.Equal(t, int64(12000), out.TotalMinor)
	assert.Equal(t, "o1", out.Lines[0].OrderID)
	assert.True(t, out.Lines[0].Rated)
	require.NotNil(t, out.Lines[0].ComplaintID)
	assert.Equal(t, "c-1", *out.Lines[0].ComplaintID)
}

func TestGetCurrentPayroll_Empty(t *testing.T) {
	srv, _ := buildHandler(t, employeeUserObj())
	resp := do(t, http.MethodGet, srv.URL+"/api/employee/payroll/current", "")
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	var out struct {
		Lines      []any `json:"lines"`
		TotalMinor int64 `json:"total_minor"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&out))
	assert.Empty(t, out.Lines)
	assert.Equal(t, int64(0), out.TotalMinor)
}

func TestGetCurrentPayroll_RepoError_500(t *testing.T) {
	srv, f := buildHandler(t, employeeUserObj())
	f.lines.err = errors.New("db down")
	resp := do(t, http.MethodGet, srv.URL+"/api/employee/payroll/current", "")
	defer resp.Body.Close()
	assert.Equal(t, http.StatusInternalServerError, resp.StatusCode)
}

// === listExceptions  GET /api/admin/payroll/batches/{id}/exceptions ===

func TestListExceptions_Unauthenticated(t *testing.T) {
	srv, _ := buildHandler(t, nil)
	resp := do(t, http.MethodGet, srv.URL+"/api/admin/payroll/batches/"+batchID+"/exceptions", "")
	defer resp.Body.Close()
	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
}

func TestListExceptions_WrongRole(t *testing.T) {
	srv, _ := buildHandler(t, employeeUserObj())
	resp := do(t, http.MethodGet, srv.URL+"/api/admin/payroll/batches/"+batchID+"/exceptions", "")
	defer resp.Body.Close()
	assert.Equal(t, http.StatusForbidden, resp.StatusCode)
}

func TestListExceptions_InvalidUUID_422(t *testing.T) {
	srv, _ := buildHandler(t, adminUserObj())
	resp := do(t, http.MethodGet, srv.URL+"/api/admin/payroll/batches/not-a-uuid/exceptions", "")
	defer resp.Body.Close()
	assert.Equal(t, http.StatusUnprocessableEntity, resp.StatusCode)
}

func TestListExceptions_BatchNotFound_404(t *testing.T) {
	srv, _ := buildHandler(t, adminUserObj()) // batch not seeded
	resp := do(t, http.MethodGet, srv.URL+"/api/admin/payroll/batches/"+batchID+"/exceptions", "")
	defer resp.Body.Close()
	assert.Equal(t, http.StatusNotFound, resp.StatusCode)
}

func TestListExceptions_OK(t *testing.T) {
	srv, f := buildHandler(t, adminUserObj())
	f.batches.byID[batchID] = &payroll.Batch{ID: batchID, Status: payroll.BatchStatusDraft}
	resolvedBy := adminUser
	resolvedAt := time.Date(2026, 5, 3, 10, 0, 0, 0, time.UTC)
	f.excs.listByBatch = []*payroll.Exception{
		{
			ID:         excID,
			BatchID:    batchID,
			EntryID:    entryID,
			UserID:     employeeID,
			Kind:       payroll.ExceptionEmployeeDeparted,
			Status:     payroll.ExceptionExcluded,
			Detail:     "已離職",
			Resolution: "excluded",
			ResolvedBy: &resolvedBy,
			ResolvedAt: &resolvedAt,
			CreatedAt:  time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC),
		},
	}
	resp := do(t, http.MethodGet, srv.URL+"/api/admin/payroll/batches/"+batchID+"/exceptions", "")
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	var out struct {
		Items []struct {
			ID         string  `json:"id"`
			Kind       string  `json:"kind"`
			Status     string  `json:"status"`
			ResolvedBy *string `json:"resolved_by"`
		} `json:"items"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&out))
	require.Len(t, out.Items, 1)
	assert.Equal(t, excID, out.Items[0].ID)
	assert.Equal(t, "employee_departed", out.Items[0].Kind)
	assert.Equal(t, "excluded", out.Items[0].Status)
	require.NotNil(t, out.Items[0].ResolvedBy)
	assert.Equal(t, adminUser, *out.Items[0].ResolvedBy)
}

func TestListExceptions_UpsertError_500(t *testing.T) {
	srv, f := buildHandler(t, adminUserObj())
	f.batches.byID[batchID] = &payroll.Batch{ID: batchID, Status: payroll.BatchStatusDraft}
	f.excs.upsertErr = errors.New("db down")
	resp := do(t, http.MethodGet, srv.URL+"/api/admin/payroll/batches/"+batchID+"/exceptions", "")
	defer resp.Body.Close()
	assert.Equal(t, http.StatusInternalServerError, resp.StatusCode)
}

func TestListExceptions_ListError_500(t *testing.T) {
	srv, f := buildHandler(t, adminUserObj())
	f.batches.byID[batchID] = &payroll.Batch{ID: batchID, Status: payroll.BatchStatusDraft}
	f.excs.listErr = errors.New("db down")
	resp := do(t, http.MethodGet, srv.URL+"/api/admin/payroll/batches/"+batchID+"/exceptions", "")
	defer resp.Body.Close()
	assert.Equal(t, http.StatusInternalServerError, resp.StatusCode)
}

// === flagException  POST /api/admin/payroll/batches/{id}/exceptions ===

func TestFlagException_Unauthenticated(t *testing.T) {
	srv, _ := buildHandler(t, nil)
	resp := do(t, http.MethodPost, srv.URL+"/api/admin/payroll/batches/"+batchID+"/exceptions",
		`{"entry_id":"`+entryID+`","detail":"bank error"}`)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
}

func TestFlagException_WrongRole(t *testing.T) {
	srv, _ := buildHandler(t, employeeUserObj())
	resp := do(t, http.MethodPost, srv.URL+"/api/admin/payroll/batches/"+batchID+"/exceptions",
		`{"entry_id":"`+entryID+`","detail":"bank error"}`)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusForbidden, resp.StatusCode)
}

func TestFlagException_InvalidBatchUUID_422(t *testing.T) {
	srv, _ := buildHandler(t, adminUserObj())
	resp := do(t, http.MethodPost, srv.URL+"/api/admin/payroll/batches/not-a-uuid/exceptions",
		`{"entry_id":"`+entryID+`","detail":"bank error"}`)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusUnprocessableEntity, resp.StatusCode)
}

func TestFlagException_InvalidEntryUUID_422(t *testing.T) {
	// body entry_id has format:"uuid".
	srv, _ := buildHandler(t, adminUserObj())
	resp := do(t, http.MethodPost, srv.URL+"/api/admin/payroll/batches/"+batchID+"/exceptions",
		`{"entry_id":"not-a-uuid","detail":"bank error"}`)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusUnprocessableEntity, resp.StatusCode)
}

func TestFlagException_EntryNotFound_404(t *testing.T) {
	srv, _ := buildHandler(t, adminUserObj()) // entry not seeded
	resp := do(t, http.MethodPost, srv.URL+"/api/admin/payroll/batches/"+batchID+"/exceptions",
		`{"entry_id":"`+entryID+`","detail":"bank error"}`)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusNotFound, resp.StatusCode)
}

func TestFlagException_EntryNotInBatch_400(t *testing.T) {
	// Entry exists but belongs to a different batch → ErrInvalidException → 400.
	srv, f := buildHandler(t, adminUserObj())
	f.entries.byID[entryID] = &payroll.Entry{ID: entryID, BatchID: "another-batch", UserID: employeeID}
	resp := do(t, http.MethodPost, srv.URL+"/api/admin/payroll/batches/"+batchID+"/exceptions",
		`{"entry_id":"`+entryID+`","detail":"bank error"}`)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
}

func TestFlagException_OK_201(t *testing.T) {
	srv, f := buildHandler(t, adminUserObj())
	f.entries.byID[entryID] = &payroll.Entry{ID: entryID, BatchID: batchID, UserID: employeeID}
	resp := do(t, http.MethodPost, srv.URL+"/api/admin/payroll/batches/"+batchID+"/exceptions",
		`{"entry_id":"`+entryID+`","detail":"bank error"}`)
	defer resp.Body.Close()
	require.Equal(t, http.StatusCreated, resp.StatusCode)

	var out struct {
		Exception struct {
			BatchID string `json:"batch_id"`
			EntryID string `json:"entry_id"`
			UserID  string `json:"user_id"`
			Kind    string `json:"kind"`
			Status  string `json:"status"`
			Detail  string `json:"detail"`
		} `json:"exception"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&out))
	assert.Equal(t, batchID, out.Exception.BatchID)
	assert.Equal(t, entryID, out.Exception.EntryID)
	assert.Equal(t, employeeID, out.Exception.UserID)
	assert.Equal(t, "deduction_failed", out.Exception.Kind)
	assert.Equal(t, "open", out.Exception.Status)
	assert.Equal(t, "bank error", out.Exception.Detail)
}

// === resolveException  POST /api/admin/payroll/exceptions/{id}/resolve ===

func TestResolveException_Unauthenticated(t *testing.T) {
	srv, _ := buildHandler(t, nil)
	resp := do(t, http.MethodPost, srv.URL+"/api/admin/payroll/exceptions/"+excID+"/resolve",
		`{"status":"resolved","resolution":"handled"}`)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
}

func TestResolveException_WrongRole(t *testing.T) {
	srv, _ := buildHandler(t, employeeUserObj())
	resp := do(t, http.MethodPost, srv.URL+"/api/admin/payroll/exceptions/"+excID+"/resolve",
		`{"status":"resolved","resolution":"handled"}`)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusForbidden, resp.StatusCode)
}

func TestResolveException_InvalidUUID_422(t *testing.T) {
	srv, _ := buildHandler(t, adminUserObj())
	resp := do(t, http.MethodPost, srv.URL+"/api/admin/payroll/exceptions/not-a-uuid/resolve",
		`{"status":"resolved","resolution":"handled"}`)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusUnprocessableEntity, resp.StatusCode)
}

func TestResolveException_InvalidStatusEnum_422(t *testing.T) {
	// status enum is resolved|excluded → huma rejects others.
	srv, _ := buildHandler(t, adminUserObj())
	resp := do(t, http.MethodPost, srv.URL+"/api/admin/payroll/exceptions/"+excID+"/resolve",
		`{"status":"open","resolution":"x"}`)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusUnprocessableEntity, resp.StatusCode)
}

func TestResolveException_NotFound_404(t *testing.T) {
	srv, _ := buildHandler(t, adminUserObj()) // exception not seeded
	resp := do(t, http.MethodPost, srv.URL+"/api/admin/payroll/exceptions/"+excID+"/resolve",
		`{"status":"resolved","resolution":"handled"}`)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusNotFound, resp.StatusCode)
}

func TestResolveException_OK_204(t *testing.T) {
	srv, f := buildHandler(t, adminUserObj())
	f.excs.byID[excID] = &payroll.Exception{
		ID: excID, BatchID: batchID, EntryID: entryID, UserID: employeeID,
		Kind: payroll.ExceptionDeductionFailed, Status: payroll.ExceptionOpen,
	}
	resp := do(t, http.MethodPost, srv.URL+"/api/admin/payroll/exceptions/"+excID+"/resolve",
		`{"status":"resolved","resolution":"handled"}`)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusNoContent, resp.StatusCode)
}
