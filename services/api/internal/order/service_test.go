package order_test

import (
	"context"
	"path/filepath"
	"runtime"
	"sync"
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
		Vendors:     vpg.NewVendorRepo(pool),
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

	_, err := pool.Exec(ctx,
		`INSERT INTO plant (code, label) VALUES ($1, $1) ON CONFLICT DO NOTHING`, testPlant)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `
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

func TestService_Place_UsesVendorCutoffHour(t *testing.T) {
	pool, svc, cleanup := setup(t)
	defer cleanup()
	ctx := context.Background()

	vendorID, itemID, userID := seedScenario(t, pool, 5)
	// Vendor moves its cutoff earlier — 14:00 the day before supply.
	_, err := pool.Exec(ctx, `UPDATE vendor SET cutoff_hour=14 WHERE id=$1`, vendorID)
	require.NoError(t, err)

	o, err := svc.Place(ctx, order.PlaceOrderInput{
		UserID:     userID,
		Plant:      testPlant,
		SupplyDate: testSupplyDate,
		Items:      []order.PlaceItem{{MenuItemID: itemID, Qty: 1}},
	})
	require.NoError(t, err)
	// Service.Location is nil in tests → UTC. supply 2026-05-14 → cutoff
	// 2026-05-13 14:00 UTC.
	want := time.Date(2026, 5, 13, 14, 0, 0, 0, time.UTC)
	assert.True(t, o.CutoffAt.Equal(want), "cutoff %v, want %v", o.CutoffAt, want)
}

func TestService_Place_OutsidePreorderWindow(t *testing.T) {
	pool, svc, cleanup := setup(t)
	defer cleanup()
	ctx := context.Background()

	vendorID, itemID, userID := seedScenario(t, pool, 5)
	// Vendor only opens preorders 3 days ahead.
	_, err := pool.Exec(ctx, `UPDATE vendor SET preorder_window_days=3 WHERE id=$1`, vendorID)
	require.NoError(t, err)

	// 2026-05-20 is beyond the 3-day window; its cutoff is still future, so only the window rejects it.
	farDate := time.Date(2026, 5, 20, 0, 0, 0, 0, time.UTC)
	farCutoff := time.Date(2026, 5, 19, 17, 0, 0, 0, time.UTC)
	_, err = pool.Exec(ctx, `
INSERT INTO meal_supply (menu_item_id, supply_date, capacity, remain, pickup_window, eta_label, cutoff_at)
VALUES ($1, $2, 5, 5, '', '', $3)`, itemID, farDate, farCutoff)
	require.NoError(t, err)

	_, err = svc.Place(ctx, order.PlaceOrderInput{
		UserID:     userID,
		Plant:      testPlant,
		SupplyDate: farDate,
		Items:      []order.PlaceItem{{MenuItemID: itemID, Qty: 1}},
	})
	assert.ErrorIs(t, err, order.ErrOutsidePreorderWindow)
}

func TestService_ListByUser_HydratesItems(t *testing.T) {
	pool, svc, cleanup := setup(t)
	defer cleanup()
	ctx := context.Background()

	_, itemID, userID := seedScenario(t, pool, 5)
	placed, err := svc.Place(ctx, order.PlaceOrderInput{
		UserID:     userID,
		Plant:      testPlant,
		SupplyDate: testSupplyDate,
		Items:      []order.PlaceItem{{MenuItemID: itemID, Qty: 2}},
	})
	require.NoError(t, err)

	orders, err := svc.ListByUser(ctx, userID)
	require.NoError(t, err)
	require.Len(t, orders, 1)
	assert.Equal(t, placed.ID, orders[0].ID)
	require.Len(t, orders[0].Items, 1, "ListByUser must hydrate order items")
	assert.Equal(t, itemID, orders[0].Items[0].MenuItemID)
	assert.Equal(t, 2, orders[0].Items[0].Qty)
}

func TestService_Place_SoldOut(t *testing.T) {
	pool, svc, cleanup := setup(t)
	defer cleanup()
	ctx := context.Background()

	_, itemID, userID := seedScenario(t, pool, 5)
	// Vendor flags the supply sold out for the day even though capacity remains.
	_, err := pool.Exec(ctx, `UPDATE meal_supply SET sold_out=true WHERE menu_item_id=$1`, itemID)
	require.NoError(t, err)

	_, err = svc.Place(ctx, order.PlaceOrderInput{
		UserID:     userID,
		Plant:      testPlant,
		SupplyDate: testSupplyDate,
		Items:      []order.PlaceItem{{MenuItemID: itemID, Qty: 1}},
	})
	assert.ErrorIs(t, err, qmod.ErrOutOfStock)

	// Sold-out supply must not be decremented and no order should persist.
	var remain, orderCount int
	require.NoError(t, pool.QueryRow(ctx, `SELECT remain FROM meal_supply WHERE menu_item_id=$1`, itemID).Scan(&remain))
	require.NoError(t, pool.QueryRow(ctx, `SELECT count(*) FROM "order"`).Scan(&orderCount))
	assert.Equal(t, 5, remain)
	assert.Equal(t, 0, orderCount)
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

// seedExtraItem adds a second menu item (with its own supply row) for the
// given vendor so modify tests can add/remove items. capacity == initial remain.
func seedExtraItem(t *testing.T, pool *pgxpool.Pool, vendorID string, priceMinor, capacity int) (itemID string) {
	t.Helper()
	ctx := context.Background()
	require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO menu_item (vendor_id, name, description, price_minor, status, tags, badges)
VALUES ($1, 'Item B', '', $2, 'active', ARRAY[]::text[], ARRAY[]::text[])
RETURNING id`, vendorID, priceMinor).Scan(&itemID))
	_, err := pool.Exec(ctx, `
INSERT INTO meal_supply (menu_item_id, supply_date, capacity, remain, pickup_window, eta_label, cutoff_at)
VALUES ($1, $2, $3, $3, '', '', $4)`, itemID, testSupplyDate, capacity, testCutoffAt)
	require.NoError(t, err)
	return itemID
}

func remainOf(t *testing.T, pool *pgxpool.Pool, itemID string) int {
	t.Helper()
	var remain int
	require.NoError(t, pool.QueryRow(context.Background(),
		`SELECT remain FROM meal_supply WHERE menu_item_id=$1`, itemID).Scan(&remain))
	return remain
}

func TestService_Modify_Happy(t *testing.T) {
	pool, svc, cleanup := setup(t)
	defer cleanup()

	vendorID, itemA, userID := seedScenario(t, pool, 5) // A: price 110, cap 5
	itemB := seedExtraItem(t, pool, vendorID, 200, 10)  // B: price 200, cap 10

	o, err := svc.Place(context.Background(), order.PlaceOrderInput{
		UserID:     userID,
		Plant:      testPlant,
		SupplyDate: testSupplyDate,
		Items:      []order.PlaceItem{{MenuItemID: itemA, Qty: 2}},
	})
	require.NoError(t, err)
	assert.Equal(t, 3, remainOf(t, pool, itemA)) // 5 - 2

	// Modify: A 2→1 (restore 1), add B 3 (decrement 3).
	got, err := svc.Modify(context.Background(), order.ModifyOrderInput{
		OrderID: o.ID,
		UserID:  userID,
		Items:   []order.PlaceItem{{MenuItemID: itemA, Qty: 1}, {MenuItemID: itemB, Qty: 3}},
	})
	require.NoError(t, err)
	assert.Equal(t, order.StatusPlaced, got.Status)
	assert.Equal(t, int64(110*1+200*3), got.TotalPriceMinor)
	require.Len(t, got.Items, 2)
	assert.Equal(t, 4, remainOf(t, pool, itemA)) // 3 + 1 restored
	assert.Equal(t, 7, remainOf(t, pool, itemB)) // 10 - 3

	// Modify again: drop A entirely (restore 1), B 3→2 (restore 1).
	got, err = svc.Modify(context.Background(), order.ModifyOrderInput{
		OrderID: o.ID,
		UserID:  userID,
		Items:   []order.PlaceItem{{MenuItemID: itemB, Qty: 2}},
	})
	require.NoError(t, err)
	require.Len(t, got.Items, 1)
	assert.Equal(t, 5, remainOf(t, pool, itemA)) // fully restored
	assert.Equal(t, 8, remainOf(t, pool, itemB)) // 7 + 1

	// Two modify calls each leave an audit + outbox row (on top of place).
	var auditCount, outboxCount int
	require.NoError(t, pool.QueryRow(context.Background(),
		`SELECT count(*) FROM audit_event WHERE target_id=$1 AND action='order.modify'`, o.ID).Scan(&auditCount))
	require.NoError(t, pool.QueryRow(context.Background(),
		`SELECT count(*) FROM outbox_event WHERE aggregate_id::text=$1 AND subject='order.modified.v1'`, o.ID).Scan(&outboxCount))
	assert.Equal(t, 2, auditCount)
	assert.Equal(t, 2, outboxCount)
}

func TestService_Modify_OutOfStock(t *testing.T) {
	pool, svc, cleanup := setup(t)
	defer cleanup()

	_, itemA, userID := seedScenario(t, pool, 5)

	o, err := svc.Place(context.Background(), order.PlaceOrderInput{
		UserID:     userID,
		Plant:      testPlant,
		SupplyDate: testSupplyDate,
		Items:      []order.PlaceItem{{MenuItemID: itemA, Qty: 2}},
	})
	require.NoError(t, err)

	// Ask for 6 > capacity 5 → out of stock, full rollback.
	_, err = svc.Modify(context.Background(), order.ModifyOrderInput{
		OrderID: o.ID,
		UserID:  userID,
		Items:   []order.PlaceItem{{MenuItemID: itemA, Qty: 6}},
	})
	assert.ErrorIs(t, err, qmod.ErrOutOfStock)

	// Order items and quota unchanged.
	after, err := svc.Get(context.Background(), o.ID, userID)
	require.NoError(t, err)
	require.Len(t, after.Items, 1)
	assert.Equal(t, 2, after.Items[0].Qty)
	assert.Equal(t, int64(220), after.TotalPriceMinor)
	assert.Equal(t, 3, remainOf(t, pool, itemA)) // still 5 - 2
}

func TestService_Modify_AfterCutoff(t *testing.T) {
	pool, _, cleanup := setup(t)
	defer cleanup()

	_, itemA, userID := seedScenario(t, pool, 5)

	// Clock before cutoff for Place.
	svcEarly := &order.Service{
		Pool: pool, Orders: opg.NewOrderRepo(pool), OrdersTx: opg.NewOrderRepo(pool),
		StateEvents: opg.NewStateEventRepo(pool), StateTx: opg.NewStateEventRepo(pool),
		Audit: opg.NewAuditRepo(pool), AuditTx: opg.NewAuditRepo(pool),
		Outbox: opg.NewOutboxRepo(pool), OutboxTx: opg.NewOutboxRepo(pool),
		QuotaTx: qpg.NewSupplyRepo(pool), Items: mpg.NewItemRepo(pool),
		Plants: vpg.NewPlantMappingRepo(pool), Vendors: vpg.NewVendorRepo(pool),
		Clock: fixedClock{T: testClockTime},
	}
	o, err := svcEarly.Place(context.Background(), order.PlaceOrderInput{
		UserID: userID, Plant: testPlant, SupplyDate: testSupplyDate,
		Items: []order.PlaceItem{{MenuItemID: itemA, Qty: 1}},
	})
	require.NoError(t, err)

	// Clock past cutoff for Modify.
	svcLate := *svcEarly
	svcLate.Clock = fixedClock{T: testCutoffAt.Add(time.Minute)}
	_, err = svcLate.Modify(context.Background(), order.ModifyOrderInput{
		OrderID: o.ID, UserID: userID,
		Items: []order.PlaceItem{{MenuItemID: itemA, Qty: 2}},
	})
	assert.ErrorIs(t, err, order.ErrCutoffPassed)
}

func TestService_Modify_NotOwner(t *testing.T) {
	pool, svc, cleanup := setup(t)
	defer cleanup()

	_, itemA, userID := seedScenario(t, pool, 5)

	o, err := svc.Place(context.Background(), order.PlaceOrderInput{
		UserID: userID, Plant: testPlant, SupplyDate: testSupplyDate,
		Items: []order.PlaceItem{{MenuItemID: itemA, Qty: 1}},
	})
	require.NoError(t, err)

	var otherID string
	require.NoError(t, pool.QueryRow(context.Background(), `
INSERT INTO "user" (primary_email, display_name, role, status, plant)
VALUES ('other2@test.com', 'Other', 'employee', 'active', $1)
RETURNING id`, testPlant).Scan(&otherID))

	_, err = svc.Modify(context.Background(), order.ModifyOrderInput{
		OrderID: o.ID, UserID: otherID,
		Items: []order.PlaceItem{{MenuItemID: itemA, Qty: 2}},
	})
	assert.ErrorIs(t, err, order.ErrForbidden)
}

func TestService_Modify_NotPlaced(t *testing.T) {
	pool, svc, cleanup := setup(t)
	defer cleanup()

	_, itemA, userID := seedScenario(t, pool, 5)

	o, err := svc.Place(context.Background(), order.PlaceOrderInput{
		UserID: userID, Plant: testPlant, SupplyDate: testSupplyDate,
		Items: []order.PlaceItem{{MenuItemID: itemA, Qty: 1}},
	})
	require.NoError(t, err)
	require.NoError(t, svc.Cancel(context.Background(), o.ID, userID))

	_, err = svc.Modify(context.Background(), order.ModifyOrderInput{
		OrderID: o.ID, UserID: userID,
		Items: []order.PlaceItem{{MenuItemID: itemA, Qty: 2}},
	})
	assert.ErrorIs(t, err, order.ErrInvalidTransition)
}

func TestService_Modify_CrossVendor(t *testing.T) {
	pool, svc, cleanup := setup(t)
	defer cleanup()

	_, itemA, userID := seedScenario(t, pool, 5)

	o, err := svc.Place(context.Background(), order.PlaceOrderInput{
		UserID: userID, Plant: testPlant, SupplyDate: testSupplyDate,
		Items: []order.PlaceItem{{MenuItemID: itemA, Qty: 1}},
	})
	require.NoError(t, err)

	// A second vendor with its own item — modifying to add it must be rejected.
	var otherVendor string
	require.NoError(t, pool.QueryRow(context.Background(), `
INSERT INTO vendor (display_name, legal_name, contact_email, status)
VALUES ('Other', 'Other Ltd', 'other3@vendor.com', 'approved')
RETURNING id`).Scan(&otherVendor))
	foreignItem := seedExtraItem(t, pool, otherVendor, 150, 10)

	_, err = svc.Modify(context.Background(), order.ModifyOrderInput{
		OrderID: o.ID, UserID: userID,
		Items: []order.PlaceItem{{MenuItemID: itemA, Qty: 1}, {MenuItemID: foreignItem, Qty: 1}},
	})
	assert.ErrorIs(t, err, order.ErrMultiVendor)
}

func TestService_Notes_PlaceAndModify(t *testing.T) {
	pool, svc, cleanup := setup(t)
	defer cleanup()

	_, itemA, userID := seedScenario(t, pool, 5)

	// Notes given at placement are persisted and read back.
	o, err := svc.Place(context.Background(), order.PlaceOrderInput{
		UserID: userID, Plant: testPlant, SupplyDate: testSupplyDate,
		Items: []order.PlaceItem{{MenuItemID: itemA, Qty: 1}},
		Notes: "不要辣，謝謝",
	})
	require.NoError(t, err)
	assert.Equal(t, "不要辣，謝謝", o.Notes)

	got, err := svc.Get(context.Background(), o.ID, userID)
	require.NoError(t, err)
	assert.Equal(t, "不要辣，謝謝", got.Notes)

	// Modify overwrites the note.
	mod, err := svc.Modify(context.Background(), order.ModifyOrderInput{
		OrderID: o.ID, UserID: userID,
		Items: []order.PlaceItem{{MenuItemID: itemA, Qty: 2}},
		Notes: "改成大辣",
	})
	require.NoError(t, err)
	assert.Equal(t, "改成大辣", mod.Notes)
}

// forceStatus rewrites the order's status directly in the DB so tests can land
// an order in a state that normally requires a scheduler tick (cutoff, ready)
// without having to mock time.
func forceStatus(t *testing.T, pool *pgxpool.Pool, orderID string, status order.Status) {
	t.Helper()
	ctx := context.Background()
	_, err := pool.Exec(ctx, `
UPDATE "order"
   SET status=$2::order_status,
       ready_at = CASE WHEN $2::text = 'ready' THEN now() ELSE ready_at END,
       updated_at = now()
 WHERE id=$1`, orderID, string(status))
	require.NoError(t, err)
}

func TestService_MarkReady_Happy(t *testing.T) {
	pool, svc, cleanup := setup(t)
	defer cleanup()

	vendorID, itemID, userID := seedScenario(t, pool, 5)

	o, err := svc.Place(context.Background(), order.PlaceOrderInput{
		UserID:     userID,
		Plant:      testPlant,
		SupplyDate: testSupplyDate,
		Items:      []order.PlaceItem{{MenuItemID: itemID, Qty: 1}},
	})
	require.NoError(t, err)

	actorID := userID // any UUID works as the vendor operator for the audit row
	require.NoError(t, svc.MarkReady(context.Background(), vendorID, []string{o.ID}, actorID))

	after, err := svc.Get(context.Background(), o.ID, userID)
	require.NoError(t, err)
	assert.Equal(t, order.StatusReady, after.Status)
	require.NotNil(t, after.ReadyAt)

	// state event written (placed → ready) on top of the initial placement event.
	var seCount int
	require.NoError(t, pool.QueryRow(context.Background(),
		`SELECT count(*) FROM order_state_event WHERE order_id=$1 AND to_state='ready'`, o.ID).Scan(&seCount))
	assert.Equal(t, 1, seCount)
}

func TestService_MarkReady_WrongVendor(t *testing.T) {
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

	// A different vendor must not be able to mark the order ready.
	var otherVendor string
	require.NoError(t, pool.QueryRow(context.Background(), `
INSERT INTO vendor (display_name, legal_name, contact_email, status)
VALUES ('Other', 'Other Ltd', 'other@vendor.com', 'approved')
RETURNING id`).Scan(&otherVendor))

	err = svc.MarkReady(context.Background(), otherVendor, []string{o.ID}, userID)
	assert.ErrorIs(t, err, order.ErrForbidden)
}

func TestService_MarkReady_WrongStatus(t *testing.T) {
	pool, svc, cleanup := setup(t)
	defer cleanup()

	vendorID, itemID, userID := seedScenario(t, pool, 5)

	o, err := svc.Place(context.Background(), order.PlaceOrderInput{
		UserID:     userID,
		Plant:      testPlant,
		SupplyDate: testSupplyDate,
		Items:      []order.PlaceItem{{MenuItemID: itemID, Qty: 1}},
	})
	require.NoError(t, err)

	// User cancels first, then vendor tries to mark ready → cancelled cannot go to ready.
	require.NoError(t, svc.Cancel(context.Background(), o.ID, userID))

	err = svc.MarkReady(context.Background(), vendorID, []string{o.ID}, userID)
	assert.ErrorIs(t, err, order.ErrInvalidTransition)
}

// pickupReadyOrder places an order and forces it to READY, returning its ID.
func pickupReadyOrder(t *testing.T, pool *pgxpool.Pool, svc *order.Service, itemID, userID string) string {
	t.Helper()
	o, err := svc.Place(context.Background(), order.PlaceOrderInput{
		UserID:     userID,
		Plant:      testPlant,
		SupplyDate: testSupplyDate,
		Items:      []order.PlaceItem{{MenuItemID: itemID, Qty: 1}},
	})
	require.NoError(t, err)
	forceStatus(t, pool, o.ID, order.StatusReady)
	return o.ID
}

func TestService_Pickup_Happy(t *testing.T) {
	pool, svc, cleanup := setup(t)
	defer cleanup()

	_, itemID, userID := seedScenario(t, pool, 5)
	orderID := pickupReadyOrder(t, pool, svc, itemID, userID)

	require.NoError(t, svc.Pickup(context.Background(), orderID, userID))

	after, err := svc.Get(context.Background(), orderID, userID)
	require.NoError(t, err)
	assert.Equal(t, order.StatusPickedUp, after.Status)
	require.NotNil(t, after.PickedUpAt)
}

func TestService_Pickup_NotOwner(t *testing.T) {
	pool, svc, cleanup := setup(t)
	defer cleanup()

	_, itemID, userID := seedScenario(t, pool, 5)
	orderID := pickupReadyOrder(t, pool, svc, itemID, userID)

	var otherID string
	require.NoError(t, pool.QueryRow(context.Background(), `
INSERT INTO "user" (primary_email, display_name, role, status, plant)
VALUES ('other-pickup@test.com', 'Other', 'employee', 'active', $1)
RETURNING id`, testPlant).Scan(&otherID))

	err := svc.Pickup(context.Background(), orderID, otherID)
	assert.ErrorIs(t, err, order.ErrForbidden)

	// Order must remain READY — a non-owner attempt does not transition it.
	after, err := svc.Get(context.Background(), orderID, userID)
	require.NoError(t, err)
	assert.Equal(t, order.StatusReady, after.Status)
}

func TestService_Pickup_WrongStatus(t *testing.T) {
	pool, svc, cleanup := setup(t)
	defer cleanup()

	_, itemID, userID := seedScenario(t, pool, 5)

	// Order is still PLACED (not yet marked ready) → cannot be picked up.
	o, err := svc.Place(context.Background(), order.PlaceOrderInput{
		UserID:     userID,
		Plant:      testPlant,
		SupplyDate: testSupplyDate,
		Items:      []order.PlaceItem{{MenuItemID: itemID, Qty: 1}},
	})
	require.NoError(t, err)

	err = svc.Pickup(context.Background(), o.ID, userID)
	assert.ErrorIs(t, err, order.ErrInvalidTransition)
}

func TestService_Pickup_Double(t *testing.T) {
	pool, svc, cleanup := setup(t)
	defer cleanup()

	_, itemID, userID := seedScenario(t, pool, 5)
	orderID := pickupReadyOrder(t, pool, svc, itemID, userID)

	require.NoError(t, svc.Pickup(context.Background(), orderID, userID))

	// Second pickup of an already picked-up order must be rejected.
	err := svc.Pickup(context.Background(), orderID, userID)
	assert.ErrorIs(t, err, order.ErrInvalidTransition)
}

func TestService_Pickup_NotFound(t *testing.T) {
	_, svc, cleanup := setup(t)
	defer cleanup()

	err := svc.Pickup(context.Background(), "11111111-1111-1111-1111-111111111111", "22222222-2222-2222-2222-222222222222")
	assert.ErrorIs(t, err, order.ErrOrderNotFound)
}

// TestService_Pickup_AtomicNoDouble_1000 fires 1000 goroutines against one
// READY order owned by the caller. The conditional UPDATE inside
// MarkPickedUpTx must guarantee exactly one winner (one-time idempotency).
func TestService_Pickup_AtomicNoDouble_1000(t *testing.T) {
	pool, svc, cleanup := setup(t)
	defer cleanup()

	_, itemID, userID := seedScenario(t, pool, 5)
	orderID := pickupReadyOrder(t, pool, svc, itemID, userID)

	const N = 1000
	var (
		succeeded    atomic.Int64
		invalidTrans atomic.Int64
		otherErr     atomic.Int64
		start        = make(chan struct{})
		wg           sync.WaitGroup
	)
	wg.Add(N)
	for i := 0; i < N; i++ {
		go func() {
			defer wg.Done()
			<-start
			err := svc.Pickup(context.Background(), orderID, userID)
			switch {
			case err == nil:
				succeeded.Add(1)
			case err == order.ErrInvalidTransition:
				invalidTrans.Add(1)
			default:
				otherErr.Add(1)
				t.Logf("unexpected error: %v", err)
			}
		}()
	}
	close(start)
	wg.Wait()

	assert.Equal(t, int64(1), succeeded.Load(), "exactly one goroutine must succeed")
	assert.Equal(t, int64(N-1), invalidTrans.Load(), "all losers must see ErrInvalidTransition")
	assert.Equal(t, int64(0), otherErr.Load(), "no other errors allowed")

	after, err := svc.Get(context.Background(), orderID, userID)
	require.NoError(t, err)
	assert.Equal(t, order.StatusPickedUp, after.Status)
}
