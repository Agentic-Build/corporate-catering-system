package payroll_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Agentic-Build/corporate-catering-system/services/api/internal/order"
	"github.com/Agentic-Build/corporate-catering-system/services/api/internal/payroll"
	plaudit "github.com/Agentic-Build/corporate-catering-system/services/api/internal/platform/audit"
)

// These tests drive payroll.Service entirely through in-process fakes — no
// Postgres container — to exercise the repo-error branches that the
// container-backed tests cannot reach (a repo returning an arbitrary error
// mid-orchestration). They are fast and deterministic.

var errBoom = errors.New("boom")

// --- pool / tx fakes (BeginFunc-compatible) ---

// nopBeginner hands the write closure a no-op tx. BeginFunc commits on success.
type nopBeginner struct{}

func (nopBeginner) Begin(context.Context) (pgx.Tx, error) { return nopTx{}, nil }
func (nopBeginner) Query(context.Context, string, ...any) (pgx.Rows, error) {
	return nil, nil
}

type nopTx struct{ pgx.Tx }

func (nopTx) Commit(context.Context) error   { return nil }
func (nopTx) Rollback(context.Context) error { return nil }

// --- repo fakes: every method returns a configurable error / value ---

type fakeBatches struct {
	getByID     *payroll.Batch
	getByIDErr  error
	getByPeriod *payroll.Batch
	periodErr   error
}

func (f *fakeBatches) Create(context.Context, *payroll.Batch) error { return nil }
func (f *fakeBatches) CreateTx(context.Context, pgx.Tx, *payroll.Batch) error {
	return nil
}
func (f *fakeBatches) GetByID(context.Context, string) (*payroll.Batch, error) {
	return f.getByID, f.getByIDErr
}
func (f *fakeBatches) GetByPeriod(context.Context, time.Time, time.Time) (*payroll.Batch, error) {
	return f.getByPeriod, f.periodErr
}
func (f *fakeBatches) UpdateStatusTx(context.Context, pgx.Tx, string, payroll.BatchStatus, payroll.BatchStatus, *string) error {
	return nil
}
func (f *fakeBatches) SetExportInfoTx(context.Context, pgx.Tx, string, string, time.Time) error {
	return nil
}
func (f *fakeBatches) List(context.Context, []payroll.BatchStatus) ([]*payroll.Batch, error) {
	return nil, nil
}

type fakeEntries struct {
	getByID      *payroll.Entry
	getByIDErr   error
	findID       string
	findErr      error
	createErr    error
	incErr       error
	createCalled bool
}

func (f *fakeEntries) CreateTx(context.Context, pgx.Tx, *payroll.Entry) error {
	f.createCalled = true
	return f.createErr
}
func (f *fakeEntries) GetByID(context.Context, string) (*payroll.Entry, error) {
	return f.getByID, f.getByIDErr
}
func (f *fakeEntries) ListByBatch(context.Context, string) ([]*payroll.Entry, error) {
	return nil, nil
}
func (f *fakeEntries) IncrementRefundedTx(context.Context, pgx.Tx, string, int64) error {
	return f.incErr
}
func (f *fakeEntries) FindByOrderForUser(context.Context, string, string) (string, error) {
	return f.findID, f.findErr
}
func (f *fakeEntries) ListByUser(context.Context, string) ([]*payroll.EmployeeEntry, error) {
	return nil, nil
}

type fakeDisputes struct {
	getByID    *payroll.Dispute
	getByIDErr error
	createErr  error
	updateErr  error
}

func (f *fakeDisputes) Create(context.Context, *payroll.Dispute) error { return f.createErr }
func (f *fakeDisputes) GetByID(context.Context, string) (*payroll.Dispute, error) {
	return f.getByID, f.getByIDErr
}
func (f *fakeDisputes) UpdateStatusTx(context.Context, pgx.Tx, string, payroll.DisputeStatus, *string, string, int64) error {
	return f.updateErr
}
func (f *fakeDisputes) ListByStatus(context.Context, []payroll.DisputeStatus) ([]*payroll.Dispute, error) {
	return nil, nil
}
func (f *fakeDisputes) ListByUser(context.Context, string) ([]*payroll.Dispute, error) {
	return nil, nil
}

type fakeExceptions struct {
	upsertDepartedErr   error
	upsertDepartedTxErr error
	getByID             *payroll.Exception
	getByIDErr          error
	createErr           error
	resolveErr          error
}

func (f *fakeExceptions) UpsertDepartedTx(context.Context, pgx.Tx, string) error {
	return f.upsertDepartedTxErr
}
func (f *fakeExceptions) UpsertDeparted(context.Context, string) error {
	return f.upsertDepartedErr
}
func (f *fakeExceptions) Create(context.Context, *payroll.Exception) error { return f.createErr }
func (f *fakeExceptions) GetByID(context.Context, string) (*payroll.Exception, error) {
	return f.getByID, f.getByIDErr
}
func (f *fakeExceptions) ListByBatch(context.Context, string) ([]*payroll.Exception, error) {
	return nil, nil
}
func (f *fakeExceptions) Resolve(context.Context, string, payroll.ExceptionStatus, string, string) error {
	return f.resolveErr
}

type fakeOrders struct {
	get     *order.Order
	getErr  error
	list    []*order.Order
	listErr error
}

func (f *fakeOrders) GetByID(context.Context, string) (*order.Order, error) {
	return f.get, f.getErr
}
func (f *fakeOrders) ListPickedOrNoShowInPeriod(context.Context, time.Time, time.Time) ([]*order.Order, error) {
	return f.list, f.listErr
}

type fakeOrderTx struct{ err error }

func (f fakeOrderTx) UpdateStatusTx(context.Context, pgx.Tx, string, order.Status, order.Status) error {
	return f.err
}

type fakeAudit struct{ err error }

func (f fakeAudit) WriteTx(context.Context, pgx.Tx, plaudit.Entry) error { return f.err }

type fakeOutbox struct{ err error }

func (f fakeOutbox) AppendTx(context.Context, pgx.Tx, string, string, string, map[string]any, map[string]any) error {
	return f.err
}

// baseService wires a Service with no-op pool + supplied repos. Unset repos are
// nil; only set what each test needs.
func baseService() *payroll.Service {
	return &payroll.Service{Pool: nopBeginner{}}
}

func aprilStartEnd() (time.Time, time.Time) {
	start := time.Date(2026, time.April, 1, 0, 0, 0, 0, time.UTC)
	return start, start.AddDate(0, 1, -1)
}

// === BuildDraft: GetByPeriod returns a non-NotFound error ===

func TestService_BuildDraft_GetByPeriodError(t *testing.T) {
	svc := baseService()
	svc.Batches = &fakeBatches{periodErr: errBoom}
	start, end := aprilStartEnd()
	_, err := svc.BuildDraft(context.Background(), payroll.BuildDraftInput{PeriodStart: start, PeriodEnd: end})
	assert.ErrorIs(t, err, errBoom)
}

// === BuildDraft: ListPickedOrNoShowInPeriod error ===

func TestService_BuildDraft_ListOrdersError(t *testing.T) {
	svc := baseService()
	svc.Batches = &fakeBatches{periodErr: payroll.ErrBatchNotFound}
	svc.Orders = &fakeOrders{listErr: errBoom}
	start, end := aprilStartEnd()
	_, err := svc.BuildDraft(context.Background(), payroll.BuildDraftInput{PeriodStart: start, PeriodEnd: end})
	assert.ErrorIs(t, err, errBoom)
}

// === BuildDraft: persistDraftBatch — Entries.CreateTx fails in the per-user loop ===

func TestService_BuildDraft_EntryCreateTxError(t *testing.T) {
	svc := baseService()
	svc.Batches = &fakeBatches{periodErr: payroll.ErrBatchNotFound}
	svc.Orders = &fakeOrders{list: []*order.Order{{ID: "o1", UserID: "u1", TotalPriceMinor: 1000}}}
	svc.Entries = &fakeEntries{createErr: errBoom}
	start, end := aprilStartEnd()
	_, err := svc.BuildDraft(context.Background(), payroll.BuildDraftInput{PeriodStart: start, PeriodEnd: end})
	assert.ErrorIs(t, err, errBoom)
}

// === BuildDraft: persistDraftBatch — Exceptions.UpsertDepartedTx fails ===

func TestService_BuildDraft_UpsertDepartedTxError(t *testing.T) {
	svc := baseService()
	svc.Batches = &fakeBatches{periodErr: payroll.ErrBatchNotFound}
	svc.Orders = &fakeOrders{list: []*order.Order{{ID: "o1", UserID: "u1", TotalPriceMinor: 1000}}}
	svc.Entries = &fakeEntries{}
	svc.Exceptions = &fakeExceptions{upsertDepartedTxErr: errBoom}
	start, end := aprilStartEnd()
	_, err := svc.BuildDraft(context.Background(), payroll.BuildDraftInput{PeriodStart: start, PeriodEnd: end})
	assert.ErrorIs(t, err, errBoom)
}

// === Lock: Outbox.AppendTx fails inside the transaction ===

func TestService_Lock_OutboxError(t *testing.T) {
	svc := baseService()
	svc.Batches = &fakeBatches{getByID: &payroll.Batch{ID: "b1", Status: payroll.BatchStatusDraft}}
	svc.Outbox = fakeOutbox{err: errBoom}
	svc.Audit = fakeAudit{}
	err := svc.Lock(context.Background(), "b1", "admin")
	assert.ErrorIs(t, err, errBoom)
}

// === ResolveException: Exceptions.Resolve fails ===

func TestService_ResolveException_ResolveError(t *testing.T) {
	svc := baseService()
	svc.Exceptions = &fakeExceptions{
		getByID:    &payroll.Exception{ID: "x1", BatchID: "b1"},
		resolveErr: errBoom,
	}
	err := svc.ResolveException(context.Background(), "x1", payroll.ExceptionResolved, "ok", "admin")
	assert.ErrorIs(t, err, errBoom)
}

// === OpenDisputeByOrder: FindByOrderForUser returns a non-NotFound error ===

func TestService_OpenDisputeByOrder_FindError(t *testing.T) {
	svc := baseService()
	svc.Entries = &fakeEntries{findErr: errBoom}
	_, err := svc.OpenDisputeByOrder(context.Background(), "o1", "u1", "reason")
	assert.ErrorIs(t, err, errBoom)
}

// === OpenDisputeByOrder: entry-less path, Disputes.Create fails ===

func TestService_OpenDisputeByOrder_EntrylessCreateError(t *testing.T) {
	svc := baseService()
	svc.Entries = &fakeEntries{findErr: payroll.ErrEntryNotFound}
	svc.Orders = &fakeOrders{get: &order.Order{ID: "o1", UserID: "u1", Status: order.StatusPickedUp, TotalPriceMinor: 1000}}
	svc.Disputes = &fakeDisputes{createErr: errBoom}
	_, err := svc.OpenDisputeByOrder(context.Background(), "o1", "u1", "reason")
	assert.ErrorIs(t, err, errBoom)
}

// === OpenDispute: Disputes.Create fails (entry path) ===

func TestService_OpenDispute_CreateError(t *testing.T) {
	svc := baseService()
	svc.Entries = &fakeEntries{getByID: &payroll.Entry{ID: "e1", UserID: "u1", OrderIDs: []string{"o1"}}}
	svc.Disputes = &fakeDisputes{createErr: errBoom}
	_, err := svc.OpenDispute(context.Background(), payroll.OpenDisputeInput{
		EntryID: "e1", OrderID: "o1", OpenedBy: "u1", Reason: "r",
	})
	assert.ErrorIs(t, err, errBoom)
}

// === ResolveDispute: applyDisputeRefund — Orders.GetByID fails ===

func TestService_ResolveDispute_RefundGetOrderError(t *testing.T) {
	svc := baseService()
	entryID := "e1"
	svc.Disputes = &fakeDisputes{getByID: &payroll.Dispute{ID: "d1", OrderID: "o1", EntryID: &entryID, Status: payroll.DisputeStatusOpen}}
	svc.Orders = &fakeOrders{getErr: errBoom}
	svc.Outbox = fakeOutbox{}
	svc.Audit = fakeAudit{}
	err := svc.ResolveDispute(context.Background(), payroll.ResolveDisputeInput{
		DisputeID: "d1", ResolvedBy: "admin", Status: payroll.DisputeStatusResolvedRefund, RefundMinor: 100,
	})
	assert.ErrorIs(t, err, errBoom)
}

// === ResolveDispute: applyDisputeRefund — IncrementRefundedTx fails ===

func TestService_ResolveDispute_RefundIncrementError(t *testing.T) {
	svc := baseService()
	entryID := "e1"
	svc.Disputes = &fakeDisputes{getByID: &payroll.Dispute{ID: "d1", OrderID: "o1", EntryID: &entryID, Status: payroll.DisputeStatusOpen}}
	svc.Orders = &fakeOrders{get: &order.Order{ID: "o1", UserID: "u1", Status: order.StatusPickedUp, TotalPriceMinor: 1000}}
	svc.Entries = &fakeEntries{incErr: errBoom}
	svc.OrderTx = fakeOrderTx{}
	svc.Outbox = fakeOutbox{}
	svc.Audit = fakeAudit{}
	err := svc.ResolveDispute(context.Background(), payroll.ResolveDisputeInput{
		DisputeID: "d1", ResolvedBy: "admin", Status: payroll.DisputeStatusResolvedRefund, RefundMinor: 100,
	})
	assert.ErrorIs(t, err, errBoom)
}

// === ResolveDispute: recordDisputeResolution — Outbox append fails (reject path) ===

func TestService_ResolveDispute_OutboxError(t *testing.T) {
	svc := baseService()
	svc.Disputes = &fakeDisputes{getByID: &payroll.Dispute{ID: "d1", OrderID: "o1", Status: payroll.DisputeStatusOpen}}
	svc.Outbox = fakeOutbox{err: errBoom}
	svc.Audit = fakeAudit{}
	err := svc.ResolveDispute(context.Background(), payroll.ResolveDisputeInput{
		DisputeID: "d1", ResolvedBy: "admin", Status: payroll.DisputeStatusResolvedReject,
	})
	assert.ErrorIs(t, err, errBoom)
}

// === ResolveDispute: recordDisputeResolution — Audit write fails ===

func TestService_ResolveDispute_AuditError(t *testing.T) {
	svc := baseService()
	svc.Disputes = &fakeDisputes{getByID: &payroll.Dispute{ID: "d1", OrderID: "o1", Status: payroll.DisputeStatusOpen}}
	svc.Outbox = fakeOutbox{}
	svc.Audit = fakeAudit{err: errBoom}
	err := svc.ResolveDispute(context.Background(), payroll.ResolveDisputeInput{
		DisputeID: "d1", ResolvedBy: "admin", Status: payroll.DisputeStatusResolvedReject,
	})
	assert.ErrorIs(t, err, errBoom)
}

// === ListExceptions: UpsertDeparted error path ===

func TestService_ListExceptions_UpsertError(t *testing.T) {
	svc := baseService()
	svc.Batches = &fakeBatches{getByID: &payroll.Batch{ID: "b1"}}
	svc.Exceptions = &fakeExceptions{upsertDepartedErr: errBoom}
	_, err := svc.ListExceptions(context.Background(), "b1")
	assert.ErrorIs(t, err, errBoom)
}

// === FlagException: GetByID error ===

func TestService_FlagException_GetEntryError(t *testing.T) {
	svc := baseService()
	svc.Entries = &fakeEntries{getByIDErr: errBoom}
	_, err := svc.FlagException(context.Background(), payroll.FlagExceptionInput{
		BatchID: "b1", EntryID: "e1", Detail: "x", FlaggedBy: "admin",
	})
	assert.ErrorIs(t, err, errBoom)
}

// === FlagException: Exceptions.Create error ===

func TestService_FlagException_CreateError(t *testing.T) {
	svc := baseService()
	svc.Entries = &fakeEntries{getByID: &payroll.Entry{ID: "e1", BatchID: "b1", UserID: "u1"}}
	svc.Exceptions = &fakeExceptions{createErr: errBoom}
	_, err := svc.FlagException(context.Background(), payroll.FlagExceptionInput{
		BatchID: "b1", EntryID: "e1", Detail: "x", FlaggedBy: "admin",
	})
	assert.ErrorIs(t, err, errBoom)
}

// === ReverseOrder: FindByOrderForUser returns a non-NotFound error ===

func TestService_ReverseOrder_FindEntryError(t *testing.T) {
	svc := baseService()
	svc.Orders = &fakeOrders{get: &order.Order{ID: "o1", UserID: "u1", Status: order.StatusPickedUp, TotalPriceMinor: 1000}}
	svc.Entries = &fakeEntries{findErr: errBoom}
	err := svc.ReverseOrder(context.Background(), "o1")
	assert.ErrorIs(t, err, errBoom)
}

// === ReverseOrder: applyReverseOrderTx — IncrementRefundedTx fails (has entry) ===

func TestService_ReverseOrder_IncrementError(t *testing.T) {
	svc := baseService()
	svc.Orders = &fakeOrders{get: &order.Order{ID: "o1", UserID: "u1", Status: order.StatusPickedUp, TotalPriceMinor: 1000}}
	svc.Entries = &fakeEntries{findID: "e1", incErr: errBoom}
	svc.OrderTx = fakeOrderTx{}
	svc.Outbox = fakeOutbox{}
	svc.Audit = fakeAudit{}
	err := svc.ReverseOrder(context.Background(), "o1")
	assert.ErrorIs(t, err, errBoom)
}

// === ReverseOrder: applyReverseOrderTx — Outbox append fails (current period, no entry) ===

func TestService_ReverseOrder_OutboxError(t *testing.T) {
	svc := baseService()
	svc.Orders = &fakeOrders{get: &order.Order{ID: "o1", UserID: "u1", Status: order.StatusNoShow, TotalPriceMinor: 1000}}
	svc.Entries = &fakeEntries{findErr: payroll.ErrEntryNotFound}
	svc.OrderTx = fakeOrderTx{}
	svc.Outbox = fakeOutbox{err: errBoom}
	svc.Audit = fakeAudit{}
	err := svc.ReverseOrder(context.Background(), "o1")
	assert.ErrorIs(t, err, errBoom)
}

// === ListCurrentLines: Pool fallback (CurrentLines nil) returns query error ===

func TestService_ListCurrentLines_PoolFallbackError(t *testing.T) {
	svc := &payroll.Service{Pool: errBeginner{}}
	require.Nil(t, svc.CurrentLines)
	_, err := svc.ListCurrentLines(context.Background(), "u1")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "query current lines")
}

// errBeginner.Query always errors, driving the QueryCurrentLines error branch
// through the Pool fallback in ListCurrentLines.
type errBeginner struct{}

func (errBeginner) Begin(context.Context) (pgx.Tx, error) { return nopTx{}, nil }
func (errBeginner) Query(context.Context, string, ...any) (pgx.Rows, error) {
	return nil, errBoom
}
