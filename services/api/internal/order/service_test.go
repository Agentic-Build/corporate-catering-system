package order_test

import (
	"context"
	"path/filepath"
	"runtime"
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

	mpg "github.com/takalawang/corporate-catering-system/services/api/internal/menu/postgres"
	"github.com/takalawang/corporate-catering-system/services/api/internal/order"
	opg "github.com/takalawang/corporate-catering-system/services/api/internal/order/postgres"
	qmod "github.com/takalawang/corporate-catering-system/services/api/internal/quota"
	qpg "github.com/takalawang/corporate-catering-system/services/api/internal/quota/postgres"
	vpg "github.com/takalawang/corporate-catering-system/services/api/internal/vendors/postgres"
)

func migrationsDir() string {
	_, thisFile, _, _ := runtime.Caller(0)
	// services/api/internal/order/service_test.go → ../../../../migrations
	return filepath.Join(filepath.Dir(thisFile), "..", "..", "..", "..", "migrations")
}

type fixedClock struct{ T time.Time }

func (c fixedClock) Now() time.Time { return c.T }

const testPlant = "F12B-3F"

var (
	testSupplyDate = time.Date(2026, 5, 14, 0, 0, 0, 0, time.UTC)
	testClockTime  = time.Date(2026, 5, 13, 10, 0, 0, 0, time.UTC)
	testCutoffAt   = time.Date(2026, 5, 13, 17, 0, 0, 0, time.UTC)
)

func setup(t *testing.T) (*pgxpool.Pool, *order.Service, func()) {
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
	cfg.MaxConns = 30
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	require.NoError(t, err)

	orderRepo := opg.NewOrderRepo(pool)
	stateRepo := opg.NewStateEventRepo(pool)
	auditRepo := opg.NewAuditRepo(pool)
	outboxRepo := opg.NewOutboxRepo(pool)
	supplyRepo := qpg.NewSupplyRepo(pool)
	itemRepo := mpg.NewItemRepo(pool)
	plantRepo := vpg.NewPlantMappingRepo(pool)

	svc := &order.Service{
		Pool:        pool,
		Orders:      orderRepo,
		OrdersTx:    orderRepo,
		StateEvents: stateRepo,
		StateTx:     stateRepo,
		Audit:       auditRepo,
		AuditTx:     auditRepo,
		Outbox:      outboxRepo,
		OutboxTx:    outboxRepo,
		QuotaTx:     supplyRepo,
		Items:       itemRepo,
		Plants:      plantRepo,
		Clock:       fixedClock{T: testClockTime},
	}
	cleanup := func() {
		pool.Close()
		_ = container.Terminate(ctx)
	}
	return pool, svc, cleanup
}

// seedScenario provisions vendor + plant mapping + menu item + supply + user
// for the standard happy-path tests. capacity == initial remain.
func seedScenario(t *testing.T, pool *pgxpool.Pool, capacity int) (vendorID, itemID, userID string) {
	t.Helper()
	ctx := context.Background()

	require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO vendor (display_name, legal_name, contact_email, status)
VALUES ('Vendor', 'Vendor Ltd', 'vendor@test.com', 'approved')
RETURNING id`).Scan(&vendorID))

	require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO menu_item (vendor_id, name, description, price_minor, status, tags, badges)
VALUES ($1, 'Item', '', 110, 'active', ARRAY[]::text[], ARRAY[]::text[])
RETURNING id`, vendorID).Scan(&itemID))

	_, err := pool.Exec(ctx, `
INSERT INTO vendor_plant_mapping (vendor_id, plant, active)
VALUES ($1, $2, true)`, vendorID, testPlant)
	require.NoError(t, err)

	_, err = pool.Exec(ctx, `
INSERT INTO meal_supply (menu_item_id, supply_date, capacity, remain, pickup_window, eta_label, cutoff_at)
VALUES ($1, $2, $3, $3, '', '', $4)`,
		itemID, testSupplyDate, capacity, testCutoffAt)
	require.NoError(t, err)

	require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO "user" (primary_email, display_name, role, status, plant)
VALUES ('employee@test.com', 'Employee', 'employee', 'active', $1)
RETURNING id`, testPlant).Scan(&userID))

	return vendorID, itemID, userID
}

func TestService_Place_Happy(t *testing.T) {
	pool, svc, cleanup := setup(t)
	defer cleanup()

	_, itemID, userID := seedScenario(t, pool, 5)

	o, err := svc.Place(context.Background(), order.PlaceOrderInput{
		UserID:     userID,
		Plant:      testPlant,
		SupplyDate: testSupplyDate,
		Items:      []order.PlaceItem{{MenuItemID: itemID, Qty: 2}},
	})
	require.NoError(t, err)
	require.NotNil(t, o)
	assert.Equal(t, order.StatusPlaced, o.Status)
	assert.Equal(t, int64(220), o.TotalPriceMinor) // 110 * 2
	require.Len(t, o.Items, 1)
	assert.Equal(t, int64(110), o.Items[0].UnitPriceMinor)

	// Quota decremented.
	var remain int
	require.NoError(t, pool.QueryRow(context.Background(),
		`SELECT remain FROM meal_supply WHERE menu_item_id=$1`, itemID).Scan(&remain))
	assert.Equal(t, 3, remain)

	// State event, audit, and outbox each have one row for this order.
	var seCount, auditCount, outboxCount int
	require.NoError(t, pool.QueryRow(context.Background(),
		`SELECT count(*) FROM order_state_event WHERE order_id=$1`, o.ID).Scan(&seCount))
	require.NoError(t, pool.QueryRow(context.Background(),
		`SELECT count(*) FROM audit_event WHERE target_id=$1`, o.ID).Scan(&auditCount))
	require.NoError(t, pool.QueryRow(context.Background(),
		`SELECT count(*) FROM outbox_event WHERE aggregate_id::text=$1`, o.ID).Scan(&outboxCount))
	assert.Equal(t, 1, seCount)
	assert.Equal(t, 1, auditCount)
	assert.Equal(t, 1, outboxCount)
}

func TestService_Place_OutOfStock(t *testing.T) {
	pool, svc, cleanup := setup(t)
	defer cleanup()

	_, itemID, userID := seedScenario(t, pool, 1) // capacity 1 only

	_, err := svc.Place(context.Background(), order.PlaceOrderInput{
		UserID:     userID,
		Plant:      testPlant,
		SupplyDate: testSupplyDate,
		Items:      []order.PlaceItem{{MenuItemID: itemID, Qty: 2}}, // ask 2 > 1
	})
	assert.ErrorIs(t, err, qmod.ErrOutOfStock)

	// Rollback: no order, no state_event, no audit, no outbox; quota still 1.
	var orderCount, stateCount, auditCount, outboxCount, remain int
	require.NoError(t, pool.QueryRow(context.Background(), `SELECT count(*) FROM "order"`).Scan(&orderCount))
	require.NoError(t, pool.QueryRow(context.Background(), `SELECT count(*) FROM order_state_event`).Scan(&stateCount))
	require.NoError(t, pool.QueryRow(context.Background(), `SELECT count(*) FROM audit_event`).Scan(&auditCount))
	require.NoError(t, pool.QueryRow(context.Background(), `SELECT count(*) FROM outbox_event`).Scan(&outboxCount))
	require.NoError(t, pool.QueryRow(context.Background(),
		`SELECT remain FROM meal_supply WHERE menu_item_id=$1`, itemID).Scan(&remain))
	assert.Equal(t, 0, orderCount, "order should not exist after rollback")
	assert.Equal(t, 0, stateCount)
	assert.Equal(t, 0, auditCount)
	assert.Equal(t, 0, outboxCount)
	assert.Equal(t, 1, remain, "quota should be restored after rollback")
}

func TestService_Place_EmptyOrder(t *testing.T) {
	_, svc, cleanup := setup(t)
	defer cleanup()

	_, err := svc.Place(context.Background(), order.PlaceOrderInput{
		UserID:     "11111111-1111-1111-1111-111111111111",
		Plant:      testPlant,
		SupplyDate: testSupplyDate,
		Items:      nil,
	})
	assert.ErrorIs(t, err, order.ErrEmptyOrder)
}

func TestService_Place_PlantNotServed(t *testing.T) {
	pool, svc, cleanup := setup(t)
	defer cleanup()

	_, itemID, userID := seedScenario(t, pool, 5)

	// Vendor only mapped to testPlant; F15-2F must be rejected.
	_, err := svc.Place(context.Background(), order.PlaceOrderInput{
		UserID:     userID,
		Plant:      "F15-2F",
		SupplyDate: testSupplyDate,
		Items:      []order.PlaceItem{{MenuItemID: itemID, Qty: 1}},
	})
	assert.ErrorIs(t, err, order.ErrVendorPlantMismatch)
}

func TestService_Cancel_Happy(t *testing.T) {
	pool, svc, cleanup := setup(t)
	defer cleanup()

	_, itemID, userID := seedScenario(t, pool, 5)

	o, err := svc.Place(context.Background(), order.PlaceOrderInput{
		UserID:     userID,
		Plant:      testPlant,
		SupplyDate: testSupplyDate,
		Items:      []order.PlaceItem{{MenuItemID: itemID, Qty: 2}},
	})
	require.NoError(t, err)

	require.NoError(t, svc.Cancel(context.Background(), o.ID, userID))

	after, err := svc.Get(context.Background(), o.ID, userID)
	require.NoError(t, err)
	assert.Equal(t, order.StatusCancelled, after.Status)

	// Quota restored.
	var remain int
	require.NoError(t, pool.QueryRow(context.Background(),
		`SELECT remain FROM meal_supply WHERE menu_item_id=$1`, itemID).Scan(&remain))
	assert.Equal(t, 5, remain)

	// 2 state events (placed + cancelled).
	var seCount int
	require.NoError(t, pool.QueryRow(context.Background(),
		`SELECT count(*) FROM order_state_event WHERE order_id=$1`, o.ID).Scan(&seCount))
	assert.Equal(t, 2, seCount)
}

func TestService_Cancel_NotOwner(t *testing.T) {
	pool, svc, cleanup := setup(t)
	defer cleanup()

	_, itemID, userID := seedScenario(t, pool, 5)

	o, err := svc.Place(context.Background(), order.PlaceOrderInput{
		UserID:     userID,
		Plant:      testPlant,
		SupplyDate: testSupplyDate,
		Items:      []order.PlaceItem{{MenuItemID: itemID, Qty: 1}},
	})
	require.NoError(t, err)

	var otherID string
	require.NoError(t, pool.QueryRow(context.Background(), `
INSERT INTO "user" (primary_email, display_name, role, status, plant)
VALUES ('other@test.com', 'Other', 'employee', 'active', $1)
RETURNING id`, testPlant).Scan(&otherID))

	err = svc.Cancel(context.Background(), o.ID, otherID)
	assert.ErrorIs(t, err, order.ErrForbidden)
}
