package settlement_test

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

	opg "github.com/takalawang/corporate-catering-system/services/api/internal/order/postgres"
	"github.com/takalawang/corporate-catering-system/services/api/internal/settlement"
	spg "github.com/takalawang/corporate-catering-system/services/api/internal/settlement/postgres"
)

func migrationsDir() string {
	_, thisFile, _, _ := runtime.Caller(0)
	// services/api/internal/settlement/service_test.go → ../../../../migrations
	return filepath.Join(filepath.Dir(thisFile), "..", "..", "..", "..", "migrations")
}

func setup(t *testing.T) (*pgxpool.Pool, *settlement.Service, func()) {
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

	repo := spg.NewSettlementRepo(pool)
	svc := &settlement.Service{
		Pool:        pool,
		Settlements: repo,
		Orders:      repo,
		Audit:       opg.NewAuditRepo(pool),
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

func seedUserWithRole(t *testing.T, pool *pgxpool.Pool, role string) string {
	t.Helper()
	n := userCounter.Add(1)
	var id string
	err := pool.QueryRow(context.Background(), `
INSERT INTO "user" (primary_email, display_name, role)
VALUES ($1, $2, $3) RETURNING id`,
		fmt.Sprintf("settlement-svc-user-%d@test.com", n),
		fmt.Sprintf("settlement-svc-user-%d", n),
		role,
	).Scan(&id)
	require.NoError(t, err)
	return id
}

func seedAdminUser(t *testing.T, pool *pgxpool.Pool) string {
	return seedUserWithRole(t, pool, "welfare_admin")
}

func seedEmployeeUser(t *testing.T, pool *pgxpool.Pool) string {
	return seedUserWithRole(t, pool, "employee")
}

func seedVendor(t *testing.T, pool *pgxpool.Pool) string {
	t.Helper()
	n := vendorCounter.Add(1)
	var id string
	err := pool.QueryRow(context.Background(), `
INSERT INTO vendor (display_name, legal_name, contact_email, status)
VALUES ($1, $2, $3, 'approved') RETURNING id`,
		fmt.Sprintf("settlement-svc-vendor-%d", n),
		fmt.Sprintf("settlement-svc-vendor-%d Ltd", n),
		fmt.Sprintf("settlement-svc-vendor-%d@test.com", n),
	).Scan(&id)
	require.NoError(t, err)
	return id
}

func seedMenuItem(t *testing.T, pool *pgxpool.Pool, vendorID string, priceMinor int64) string {
	t.Helper()
	n := itemCounter.Add(1)
	var id string
	err := pool.QueryRow(context.Background(), `
INSERT INTO menu_item (vendor_id, name, description, price_minor, status, tags, badges)
VALUES ($1, $2, '', $3, 'active', '{}', '{}') RETURNING id`,
		vendorID, fmt.Sprintf("settlement-svc-item-%d", n), priceMinor,
	).Scan(&id)
	require.NoError(t, err)
	return id
}

// seedOrder inserts an order with status and (when qty > 0) a matching
// order_item so portion aggregation has data.
func seedOrder(t *testing.T, pool *pgxpool.Pool, userID, vendorID string, supplyDate time.Time, status string, amount int64, qty int) string {
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
		userID, vendorID, supplyDate, status, amount,
		supplyDate.Add(-6*time.Hour), supplyDate.Add(-1*time.Hour), secret,
	).Scan(&id)
	require.NoError(t, err)
	if qty > 0 {
		item := seedMenuItem(t, pool, vendorID, amount)
		_, err := pool.Exec(context.Background(), `
INSERT INTO order_item (order_id, menu_item_id, qty, unit_price_minor)
VALUES ($1, $2, $3, $4)`, id, item, qty, amount)
		require.NoError(t, err)
	}
	return id
}

func aprilPeriod() (time.Time, time.Time) {
	start := time.Date(2026, time.April, 1, 0, 0, 0, 0, time.UTC)
	end := start.AddDate(0, 1, -1)
	return start, end
}

func auditCount(t *testing.T, pool *pgxpool.Pool, action string) int {
	t.Helper()
	var n int
	err := pool.QueryRow(context.Background(),
		`SELECT COUNT(*) FROM audit_event WHERE action=$1`, action).Scan(&n)
	require.NoError(t, err)
	return n
}

// ---------- CloseSettlement ----------

func TestService_CloseSettlement_AggregatesPerVendor(t *testing.T) {
	pool, svc, cleanup := setup(t)
	defer cleanup()
	ctx := context.Background()

	start, end := aprilPeriod()
	admin := seedAdminUser(t, pool)
	user := seedEmployeeUser(t, pool)
	vendorA := seedVendor(t, pool)
	vendorB := seedVendor(t, pool)

	// Vendor A: picked_up (qty 3, 12000) + no_show (qty 2, 8000).
	seedOrder(t, pool, user, vendorA, start.AddDate(0, 0, 2), "picked_up", 12000, 3)
	seedOrder(t, pool, user, vendorA, start.AddDate(0, 0, 5), "no_show", 8000, 2)
	// Vendor A: cancelled / refunded must NOT count.
	seedOrder(t, pool, user, vendorA, start.AddDate(0, 0, 6), "cancelled", 5000, 1)
	seedOrder(t, pool, user, vendorA, start.AddDate(0, 0, 7), "refunded", 5000, 1)
	// Vendor B: picked_up (qty 1, 5000).
	seedOrder(t, pool, user, vendorB, start.AddDate(0, 0, 3), "picked_up", 5000, 1)

	settlements, err := svc.CloseSettlement(ctx, settlement.CloseSettlementInput{
		PeriodStart: start, PeriodEnd: end, ClosedBy: admin,
	})
	require.NoError(t, err)
	require.Len(t, settlements, 2)

	byVendor := map[string]*settlement.Settlement{}
	for _, s := range settlements {
		byVendor[s.VendorID] = s
		assert.Equal(t, settlement.StatusClosed, s.Status)
	}
	a := byVendor[vendorA]
	require.NotNil(t, a)
	assert.Equal(t, 2, a.OrderCount)
	assert.Equal(t, 5, a.PortionCount)
	assert.Equal(t, int64(20000), a.GrossMinor)

	b := byVendor[vendorB]
	require.NotNil(t, b)
	assert.Equal(t, 1, b.OrderCount)
	assert.Equal(t, int64(5000), b.GrossMinor)

	assert.Equal(t, 1, auditCount(t, pool, "settlement.close"))
}

func TestService_CloseSettlement_InvalidPeriod(t *testing.T) {
	pool, svc, cleanup := setup(t)
	defer cleanup()
	admin := seedAdminUser(t, pool)
	start, end := aprilPeriod()
	_, err := svc.CloseSettlement(context.Background(), settlement.CloseSettlementInput{
		PeriodStart: end, PeriodEnd: start, ClosedBy: admin,
	})
	assert.ErrorIs(t, err, settlement.ErrInvalidPeriod)
}

func TestService_CloseSettlement_NoOrders(t *testing.T) {
	pool, svc, cleanup := setup(t)
	defer cleanup()
	admin := seedAdminUser(t, pool)
	start, end := aprilPeriod()
	_, err := svc.CloseSettlement(context.Background(), settlement.CloseSettlementInput{
		PeriodStart: start, PeriodEnd: end, ClosedBy: admin,
	})
	assert.ErrorIs(t, err, settlement.ErrNoOrdersInPeriod)
}

func TestService_CloseSettlement_DuplicatePeriod(t *testing.T) {
	pool, svc, cleanup := setup(t)
	defer cleanup()
	ctx := context.Background()

	start, end := aprilPeriod()
	admin := seedAdminUser(t, pool)
	user := seedEmployeeUser(t, pool)
	vendor := seedVendor(t, pool)
	seedOrder(t, pool, user, vendor, start.AddDate(0, 0, 2), "picked_up", 12000, 1)

	_, err := svc.CloseSettlement(ctx, settlement.CloseSettlementInput{
		PeriodStart: start, PeriodEnd: end, ClosedBy: admin,
	})
	require.NoError(t, err)

	// Re-closing the same period (active settlement exists) → conflict.
	_, err = svc.CloseSettlement(ctx, settlement.CloseSettlementInput{
		PeriodStart: start, PeriodEnd: end, ClosedBy: admin,
	})
	assert.ErrorIs(t, err, settlement.ErrPeriodAlreadyClosed)
}

func TestService_CloseSettlement_ReclosableAfterVoid(t *testing.T) {
	pool, svc, cleanup := setup(t)
	defer cleanup()
	ctx := context.Background()

	start, end := aprilPeriod()
	admin := seedAdminUser(t, pool)
	user := seedEmployeeUser(t, pool)
	vendor := seedVendor(t, pool)
	seedOrder(t, pool, user, vendor, start.AddDate(0, 0, 2), "picked_up", 12000, 1)

	first, err := svc.CloseSettlement(ctx, settlement.CloseSettlementInput{
		PeriodStart: start, PeriodEnd: end, ClosedBy: admin,
	})
	require.NoError(t, err)
	require.Len(t, first, 1)

	require.NoError(t, svc.VoidSettlement(ctx, first[0].ID, admin))

	// After voiding, the period can be re-closed.
	second, err := svc.CloseSettlement(ctx, settlement.CloseSettlementInput{
		PeriodStart: start, PeriodEnd: end, ClosedBy: admin,
	})
	require.NoError(t, err)
	require.Len(t, second, 1)
	assert.NotEqual(t, first[0].ID, second[0].ID)
}

// ---------- VoidSettlement ----------

func TestService_VoidSettlement(t *testing.T) {
	pool, svc, cleanup := setup(t)
	defer cleanup()
	ctx := context.Background()

	start, end := aprilPeriod()
	admin := seedAdminUser(t, pool)
	user := seedEmployeeUser(t, pool)
	vendor := seedVendor(t, pool)
	seedOrder(t, pool, user, vendor, start.AddDate(0, 0, 2), "picked_up", 12000, 1)

	closed, err := svc.CloseSettlement(ctx, settlement.CloseSettlementInput{
		PeriodStart: start, PeriodEnd: end, ClosedBy: admin,
	})
	require.NoError(t, err)

	require.NoError(t, svc.VoidSettlement(ctx, closed[0].ID, admin))
	assert.Equal(t, 1, auditCount(t, pool, "settlement.void"))

	// Voiding twice is an invalid transition.
	err = svc.VoidSettlement(ctx, closed[0].ID, admin)
	assert.ErrorIs(t, err, settlement.ErrInvalidTransition)
}

func TestService_VoidSettlement_NotFound(t *testing.T) {
	pool, svc, cleanup := setup(t)
	defer cleanup()
	admin := seedAdminUser(t, pool)
	err := svc.VoidSettlement(context.Background(), "00000000-0000-0000-0000-000000000000", admin)
	assert.ErrorIs(t, err, settlement.ErrSettlementNotFound)
}

// ---------- Reconciliation ----------

func TestService_Reconciliation(t *testing.T) {
	pool, svc, cleanup := setup(t)
	defer cleanup()
	ctx := context.Background()

	start, end := aprilPeriod()
	user := seedEmployeeUser(t, pool)
	vendor := seedVendor(t, pool)

	seedOrder(t, pool, user, vendor, start.AddDate(0, 0, 1), "picked_up", 10000, 2)
	seedOrder(t, pool, user, vendor, start.AddDate(0, 0, 2), "picked_up", 10000, 3)
	seedOrder(t, pool, user, vendor, start.AddDate(0, 0, 3), "no_show", 6000, 1)
	seedOrder(t, pool, user, vendor, start.AddDate(0, 0, 4), "cancelled", 9999, 1)
	seedOrder(t, pool, user, vendor, start.AddDate(0, 0, 5), "refunded", 9999, 1)

	rec, err := svc.Reconciliation(ctx, vendor, start, end)
	require.NoError(t, err)
	assert.Equal(t, 3, rec.OrderCount)            // picked_up + no_show
	assert.Equal(t, 6, rec.PortionCount)          // 2 + 3 + 1
	assert.Equal(t, int64(26000), rec.GrossMinor) // 10000 + 10000 + 6000
	assert.Equal(t, 2, rec.Breakdown.PickedUp)
	assert.Equal(t, 1, rec.Breakdown.NoShow)
	assert.Equal(t, 1, rec.Breakdown.Cancelled)
	assert.Equal(t, 1, rec.Breakdown.Refunded)
}

func TestService_Reconciliation_EmptyVendor(t *testing.T) {
	pool, svc, cleanup := setup(t)
	defer cleanup()
	start, end := aprilPeriod()
	vendor := seedVendor(t, pool)

	rec, err := svc.Reconciliation(context.Background(), vendor, start, end)
	require.NoError(t, err)
	assert.Equal(t, 0, rec.OrderCount)
	assert.Equal(t, int64(0), rec.GrossMinor)
}

// ---------- vendor-scoped reads ----------

func TestService_GetVendorSettlement_OwnershipCheck(t *testing.T) {
	pool, svc, cleanup := setup(t)
	defer cleanup()
	ctx := context.Background()

	start, end := aprilPeriod()
	admin := seedAdminUser(t, pool)
	user := seedEmployeeUser(t, pool)
	vendorA := seedVendor(t, pool)
	vendorB := seedVendor(t, pool)
	seedOrder(t, pool, user, vendorA, start.AddDate(0, 0, 2), "picked_up", 12000, 2)

	closed, err := svc.CloseSettlement(ctx, settlement.CloseSettlementInput{
		PeriodStart: start, PeriodEnd: end, ClosedBy: admin,
	})
	require.NoError(t, err)
	require.Len(t, closed, 1)
	id := closed[0].ID

	// Owner can read its settlement with order-level detail.
	st, lines, err := svc.GetVendorSettlement(ctx, vendorA, id)
	require.NoError(t, err)
	assert.Equal(t, vendorA, st.VendorID)
	require.Len(t, lines, 1)
	assert.Equal(t, int64(12000), lines[0].TotalPriceMinor)
	assert.Equal(t, 2, lines[0].PortionCount)

	// Another vendor probing the same id gets NotFound (not Forbidden).
	_, _, err = svc.GetVendorSettlement(ctx, vendorB, id)
	assert.ErrorIs(t, err, settlement.ErrSettlementNotFound)
}

func TestService_ListVendorSettlements(t *testing.T) {
	pool, svc, cleanup := setup(t)
	defer cleanup()
	ctx := context.Background()

	admin := seedAdminUser(t, pool)
	user := seedEmployeeUser(t, pool)
	vendorA := seedVendor(t, pool)
	vendorB := seedVendor(t, pool)

	apr := time.Date(2026, time.April, 1, 0, 0, 0, 0, time.UTC)
	aprEnd := apr.AddDate(0, 1, -1)
	seedOrder(t, pool, user, vendorA, apr.AddDate(0, 0, 2), "picked_up", 10000, 1)
	seedOrder(t, pool, user, vendorB, apr.AddDate(0, 0, 2), "picked_up", 10000, 1)

	_, err := svc.CloseSettlement(ctx, settlement.CloseSettlementInput{
		PeriodStart: apr, PeriodEnd: aprEnd, ClosedBy: admin,
	})
	require.NoError(t, err)

	got, err := svc.ListVendorSettlements(ctx, vendorA)
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, vendorA, got[0].VendorID)
}

func TestService_ListSettlementsByPeriod(t *testing.T) {
	pool, svc, cleanup := setup(t)
	defer cleanup()
	ctx := context.Background()

	admin := seedAdminUser(t, pool)
	user := seedEmployeeUser(t, pool)
	vendorA := seedVendor(t, pool)
	vendorB := seedVendor(t, pool)

	apr := time.Date(2026, time.April, 1, 0, 0, 0, 0, time.UTC)
	aprEnd := apr.AddDate(0, 1, -1)
	seedOrder(t, pool, user, vendorA, apr.AddDate(0, 0, 2), "picked_up", 10000, 1)
	seedOrder(t, pool, user, vendorB, apr.AddDate(0, 0, 2), "picked_up", 10000, 1)

	_, err := svc.CloseSettlement(ctx, settlement.CloseSettlementInput{
		PeriodStart: apr, PeriodEnd: aprEnd, ClosedBy: admin,
	})
	require.NoError(t, err)

	got, err := svc.ListSettlementsByPeriod(ctx, apr, aprEnd)
	require.NoError(t, err)
	assert.Len(t, got, 2) // both vendors
}
