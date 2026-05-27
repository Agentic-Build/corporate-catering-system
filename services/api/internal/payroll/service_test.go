package payroll_test

import (
	"context"
	"fmt"
	"path/filepath"
	"runtime"
	"sync/atomic"
	"testing"
	"time"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/takalawang/corporate-catering-system/services/api/internal/order"
	opg "github.com/takalawang/corporate-catering-system/services/api/internal/order/postgres"
	"github.com/takalawang/corporate-catering-system/services/api/internal/payroll"
	ppg "github.com/takalawang/corporate-catering-system/services/api/internal/payroll/postgres"
)

type fixedClock struct{ T time.Time }

func (c fixedClock) Now() time.Time { return c.T }

func migrationsDir() string {
	_, thisFile, _, _ := runtime.Caller(0)
	// services/api/internal/payroll/service_test.go → ../../../../migrations
	return filepath.Join(filepath.Dir(thisFile), "..", "..", "..", "..", "migrations")
}

func setup(t *testing.T) (*pgxpool.Pool, *payroll.Service, func()) {
	t.Helper()
	ctx := context.Background()
	container, err := tcpostgres.Run(ctx,
		"postgres:16-alpine",
		tcpostgres.WithDatabase("tbite"),
		tcpostgres.WithUsername("tbite"),
		tcpostgres.WithPassword("tbite"),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).
				WithStartupTimeout(60*time.Second)),
	)
	require.NoError(t, err)
	dsn, err := container.ConnectionString(ctx, "sslmode=disable")
	require.NoError(t, err)
	m, err := migrate.New("file://"+migrationsDir(), dsn)
	require.NoError(t, err)
	require.NoError(t, m.Up())
	cfg, err := pgxpool.ParseConfig(dsn)
	require.NoError(t, err)
	cfg.MaxConns = 20
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	require.NoError(t, err)

	orderRepo := opg.NewOrderRepo(pool)
	auditRepo := opg.NewAuditRepo(pool)
	outboxRepo := opg.NewOutboxRepo(pool)

	svc := &payroll.Service{
		Pool:       pool,
		Batches:    ppg.NewBatchRepo(pool),
		Entries:    ppg.NewEntryRepo(pool),
		Disputes:   ppg.NewDisputeRepo(pool),
		Exceptions: ppg.NewExceptionRepo(pool),
		Orders:     orderRepo,
		OrderTx:    orderRepo,
		Audit:      auditRepo,
		Outbox:     outboxRepo,
		Clock:      fixedClock{T: time.Date(2026, 5, 13, 12, 0, 0, 0, time.UTC)},
	}
	cleanup := func() {
		pool.Close()
		_ = container.Terminate(ctx)
	}
	return pool, svc, cleanup
}

var (
	userCounter   atomic.Uint64
	vendorCounter atomic.Uint64
	itemCounter   atomic.Uint64
)

func seedAdminUser(t *testing.T, pool *pgxpool.Pool) string {
	t.Helper()
	return seedUserWithRole(t, pool, "welfare_admin")
}

func seedEmployeeUser(t *testing.T, pool *pgxpool.Pool) string {
	t.Helper()
	return seedUserWithRole(t, pool, "employee")
}

func seedUserWithRole(t *testing.T, pool *pgxpool.Pool, role string) string {
	t.Helper()
	n := userCounter.Add(1)
	var id string
	err := pool.QueryRow(context.Background(), `
INSERT INTO "user" (primary_email, display_name, role)
VALUES ($1, $2, $3) RETURNING id`,
		fmt.Sprintf("payroll-svc-user-%d@test.com", n),
		fmt.Sprintf("payroll-svc-user-%d", n),
		role,
	).Scan(&id)
	require.NoError(t, err)
	return id
}

func seedVendor(t *testing.T, pool *pgxpool.Pool) string {
	t.Helper()
	n := vendorCounter.Add(1)
	var id string
	err := pool.QueryRow(context.Background(), `
INSERT INTO vendor (display_name, legal_name, contact_email, status)
VALUES ($1, $2, $3, 'approved') RETURNING id`,
		fmt.Sprintf("payroll-svc-vendor-%d", n),
		fmt.Sprintf("payroll-svc-vendor-%d Ltd", n),
		fmt.Sprintf("payroll-svc-vendor-%d@test.com", n),
	).Scan(&id)
	require.NoError(t, err)
	return id
}

func seedMenuItem(t *testing.T, pool *pgxpool.Pool, vendorID string, priceMinor int64) string {
	t.Helper()
	n := itemCounter.Add(1)
	var id string
	err := pool.QueryRow(context.Background(), `
INSERT INTO menu_item (vendor_id, name, description, price_minor, status, tags)
VALUES ($1, $2, '', $3, 'active', '{}') RETURNING id`,
		vendorID, fmt.Sprintf("payroll-svc-item-%d", n), priceMinor,
	).Scan(&id)
	require.NoError(t, err)
	return id
}

// seedOrderWithStatus inserts an order row directly so tests can land orders
// in picked_up / no_show without going through service.Place (which requires
// quota + plant mapping + clock control).
func seedOrderWithStatus(t *testing.T, pool *pgxpool.Pool, userID, vendorID string, supplyDate time.Time, amount int64, status order.Status) string {
	t.Helper()
	secret := make([]byte, 32)
	for i := range secret {
		secret[i] = 0xab
	}
	var id string
	err := pool.QueryRow(context.Background(), `
INSERT INTO "order"
  (user_id, vendor_id, plant, supply_date, status, total_price_minor, placed_at, cutoff_at, totp_secret)
VALUES ($1, $2, 'F12B-3F', $3, $4::order_status, $5, $6, $7, $8) RETURNING id`,
		userID, vendorID, supplyDate, string(status), amount,
		supplyDate.Add(-6*time.Hour), supplyDate.Add(-1*time.Hour), secret,
	).Scan(&id)
	require.NoError(t, err)
	return id
}

func seedPickedUpOrder(t *testing.T, pool *pgxpool.Pool, userID, vendorID string, supplyDate time.Time, amount int64) string {
	return seedOrderWithStatus(t, pool, userID, vendorID, supplyDate, amount, order.StatusPickedUp)
}

func aprilPeriod() (time.Time, time.Time) {
	start := time.Date(2026, time.April, 1, 0, 0, 0, 0, time.UTC)
	end := start.AddDate(0, 1, -1)
	return start, end
}

// ---------- BuildDraft tests ----------

func TestService_BuildDraft_AggregatesByUser(t *testing.T) {
	pool, svc, cleanup := setup(t)
	defer cleanup()
	ctx := context.Background()

	start, end := aprilPeriod()
	vendor := seedVendor(t, pool)
	_ = seedMenuItem(t, pool, vendor, 12000) // referenced indirectly; not required by seed

	userA := seedEmployeeUser(t, pool)
	userB := seedEmployeeUser(t, pool)

	// User A: 2 picked_up + 1 no_show in April
	a1 := seedPickedUpOrder(t, pool, userA, vendor, start.AddDate(0, 0, 2), 12000)
	a2 := seedPickedUpOrder(t, pool, userA, vendor, start.AddDate(0, 0, 5), 13000)
	a3 := seedOrderWithStatus(t, pool, userA, vendor, start.AddDate(0, 0, 7), 5000, order.StatusNoShow)

	// User B: 1 picked_up in April
	b1 := seedPickedUpOrder(t, pool, userB, vendor, start.AddDate(0, 0, 10), 20000)

	// Out-of-period order (May 1) — must NOT be aggregated.
	_ = seedPickedUpOrder(t, pool, userA, vendor, end.AddDate(0, 0, 1), 99999)

	// Out-of-status order (placed) — must NOT be aggregated.
	_ = seedOrderWithStatus(t, pool, userA, vendor, start.AddDate(0, 0, 3), 77777, order.StatusPlaced)

	batch, err := svc.BuildDraft(ctx, payroll.BuildDraftInput{PeriodStart: start, PeriodEnd: end})
	require.NoError(t, err)
	require.NotEmpty(t, batch.ID)
	assert.Equal(t, payroll.BatchStatusDraft, batch.Status)

	entries, err := svc.ListBatchEntries(ctx, batch.ID)
	require.NoError(t, err)
	require.Len(t, entries, 2, "exactly one entry per user with in-period activity")

	byUser := map[string]*payroll.Entry{}
	for _, e := range entries {
		byUser[e.UserID] = e
	}

	ea := byUser[userA]
	require.NotNil(t, ea)
	assert.Equal(t, int64(12000+13000+5000), ea.AmountMinor)
	assert.ElementsMatch(t, []string{a1, a2, a3}, ea.OrderIDs)

	eb := byUser[userB]
	require.NotNil(t, eb)
	assert.Equal(t, int64(20000), eb.AmountMinor)
	assert.ElementsMatch(t, []string{b1}, eb.OrderIDs)
}

func TestService_BuildDraft_DuplicatePeriod_Rejected(t *testing.T) {
	_, svc, cleanup := setup(t)
	defer cleanup()
	ctx := context.Background()
	start, end := aprilPeriod()

	_, err := svc.BuildDraft(ctx, payroll.BuildDraftInput{PeriodStart: start, PeriodEnd: end})
	require.NoError(t, err)

	_, err = svc.BuildDraft(ctx, payroll.BuildDraftInput{PeriodStart: start, PeriodEnd: end})
	assert.ErrorIs(t, err, payroll.ErrBatchPeriodExists)
}

// ---------- Lock tests ----------

func TestService_Lock_HappyPath(t *testing.T) {
	pool, svc, cleanup := setup(t)
	defer cleanup()
	ctx := context.Background()
	start, end := aprilPeriod()
	admin := seedAdminUser(t, pool)

	batch, err := svc.BuildDraft(ctx, payroll.BuildDraftInput{PeriodStart: start, PeriodEnd: end})
	require.NoError(t, err)

	require.NoError(t, svc.Lock(ctx, batch.ID, admin))

	got, err := svc.GetBatch(ctx, batch.ID)
	require.NoError(t, err)
	assert.Equal(t, payroll.BatchStatusLocked, got.Status)
	require.NotNil(t, got.LockedBy)
	assert.Equal(t, admin, *got.LockedBy)
	require.NotNil(t, got.LockedAt)

	var auditCount, outboxCount int
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT count(*) FROM audit_event WHERE target_id=$1 AND action='payroll.lock'`, batch.ID).
		Scan(&auditCount))
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT count(*) FROM outbox_event WHERE aggregate_id::text=$1 AND subject='payroll.batch_locked.v1'`,
		batch.ID).Scan(&outboxCount))
	assert.Equal(t, 1, auditCount, "expected exactly one audit row for lock")
	assert.Equal(t, 1, outboxCount, "expected exactly one outbox row for lock")
}

func TestService_Lock_AlreadyLocked(t *testing.T) {
	pool, svc, cleanup := setup(t)
	defer cleanup()
	ctx := context.Background()
	start, end := aprilPeriod()
	admin := seedAdminUser(t, pool)

	batch, err := svc.BuildDraft(ctx, payroll.BuildDraftInput{PeriodStart: start, PeriodEnd: end})
	require.NoError(t, err)
	require.NoError(t, svc.Lock(ctx, batch.ID, admin))

	err = svc.Lock(ctx, batch.ID, admin)
	assert.ErrorIs(t, err, payroll.ErrBatchLocked)
}

// ---------- OpenDispute tests ----------

func TestService_OpenDispute_Happy(t *testing.T) {
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
	entryID := entries[0].ID

	d, err := svc.OpenDispute(ctx, payroll.OpenDisputeInput{
		EntryID:  entryID,
		OrderID:  orderID,
		OpenedBy: user,
		Reason:   "missing dessert",
	})
	require.NoError(t, err)
	require.NotEmpty(t, d.ID)
	assert.Equal(t, payroll.DisputeStatusOpen, d.Status)
	require.NotNil(t, d.EntryID)
	assert.Equal(t, entryID, *d.EntryID)
	assert.Equal(t, orderID, d.OrderID)
	assert.Equal(t, user, d.OpenedBy)

	mine, err := svc.ListMyDisputes(ctx, user)
	require.NoError(t, err)
	require.Len(t, mine, 1)
	assert.Equal(t, d.ID, mine[0].ID)

	// Verify dispute persisted by FK count
	var rowCount int
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT count(*) FROM payroll_dispute WHERE id=$1`, d.ID).Scan(&rowCount))
	assert.Equal(t, 1, rowCount)
}

func TestService_OpenDispute_NotOwner(t *testing.T) {
	pool, svc, cleanup := setup(t)
	defer cleanup()
	ctx := context.Background()

	start, end := aprilPeriod()
	vendor := seedVendor(t, pool)
	owner := seedEmployeeUser(t, pool)
	intruder := seedEmployeeUser(t, pool)
	orderID := seedPickedUpOrder(t, pool, owner, vendor, start.AddDate(0, 0, 4), 15000)

	batch, err := svc.BuildDraft(ctx, payroll.BuildDraftInput{PeriodStart: start, PeriodEnd: end})
	require.NoError(t, err)
	entries, err := svc.ListBatchEntries(ctx, batch.ID)
	require.NoError(t, err)
	require.Len(t, entries, 1)
	entryID := entries[0].ID

	_, err = svc.OpenDispute(ctx, payroll.OpenDisputeInput{
		EntryID:  entryID,
		OrderID:  orderID,
		OpenedBy: intruder,
		Reason:   "not mine",
	})
	assert.ErrorIs(t, err, payroll.ErrForbidden)
}

// TestService_OpenDisputeByOrder_CurrentPeriodNoEntry verifies a current-period
// order (no payroll entry built yet) can still be disputed; the dispute is
// created with a NULL entry_id linked only to the order.
func TestService_OpenDisputeByOrder_CurrentPeriodNoEntry(t *testing.T) {
	pool, svc, cleanup := setup(t)
	defer cleanup()
	ctx := context.Background()

	vendor := seedVendor(t, pool)
	user := seedEmployeeUser(t, pool)
	// no_show order, but no batch is built → no payroll entry exists.
	orderID := seedOrderWithStatus(t, pool, user, vendor,
		time.Date(2026, 5, 20, 0, 0, 0, 0, time.UTC), 15000, order.StatusNoShow)

	d, err := svc.OpenDisputeByOrder(ctx, orderID, user, "沒拿到餐")
	require.NoError(t, err)
	require.NotEmpty(t, d.ID)
	assert.Equal(t, payroll.DisputeStatusOpen, d.Status)
	assert.Equal(t, orderID, d.OrderID)
	assert.Equal(t, user, d.OpenedBy)
	assert.Nil(t, d.EntryID, "entry_id should be nil for a current-period dispute")

	// Persisted and visible in the employee's dispute list.
	mine, err := svc.ListMyDisputes(ctx, user)
	require.NoError(t, err)
	require.Len(t, mine, 1)
	assert.Equal(t, d.ID, mine[0].ID)
	assert.Nil(t, mine[0].EntryID)
}

// TestService_OpenDisputeByOrder_CurrentPeriodNotOwner verifies a non-owner
// cannot open an entry-less dispute against someone else's order.
func TestService_OpenDisputeByOrder_CurrentPeriodNotOwner(t *testing.T) {
	pool, svc, cleanup := setup(t)
	defer cleanup()
	ctx := context.Background()

	vendor := seedVendor(t, pool)
	owner := seedEmployeeUser(t, pool)
	intruder := seedEmployeeUser(t, pool)
	orderID := seedOrderWithStatus(t, pool, owner, vendor,
		time.Date(2026, 5, 20, 0, 0, 0, 0, time.UTC), 15000, order.StatusNoShow)

	_, err := svc.OpenDisputeByOrder(ctx, orderID, intruder, "not mine")
	assert.ErrorIs(t, err, payroll.ErrForbidden)
}

// TestService_OpenDisputeByOrder_NotDisputableStatus verifies a current-period
// order that was never charged (e.g. still placed) cannot be disputed — opening
// one would record a refund ResolveDispute could never apply.
func TestService_OpenDisputeByOrder_NotDisputableStatus(t *testing.T) {
	pool, svc, cleanup := setup(t)
	defer cleanup()
	ctx := context.Background()

	vendor := seedVendor(t, pool)
	user := seedEmployeeUser(t, pool)
	orderID := seedOrderWithStatus(t, pool, user, vendor,
		time.Date(2026, 5, 20, 0, 0, 0, 0, time.UTC), 15000, order.StatusPlaced)

	_, err := svc.OpenDisputeByOrder(ctx, orderID, user, "too early")
	assert.ErrorIs(t, err, payroll.ErrOrderNotDisputable)
}

// ---------- ResolveDispute tests ----------

func TestService_ResolveDispute_Refund(t *testing.T) {
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
		EntryID: entryID, OrderID: orderID, OpenedBy: user, Reason: "missing dessert",
	})
	require.NoError(t, err)

	require.NoError(t, svc.ResolveDispute(ctx, payroll.ResolveDisputeInput{
		DisputeID:   d.ID,
		ResolvedBy:  admin,
		Status:      payroll.DisputeStatusResolvedRefund,
		Resolution:  "verified, refund full amount",
		RefundMinor: 15000,
	}))

	// Dispute now resolved_refund
	var status string
	var refund int64
	var resolvedBy *string
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT status::text, refund_minor, resolved_by::text FROM payroll_dispute WHERE id=$1`, d.ID).
		Scan(&status, &refund, &resolvedBy))
	assert.Equal(t, string(payroll.DisputeStatusResolvedRefund), status)
	assert.Equal(t, int64(15000), refund)
	require.NotNil(t, resolvedBy)
	assert.Equal(t, admin, *resolvedBy)

	// Entry refunded_minor bumped
	var refunded int64
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT refunded_minor FROM payroll_entry WHERE id=$1`, entryID).Scan(&refunded))
	assert.Equal(t, int64(15000), refunded)

	// Order transitioned picked_up → refunded
	var orderStatus string
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT status::text FROM "order" WHERE id=$1`, orderID).Scan(&orderStatus))
	assert.Equal(t, string(order.StatusRefunded), orderStatus)

	// Outbox + audit emitted
	var auditCount, outboxCount int
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT count(*) FROM audit_event WHERE target_id=$1 AND action='payroll.dispute_resolve'`, d.ID).
		Scan(&auditCount))
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT count(*) FROM outbox_event WHERE aggregate_id::text=$1 AND subject='payroll.dispute_resolved.v1'`,
		d.ID).Scan(&outboxCount))
	assert.Equal(t, 1, auditCount)
	assert.Equal(t, 1, outboxCount)
}

func TestService_ResolveDispute_Reject(t *testing.T) {
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

	require.NoError(t, svc.ResolveDispute(ctx, payroll.ResolveDisputeInput{
		DisputeID:   d.ID,
		ResolvedBy:  admin,
		Status:      payroll.DisputeStatusResolvedReject,
		Resolution:  "evidence insufficient",
		RefundMinor: 0,
	}))

	// Dispute resolved_reject; entry refunded_minor unchanged; order unchanged.
	var status string
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT status::text FROM payroll_dispute WHERE id=$1`, d.ID).Scan(&status))
	assert.Equal(t, string(payroll.DisputeStatusResolvedReject), status)

	var refunded int64
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT refunded_minor FROM payroll_entry WHERE id=$1`, entryID).Scan(&refunded))
	assert.Equal(t, int64(0), refunded, "reject must not touch entry.refunded_minor")

	var orderStatus string
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT status::text FROM "order" WHERE id=$1`, orderID).Scan(&orderStatus))
	assert.Equal(t, string(order.StatusPickedUp), orderStatus, "reject must not transition order")
}

func TestService_ResolveDispute_RefundExceedsOrder_Rejected(t *testing.T) {
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
		EntryID: entryID, OrderID: orderID, OpenedBy: user, Reason: "overclaim",
	})
	require.NoError(t, err)

	// Refund 20000 exceeds the 15000 order total — must be rejected so the
	// HR-bound net deduction can never go negative.
	err = svc.ResolveDispute(ctx, payroll.ResolveDisputeInput{
		DisputeID:   d.ID,
		ResolvedBy:  admin,
		Status:      payroll.DisputeStatusResolvedRefund,
		Resolution:  "typo'd amount",
		RefundMinor: 20000,
	})
	assert.ErrorIs(t, err, payroll.ErrRefundExceedsOrder)

	// Nothing mutated: dispute still open, entry refund untouched.
	var status string
	var refunded int64
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT status::text FROM payroll_dispute WHERE id=$1`, d.ID).Scan(&status))
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT refunded_minor FROM payroll_entry WHERE id=$1`, entryID).Scan(&refunded))
	assert.Equal(t, string(payroll.DisputeStatusOpen), status)
	assert.Equal(t, int64(0), refunded)
}

// ---------- Exception list tests ----------

func TestService_Exceptions_DetectFlagResolve(t *testing.T) {
	pool, svc, cleanup := setup(t)
	defer cleanup()
	ctx := context.Background()

	activeUser := seedEmployeeUser(t, pool)
	departedUser := seedEmployeeUser(t, pool)
	_, err := pool.Exec(ctx, `UPDATE "user" SET status='terminated' WHERE id=$1`, departedUser)
	require.NoError(t, err)

	vendorID := seedVendor(t, pool)
	start := time.Date(2026, time.June, 1, 0, 0, 0, 0, time.UTC)
	end := start.AddDate(0, 1, -1)
	day := time.Date(2026, time.June, 10, 0, 0, 0, 0, time.UTC)
	seedPickedUpOrder(t, pool, activeUser, vendorID, day, 12000)
	seedPickedUpOrder(t, pool, departedUser, vendorID, day, 15000)

	batch, err := svc.BuildDraft(ctx, payroll.BuildDraftInput{PeriodStart: start, PeriodEnd: end})
	require.NoError(t, err)

	// BuildDraft auto-detected the departed employee's entry only.
	exs, err := svc.ListExceptions(ctx, batch.ID)
	require.NoError(t, err)
	require.Len(t, exs, 1)
	assert.Equal(t, payroll.ExceptionEmployeeDeparted, exs[0].Kind)
	assert.Equal(t, departedUser, exs[0].UserID)
	assert.Equal(t, payroll.ExceptionOpen, exs[0].Status)

	// Re-listing re-runs detection but must not duplicate.
	exs, err = svc.ListExceptions(ctx, batch.ID)
	require.NoError(t, err)
	require.Len(t, exs, 1)
	departedExID := exs[0].ID

	// Flag the active user's entry as a manual deduction failure.
	entries, err := svc.ListBatchEntries(ctx, batch.ID)
	require.NoError(t, err)
	var activeEntryID string
	for _, e := range entries {
		if e.UserID == activeUser {
			activeEntryID = e.ID
		}
	}
	require.NotEmpty(t, activeEntryID)
	admin := seedAdminUser(t, pool)
	flagged, err := svc.FlagException(ctx, payroll.FlagExceptionInput{
		BatchID: batch.ID, EntryID: activeEntryID, Detail: "銀行帳號錯誤", FlaggedBy: admin,
	})
	require.NoError(t, err)
	assert.Equal(t, payroll.ExceptionDeductionFailed, flagged.Kind)

	// Resolve the departed-employee exception as excluded.
	require.NoError(t, svc.ResolveException(ctx, departedExID, payroll.ExceptionExcluded, "員工已離職", admin))

	exs, err = svc.ListExceptions(ctx, batch.ID)
	require.NoError(t, err)
	require.Len(t, exs, 2)
	for _, e := range exs {
		if e.Kind == payroll.ExceptionEmployeeDeparted {
			assert.Equal(t, payroll.ExceptionExcluded, e.Status)
		}
	}

	// An invalid resolution status is rejected.
	err = svc.ResolveException(ctx, departedExID, payroll.ExceptionOpen, "", admin)
	assert.ErrorIs(t, err, payroll.ErrInvalidException)

	// Flagging an entry that is not part of the batch is rejected.
	_, err = svc.FlagException(ctx, payroll.FlagExceptionInput{
		BatchID: batch.ID, EntryID: "00000000-0000-0000-0000-000000000000", Detail: "x", FlaggedBy: admin,
	})
	require.Error(t, err)
}

// ---------- ReverseOrder tests ----------

// A charged order that is already part of a locked-batch entry, when reversed,
// transitions to refunded and bumps the entry's refunded_minor by the order's
// amount.
func TestService_ReverseOrder_LockedBatchEntry(t *testing.T) {
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
	entryID := entries[0].ID

	require.NoError(t, svc.ReverseOrder(ctx, orderID))

	// Order transitioned picked_up → refunded.
	var orderStatus string
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT status::text FROM "order" WHERE id=$1`, orderID).Scan(&orderStatus))
	assert.Equal(t, string(order.StatusRefunded), orderStatus)

	// Entry refunded_minor bumped by the full order amount.
	var refunded int64
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT refunded_minor FROM payroll_entry WHERE id=$1`, entryID).Scan(&refunded))
	assert.Equal(t, int64(15000), refunded)

	// Outbox + audit emitted.
	var auditCount, outboxCount int
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT count(*) FROM audit_event WHERE target_id=$1 AND action='payroll.order_reverse'`, orderID).
		Scan(&auditCount))
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT count(*) FROM outbox_event WHERE aggregate_id::text=$1 AND subject='payroll.order_reversed.v1'`,
		orderID).Scan(&outboxCount))
	assert.Equal(t, 1, auditCount)
	assert.Equal(t, 1, outboxCount)
}

// Reversing a charged order that is NOT yet part of any payroll entry (it falls
// in the in-progress, not-yet-locked period) just transitions it to refunded.
// B2's current-lines query maps refunded → reversed, and BuildDraft excludes
// refunded orders so it is never charged into a future batch.
func TestService_ReverseOrder_CurrentPeriodNoEntry(t *testing.T) {
	pool, svc, cleanup := setup(t)
	defer cleanup()
	ctx := context.Background()

	vendor := seedVendor(t, pool)
	user := seedEmployeeUser(t, pool)
	// Order in the current period — no batch built yet, so no entry exists.
	day := time.Date(2026, time.May, 12, 0, 0, 0, 0, time.UTC)
	orderID := seedPickedUpOrder(t, pool, user, vendor, day, 9000)

	require.NoError(t, svc.ReverseOrder(ctx, orderID))

	var orderStatus string
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT status::text FROM "order" WHERE id=$1`, orderID).Scan(&orderStatus))
	assert.Equal(t, string(order.StatusRefunded), orderStatus)

	// The reversed order surfaces in the current-lines view with status=reversed.
	lines, err := svc.ListCurrentLines(ctx, user)
	require.NoError(t, err)
	require.Len(t, lines, 1)
	assert.Equal(t, orderID, lines[0].OrderID)
	assert.Equal(t, "reversed", lines[0].Status)

	// A subsequent BuildDraft must NOT charge the refunded order.
	mayStart := time.Date(2026, time.May, 1, 0, 0, 0, 0, time.UTC)
	mayEnd := mayStart.AddDate(0, 1, -1)
	batch, err := svc.BuildDraft(ctx, payroll.BuildDraftInput{PeriodStart: mayStart, PeriodEnd: mayEnd})
	require.NoError(t, err)
	entries, err := svc.ListBatchEntries(ctx, batch.ID)
	require.NoError(t, err)
	assert.Empty(t, entries, "refunded order must not be aggregated into a batch")
}

// Reversing the same order twice must be a no-op the second time: the order is
// already refunded and refunded_minor must not double-count.
func TestService_ReverseOrder_Idempotent(t *testing.T) {
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
	entryID := entries[0].ID

	require.NoError(t, svc.ReverseOrder(ctx, orderID))
	// Second call: idempotent no-op.
	require.NoError(t, svc.ReverseOrder(ctx, orderID))

	var refunded int64
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT refunded_minor FROM payroll_entry WHERE id=$1`, entryID).Scan(&refunded))
	assert.Equal(t, int64(15000), refunded, "replayed reversal must not double-count")

	// Exactly one outbox + audit row — the no-op second call writes nothing.
	var auditCount, outboxCount int
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT count(*) FROM audit_event WHERE target_id=$1 AND action='payroll.order_reverse'`, orderID).
		Scan(&auditCount))
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT count(*) FROM outbox_event WHERE aggregate_id::text=$1 AND subject='payroll.order_reversed.v1'`,
		orderID).Scan(&outboxCount))
	assert.Equal(t, 1, auditCount)
	assert.Equal(t, 1, outboxCount)
}

// A no_show order is also chargeable, so it too can be reversed.
func TestService_ReverseOrder_NoShowOrder(t *testing.T) {
	pool, svc, cleanup := setup(t)
	defer cleanup()
	ctx := context.Background()

	vendor := seedVendor(t, pool)
	user := seedEmployeeUser(t, pool)
	day := time.Date(2026, time.May, 12, 0, 0, 0, 0, time.UTC)
	orderID := seedOrderWithStatus(t, pool, user, vendor, day, 7000, order.StatusNoShow)

	require.NoError(t, svc.ReverseOrder(ctx, orderID))

	var orderStatus string
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT status::text FROM "order" WHERE id=$1`, orderID).Scan(&orderStatus))
	assert.Equal(t, string(order.StatusRefunded), orderStatus)
}

// Reversing an order that was never charged (still placed) is rejected — there
// is no deduction to reverse.
func TestService_ReverseOrder_NotChargeable(t *testing.T) {
	pool, svc, cleanup := setup(t)
	defer cleanup()
	ctx := context.Background()

	vendor := seedVendor(t, pool)
	user := seedEmployeeUser(t, pool)
	day := time.Date(2026, time.May, 12, 0, 0, 0, 0, time.UTC)
	orderID := seedOrderWithStatus(t, pool, user, vendor, day, 7000, order.StatusPlaced)

	err := svc.ReverseOrder(ctx, orderID)
	assert.ErrorIs(t, err, payroll.ErrInvalidTransition)
}

func TestService_ListMyEntries(t *testing.T) {
	pool, svc, cleanup := setup(t)
	defer cleanup()
	ctx := context.Background()

	userA := seedEmployeeUser(t, pool)
	userB := seedEmployeeUser(t, pool)
	vendorID := seedVendor(t, pool)
	start := time.Date(2026, time.July, 1, 0, 0, 0, 0, time.UTC)
	end := start.AddDate(0, 1, -1)
	day := time.Date(2026, time.July, 9, 0, 0, 0, 0, time.UTC)
	seedPickedUpOrder(t, pool, userA, vendorID, day, 12000)
	seedPickedUpOrder(t, pool, userA, vendorID, day, 8000)
	seedPickedUpOrder(t, pool, userB, vendorID, day, 5000)

	batch, err := svc.BuildDraft(ctx, payroll.BuildDraftInput{PeriodStart: start, PeriodEnd: end})
	require.NoError(t, err)

	entries, err := svc.ListMyEntries(ctx, userA)
	require.NoError(t, err)
	require.Len(t, entries, 1)
	e := entries[0]
	assert.Equal(t, batch.ID, e.BatchID)
	assert.Equal(t, int64(20000), e.AmountMinor)
	assert.Equal(t, 2, e.OrderCount)
	assert.Equal(t, int64(0), e.RefundedMinor)
	assert.Equal(t, payroll.BatchStatusDraft, e.BatchStatus)
	assert.True(t, e.PeriodStart.Equal(start))

	// userB sees only their own entry.
	bEntries, err := svc.ListMyEntries(ctx, userB)
	require.NoError(t, err)
	require.Len(t, bEntries, 1)
	assert.Equal(t, int64(5000), bEntries[0].AmountMinor)
}
