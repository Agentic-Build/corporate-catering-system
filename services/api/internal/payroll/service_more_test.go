package payroll_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/takalawang/corporate-catering-system/services/api/internal/order"
	"github.com/takalawang/corporate-catering-system/services/api/internal/payroll"
	ppg "github.com/takalawang/corporate-catering-system/services/api/internal/payroll/postgres"
)

const missingUUID = "00000000-0000-0000-0000-000000000000"

// ---------- BuildDraft validation ----------

func TestService_BuildDraft_PeriodStartAfterEnd(t *testing.T) {
	_, svc, cleanup := setup(t)
	defer cleanup()
	ctx := context.Background()
	start, end := aprilPeriod()

	_, err := svc.BuildDraft(ctx, payroll.BuildDraftInput{PeriodStart: end, PeriodEnd: start})
	require.Error(t, err)
}

// ---------- Lock error path ----------

func TestService_Lock_BatchNotFound(t *testing.T) {
	pool, svc, cleanup := setup(t)
	defer cleanup()
	ctx := context.Background()
	admin := seedAdminUser(t, pool)

	err := svc.Lock(ctx, missingUUID, admin)
	assert.ErrorIs(t, err, payroll.ErrBatchNotFound)
}

// ---------- OpenDispute error paths ----------

func TestService_OpenDispute_EntryNotFound(t *testing.T) {
	pool, svc, cleanup := setup(t)
	defer cleanup()
	ctx := context.Background()
	user := seedEmployeeUser(t, pool)

	_, err := svc.OpenDispute(ctx, payroll.OpenDisputeInput{
		EntryID: missingUUID, OrderID: missingUUID, OpenedBy: user, Reason: "x",
	})
	assert.ErrorIs(t, err, payroll.ErrEntryNotFound)
}

func TestService_OpenDispute_OrderNotInEntry(t *testing.T) {
	pool, svc, cleanup := setup(t)
	defer cleanup()
	ctx := context.Background()

	start, end := aprilPeriod()
	vendor := seedVendor(t, pool)
	user := seedEmployeeUser(t, pool)
	seedPickedUpOrder(t, pool, user, vendor, start.AddDate(0, 0, 4), 15000)

	batch, err := svc.BuildDraft(ctx, payroll.BuildDraftInput{PeriodStart: start, PeriodEnd: end})
	require.NoError(t, err)
	entries, err := svc.ListBatchEntries(ctx, batch.ID)
	require.NoError(t, err)
	require.Len(t, entries, 1)

	_, err = svc.OpenDispute(ctx, payroll.OpenDisputeInput{
		EntryID: entries[0].ID, OrderID: missingUUID, OpenedBy: user, Reason: "not in entry",
	})
	require.Error(t, err)
	assert.NotErrorIs(t, err, payroll.ErrForbidden)
}

// ---------- OpenDisputeByOrder ----------

func TestService_OpenDisputeByOrder_Happy(t *testing.T) {
	pool, svc, cleanup := setup(t)
	defer cleanup()
	ctx := context.Background()

	start, end := aprilPeriod()
	vendor := seedVendor(t, pool)
	user := seedEmployeeUser(t, pool)
	orderID := seedPickedUpOrder(t, pool, user, vendor, start.AddDate(0, 0, 4), 15000)

	batch, err := svc.BuildDraft(ctx, payroll.BuildDraftInput{PeriodStart: start, PeriodEnd: end})
	require.NoError(t, err)
	entries, err := svc.ListBatchEntries(ctx, batch.ID)
	require.NoError(t, err)
	require.Len(t, entries, 1)

	d, err := svc.OpenDisputeByOrder(ctx, orderID, user, "missing dessert")
	require.NoError(t, err)
	assert.Equal(t, entries[0].ID, d.EntryID)
	assert.Equal(t, orderID, d.OrderID)
	assert.Equal(t, payroll.DisputeStatusOpen, d.Status)
}

func TestService_OpenDisputeByOrder_NoEntry(t *testing.T) {
	pool, svc, cleanup := setup(t)
	defer cleanup()
	ctx := context.Background()
	user := seedEmployeeUser(t, pool)

	_, err := svc.OpenDisputeByOrder(ctx, missingUUID, user, "no entry")
	assert.ErrorIs(t, err, payroll.ErrEntryNotFound)
}

// ---------- ResolveDispute error paths ----------

func TestService_ResolveDispute_InvalidStatus(t *testing.T) {
	pool, svc, cleanup := setup(t)
	defer cleanup()
	ctx := context.Background()
	admin := seedAdminUser(t, pool)

	err := svc.ResolveDispute(ctx, payroll.ResolveDisputeInput{
		DisputeID: missingUUID, ResolvedBy: admin, Status: payroll.DisputeStatusOpen,
	})
	require.Error(t, err)
}

func TestService_ResolveDispute_NotFound(t *testing.T) {
	pool, svc, cleanup := setup(t)
	defer cleanup()
	ctx := context.Background()
	admin := seedAdminUser(t, pool)

	err := svc.ResolveDispute(ctx, payroll.ResolveDisputeInput{
		DisputeID: missingUUID, ResolvedBy: admin, Status: payroll.DisputeStatusResolvedReject,
	})
	assert.ErrorIs(t, err, payroll.ErrDisputeNotFound)
}

func TestService_ResolveDispute_AlreadyResolved(t *testing.T) {
	pool, svc, cleanup := setup(t)
	defer cleanup()
	ctx := context.Background()

	start, end := aprilPeriod()
	vendor := seedVendor(t, pool)
	user := seedEmployeeUser(t, pool)
	admin := seedAdminUser(t, pool)
	orderID := seedPickedUpOrder(t, pool, user, vendor, start.AddDate(0, 0, 4), 15000)

	batch, err := svc.BuildDraft(ctx, payroll.BuildDraftInput{PeriodStart: start, PeriodEnd: end})
	require.NoError(t, err)
	entries, err := svc.ListBatchEntries(ctx, batch.ID)
	require.NoError(t, err)

	d, err := svc.OpenDispute(ctx, payroll.OpenDisputeInput{
		EntryID: entries[0].ID, OrderID: orderID, OpenedBy: user, Reason: "claim",
	})
	require.NoError(t, err)

	require.NoError(t, svc.ResolveDispute(ctx, payroll.ResolveDisputeInput{
		DisputeID: d.ID, ResolvedBy: admin, Status: payroll.DisputeStatusResolvedReject, Resolution: "done",
	}))

	// Second resolution on an already-resolved dispute is rejected.
	err = svc.ResolveDispute(ctx, payroll.ResolveDisputeInput{
		DisputeID: d.ID, ResolvedBy: admin, Status: payroll.DisputeStatusResolvedReject, Resolution: "again",
	})
	assert.ErrorIs(t, err, payroll.ErrInvalidTransition)
}

func TestService_ResolveDispute_NegativeRefund(t *testing.T) {
	pool, svc, cleanup := setup(t)
	defer cleanup()
	ctx := context.Background()

	start, end := aprilPeriod()
	vendor := seedVendor(t, pool)
	user := seedEmployeeUser(t, pool)
	admin := seedAdminUser(t, pool)
	orderID := seedPickedUpOrder(t, pool, user, vendor, start.AddDate(0, 0, 4), 15000)

	batch, err := svc.BuildDraft(ctx, payroll.BuildDraftInput{PeriodStart: start, PeriodEnd: end})
	require.NoError(t, err)
	entries, err := svc.ListBatchEntries(ctx, batch.ID)
	require.NoError(t, err)
	entryID := entries[0].ID

	d, err := svc.OpenDispute(ctx, payroll.OpenDisputeInput{
		EntryID: entryID, OrderID: orderID, OpenedBy: user, Reason: "bad amount",
	})
	require.NoError(t, err)

	err = svc.ResolveDispute(ctx, payroll.ResolveDisputeInput{
		DisputeID: d.ID, ResolvedBy: admin, Status: payroll.DisputeStatusResolvedRefund,
		Resolution: "negative", RefundMinor: -1,
	})
	require.Error(t, err)

	// Transaction rolled back: dispute still open, entry refund untouched.
	var status string
	var refunded int64
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT status::text FROM payroll_dispute WHERE id=$1`, d.ID).Scan(&status))
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT refunded_minor FROM payroll_entry WHERE id=$1`, entryID).Scan(&refunded))
	assert.Equal(t, string(payroll.DisputeStatusOpen), status)
	assert.Equal(t, int64(0), refunded)
}

// ResolveDispute refund where the order has already left picked_up/no_show:
// refunded_minor still bumps but the order is NOT re-transitioned.
func TestService_ResolveDispute_RefundOrderAlreadyRefunded(t *testing.T) {
	pool, svc, cleanup := setup(t)
	defer cleanup()
	ctx := context.Background()

	start, end := aprilPeriod()
	vendor := seedVendor(t, pool)
	user := seedEmployeeUser(t, pool)
	admin := seedAdminUser(t, pool)
	orderID := seedPickedUpOrder(t, pool, user, vendor, start.AddDate(0, 0, 4), 15000)

	batch, err := svc.BuildDraft(ctx, payroll.BuildDraftInput{PeriodStart: start, PeriodEnd: end})
	require.NoError(t, err)
	entries, err := svc.ListBatchEntries(ctx, batch.ID)
	require.NoError(t, err)
	entryID := entries[0].ID

	d, err := svc.OpenDispute(ctx, payroll.OpenDisputeInput{
		EntryID: entryID, OrderID: orderID, OpenedBy: user, Reason: "claim",
	})
	require.NoError(t, err)

	// Pre-refund the order so the resolution's status guard skips the transition.
	_, err = pool.Exec(ctx, `UPDATE "order" SET status='refunded' WHERE id=$1`, orderID)
	require.NoError(t, err)

	require.NoError(t, svc.ResolveDispute(ctx, payroll.ResolveDisputeInput{
		DisputeID: d.ID, ResolvedBy: admin, Status: payroll.DisputeStatusResolvedRefund,
		Resolution: "refund", RefundMinor: 5000,
	}))

	var refunded int64
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT refunded_minor FROM payroll_entry WHERE id=$1`, entryID).Scan(&refunded))
	assert.Equal(t, int64(5000), refunded)
}

// ---------- ListBatches / ListDisputes ----------

func TestService_ListBatches(t *testing.T) {
	_, svc, cleanup := setup(t)
	defer cleanup()
	ctx := context.Background()

	start, end := aprilPeriod()
	batch, err := svc.BuildDraft(ctx, payroll.BuildDraftInput{PeriodStart: start, PeriodEnd: end})
	require.NoError(t, err)

	all, err := svc.ListBatches(ctx, nil)
	require.NoError(t, err)
	require.Len(t, all, 1)
	assert.Equal(t, batch.ID, all[0].ID)

	drafts, err := svc.ListBatches(ctx, []payroll.BatchStatus{payroll.BatchStatusDraft})
	require.NoError(t, err)
	require.Len(t, drafts, 1)

	locked, err := svc.ListBatches(ctx, []payroll.BatchStatus{payroll.BatchStatusLocked})
	require.NoError(t, err)
	assert.Empty(t, locked)
}

func TestService_ListDisputes(t *testing.T) {
	pool, svc, cleanup := setup(t)
	defer cleanup()
	ctx := context.Background()

	start, end := aprilPeriod()
	vendor := seedVendor(t, pool)
	user := seedEmployeeUser(t, pool)
	orderID := seedPickedUpOrder(t, pool, user, vendor, start.AddDate(0, 0, 4), 15000)

	batch, err := svc.BuildDraft(ctx, payroll.BuildDraftInput{PeriodStart: start, PeriodEnd: end})
	require.NoError(t, err)
	entries, err := svc.ListBatchEntries(ctx, batch.ID)
	require.NoError(t, err)

	d, err := svc.OpenDispute(ctx, payroll.OpenDisputeInput{
		EntryID: entries[0].ID, OrderID: orderID, OpenedBy: user, Reason: "claim",
	})
	require.NoError(t, err)

	all, err := svc.ListDisputes(ctx, nil)
	require.NoError(t, err)
	require.Len(t, all, 1)
	assert.Equal(t, d.ID, all[0].ID)

	open, err := svc.ListDisputes(ctx, []payroll.DisputeStatus{payroll.DisputeStatusOpen})
	require.NoError(t, err)
	require.Len(t, open, 1)

	resolved, err := svc.ListDisputes(ctx, []payroll.DisputeStatus{payroll.DisputeStatusResolvedRefund})
	require.NoError(t, err)
	assert.Empty(t, resolved)
}

// ---------- ListCurrentLines via repo ----------

// Exercises the CurrentLines-repo branch of ListCurrentLines (the other branch,
// the Pool fallback, is covered by TestService_ReverseOrder_CurrentPeriodNoEntry).
func TestService_ListCurrentLines_ViaRepo(t *testing.T) {
	pool, svc, cleanup := setup(t)
	defer cleanup()
	ctx := context.Background()

	svc.CurrentLines = ppg.NewCurrentLinesRepo(pool)

	vendor := seedVendor(t, pool)
	user := seedEmployeeUser(t, pool)
	day := time.Date(2026, time.May, 12, 0, 0, 0, 0, time.UTC)
	orderID := seedPickedUpOrder(t, pool, user, vendor, day, 9000)

	lines, err := svc.ListCurrentLines(ctx, user)
	require.NoError(t, err)
	require.Len(t, lines, 1)
	assert.Equal(t, orderID, lines[0].OrderID)
	assert.Equal(t, "charged", lines[0].Status)
	assert.Equal(t, int64(9000), lines[0].AmountMinor)
}

// ---------- ListExceptions error path ----------

func TestService_ListExceptions_BatchNotFound(t *testing.T) {
	_, svc, cleanup := setup(t)
	defer cleanup()
	ctx := context.Background()

	_, err := svc.ListExceptions(ctx, missingUUID)
	assert.ErrorIs(t, err, payroll.ErrBatchNotFound)
}

// ---------- FlagException entry-not-in-batch ----------

func TestService_FlagException_EntryNotInBatch(t *testing.T) {
	pool, svc, cleanup := setup(t)
	defer cleanup()
	ctx := context.Background()

	start, end := aprilPeriod()
	vendor := seedVendor(t, pool)
	user := seedEmployeeUser(t, pool)
	admin := seedAdminUser(t, pool)
	seedPickedUpOrder(t, pool, user, vendor, start.AddDate(0, 0, 4), 15000)

	batch, err := svc.BuildDraft(ctx, payroll.BuildDraftInput{PeriodStart: start, PeriodEnd: end})
	require.NoError(t, err)
	entries, err := svc.ListBatchEntries(ctx, batch.ID)
	require.NoError(t, err)
	require.Len(t, entries, 1)

	// Entry exists but the supplied batch id is a different (valid) batch id.
	_, err = svc.FlagException(ctx, payroll.FlagExceptionInput{
		BatchID: missingUUID, EntryID: entries[0].ID, Detail: "x", FlaggedBy: admin,
	})
	assert.ErrorIs(t, err, payroll.ErrInvalidException)
}

// ---------- ResolveException error paths ----------

func TestService_ResolveException_InvalidStatus(t *testing.T) {
	pool, svc, cleanup := setup(t)
	defer cleanup()
	ctx := context.Background()
	admin := seedAdminUser(t, pool)

	err := svc.ResolveException(ctx, missingUUID, payroll.ExceptionOpen, "", admin)
	assert.ErrorIs(t, err, payroll.ErrInvalidException)
}

func TestService_ResolveException_NotFound(t *testing.T) {
	pool, svc, cleanup := setup(t)
	defer cleanup()
	ctx := context.Background()
	admin := seedAdminUser(t, pool)

	err := svc.ResolveException(ctx, missingUUID, payroll.ExceptionResolved, "ok", admin)
	assert.ErrorIs(t, err, payroll.ErrExceptionNotFound)
}

func TestService_ResolveException_Resolved(t *testing.T) {
	pool, svc, cleanup := setup(t)
	defer cleanup()
	ctx := context.Background()

	departed := seedEmployeeUser(t, pool)
	_, err := pool.Exec(ctx, `UPDATE "user" SET status='terminated' WHERE id=$1`, departed)
	require.NoError(t, err)
	vendor := seedVendor(t, pool)
	admin := seedAdminUser(t, pool)

	start := time.Date(2026, time.August, 1, 0, 0, 0, 0, time.UTC)
	end := start.AddDate(0, 1, -1)
	day := time.Date(2026, time.August, 10, 0, 0, 0, 0, time.UTC)
	seedPickedUpOrder(t, pool, departed, vendor, day, 12000)

	batch, err := svc.BuildDraft(ctx, payroll.BuildDraftInput{PeriodStart: start, PeriodEnd: end})
	require.NoError(t, err)
	exs, err := svc.ListExceptions(ctx, batch.ID)
	require.NoError(t, err)
	require.Len(t, exs, 1)

	require.NoError(t, svc.ResolveException(ctx, exs[0].ID, payroll.ExceptionResolved, "handled manually", admin))

	exs, err = svc.ListExceptions(ctx, batch.ID)
	require.NoError(t, err)
	require.Len(t, exs, 1)
	assert.Equal(t, payroll.ExceptionResolved, exs[0].Status)
}

// ---------- ReverseOrder error path ----------

func TestService_ReverseOrder_OrderNotFound(t *testing.T) {
	_, svc, cleanup := setup(t)
	defer cleanup()
	ctx := context.Background()

	err := svc.ReverseOrder(ctx, missingUUID)
	assert.ErrorIs(t, err, order.ErrOrderNotFound)
}

// ---------- in-transaction repo failure paths ----------

// execErrTx is a pgx.Tx whose every statement fails, so the first repo write
// inside a BeginFunc closure returns its wrapped error — exercising the
// in-transaction error branches of the write paths.
type execErrTx struct{ err error }

func (t execErrTx) Begin(ctx context.Context) (pgx.Tx, error) { return nil, t.err }
func (t execErrTx) Commit(ctx context.Context) error          { return nil }
func (t execErrTx) Rollback(ctx context.Context) error        { return nil }
func (t execErrTx) LargeObjects() pgx.LargeObjects            { return pgx.LargeObjects{} }
func (t execErrTx) Conn() *pgx.Conn                           { return nil }
func (t execErrTx) SendBatch(ctx context.Context, b *pgx.Batch) pgx.BatchResults {
	return nil
}
func (t execErrTx) CopyFrom(ctx context.Context, tn pgx.Identifier, cols []string, src pgx.CopyFromSource) (int64, error) {
	return 0, t.err
}
func (t execErrTx) Prepare(ctx context.Context, name, sql string) (*pgconn.StatementDescription, error) {
	return nil, t.err
}
func (t execErrTx) Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	return pgconn.CommandTag{}, t.err
}
func (t execErrTx) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	return nil, t.err
}
func (t execErrTx) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	return errRow{err: t.err}
}

type errRow struct{ err error }

func (r errRow) Scan(dest ...any) error { return r.err }

// execErrPool hands BeginFunc an execErrTx so the closure runs but every write fails.
type execErrPool struct{ err error }

func (p execErrPool) Begin(ctx context.Context) (pgx.Tx, error) { return execErrTx{err: p.err}, nil }
func (p execErrPool) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	return nil, p.err
}

func TestService_BuildDraft_EntryWriteError(t *testing.T) {
	_, svc, cleanup := setup(t)
	defer cleanup()
	ctx := context.Background()

	want := errors.New("write boom")
	svc.Pool = execErrPool{err: want}

	start := time.Date(2026, time.November, 1, 0, 0, 0, 0, time.UTC)
	end := start.AddDate(0, 1, -1)
	_, err := svc.BuildDraft(ctx, payroll.BuildDraftInput{PeriodStart: start, PeriodEnd: end})
	assert.ErrorIs(t, err, want)
}

func TestService_Lock_StatusWriteError(t *testing.T) {
	pool, svc, cleanup := setup(t)
	defer cleanup()
	ctx := context.Background()

	start, end := aprilPeriod()
	admin := seedAdminUser(t, pool)
	batch, err := svc.BuildDraft(ctx, payroll.BuildDraftInput{PeriodStart: start, PeriodEnd: end})
	require.NoError(t, err)

	want := errors.New("write boom")
	svc.Pool = execErrPool{err: want}
	assert.ErrorIs(t, svc.Lock(ctx, batch.ID, admin), want)
}

func TestService_ResolveDispute_WriteError(t *testing.T) {
	pool, svc, cleanup := setup(t)
	defer cleanup()
	ctx := context.Background()

	start, end := aprilPeriod()
	vendor := seedVendor(t, pool)
	user := seedEmployeeUser(t, pool)
	admin := seedAdminUser(t, pool)
	orderID := seedPickedUpOrder(t, pool, user, vendor, start.AddDate(0, 0, 4), 15000)

	batch, err := svc.BuildDraft(ctx, payroll.BuildDraftInput{PeriodStart: start, PeriodEnd: end})
	require.NoError(t, err)
	entries, err := svc.ListBatchEntries(ctx, batch.ID)
	require.NoError(t, err)

	d, err := svc.OpenDispute(ctx, payroll.OpenDisputeInput{
		EntryID: entries[0].ID, OrderID: orderID, OpenedBy: user, Reason: "claim",
	})
	require.NoError(t, err)

	want := errors.New("write boom")
	svc.Pool = execErrPool{err: want}
	err = svc.ResolveDispute(ctx, payroll.ResolveDisputeInput{
		DisputeID: d.ID, ResolvedBy: admin, Status: payroll.DisputeStatusResolvedReject, Resolution: "x",
	})
	assert.ErrorIs(t, err, want)
}

func TestService_ReverseOrder_WriteError(t *testing.T) {
	pool, svc, cleanup := setup(t)
	defer cleanup()
	ctx := context.Background()

	vendor := seedVendor(t, pool)
	user := seedEmployeeUser(t, pool)
	day := time.Date(2026, time.May, 12, 0, 0, 0, 0, time.UTC)
	orderID := seedPickedUpOrder(t, pool, user, vendor, day, 9000)

	want := errors.New("write boom")
	svc.Pool = execErrPool{err: want}
	assert.ErrorIs(t, svc.ReverseOrder(ctx, orderID), want)
}

func TestService_FlagException_AuditWriteError(t *testing.T) {
	pool, svc, cleanup := setup(t)
	defer cleanup()
	ctx := context.Background()

	start, end := aprilPeriod()
	vendor := seedVendor(t, pool)
	user := seedEmployeeUser(t, pool)
	admin := seedAdminUser(t, pool)
	seedPickedUpOrder(t, pool, user, vendor, start.AddDate(0, 0, 4), 15000)

	batch, err := svc.BuildDraft(ctx, payroll.BuildDraftInput{PeriodStart: start, PeriodEnd: end})
	require.NoError(t, err)
	entries, err := svc.ListBatchEntries(ctx, batch.ID)
	require.NoError(t, err)
	require.Len(t, entries, 1)

	want := errors.New("write boom")
	svc.Pool = execErrPool{err: want}
	_, err = svc.FlagException(ctx, payroll.FlagExceptionInput{
		BatchID: batch.ID, EntryID: entries[0].ID, Detail: "x", FlaggedBy: admin,
	})
	assert.ErrorIs(t, err, want)
}

func TestService_ResolveException_AuditWriteError(t *testing.T) {
	pool, svc, cleanup := setup(t)
	defer cleanup()
	ctx := context.Background()

	departed := seedEmployeeUser(t, pool)
	_, err := pool.Exec(ctx, `UPDATE "user" SET status='terminated' WHERE id=$1`, departed)
	require.NoError(t, err)
	vendor := seedVendor(t, pool)
	admin := seedAdminUser(t, pool)

	start := time.Date(2026, time.October, 1, 0, 0, 0, 0, time.UTC)
	end := start.AddDate(0, 1, -1)
	day := time.Date(2026, time.October, 10, 0, 0, 0, 0, time.UTC)
	seedPickedUpOrder(t, pool, departed, vendor, day, 12000)

	batch, err := svc.BuildDraft(ctx, payroll.BuildDraftInput{PeriodStart: start, PeriodEnd: end})
	require.NoError(t, err)
	exs, err := svc.ListExceptions(ctx, batch.ID)
	require.NoError(t, err)
	require.Len(t, exs, 1)

	want := errors.New("write boom")
	svc.Pool = execErrPool{err: want}
	assert.ErrorIs(t, svc.ResolveException(ctx, exs[0].ID, payroll.ExceptionResolved, "x", admin), want)
}

// ---------- QueryCurrentLines error paths (fakes) ----------

type errQuerier struct{ err error }

func (e errQuerier) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	return nil, e.err
}

func TestQueryCurrentLines_QueryError(t *testing.T) {
	_, err := payroll.QueryCurrentLines(context.Background(), errQuerier{err: errors.New("boom")}, "u1")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "query current lines")
}

// scanErrRows is a minimal pgx.Rows that yields exactly one row whose Scan
// fails, exercising the scan-error branch of QueryCurrentLines.
type scanErrRows struct{ advanced bool }

func (r *scanErrRows) Close()                                       {}
func (r *scanErrRows) Err() error                                   { return nil }
func (r *scanErrRows) CommandTag() pgconn.CommandTag                { return pgconn.CommandTag{} }
func (r *scanErrRows) FieldDescriptions() []pgconn.FieldDescription { return nil }
func (r *scanErrRows) Next() bool {
	if r.advanced {
		return false
	}
	r.advanced = true
	return true
}
func (r *scanErrRows) Scan(dest ...any) error { return errors.New("scan boom") }
func (r *scanErrRows) Values() ([]any, error) { return nil, nil }
func (r *scanErrRows) RawValues() [][]byte    { return nil }
func (r *scanErrRows) Conn() *pgx.Conn        { return nil }

type scanErrQuerier struct{}

func (scanErrQuerier) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	return &scanErrRows{}, nil
}

func TestQueryCurrentLines_ScanError(t *testing.T) {
	_, err := payroll.QueryCurrentLines(context.Background(), scanErrQuerier{}, "u1")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "scan current line")
}
