package order_test

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	menu "github.com/takalawang/corporate-catering-system/services/api/internal/menu"
	mpg "github.com/takalawang/corporate-catering-system/services/api/internal/menu/postgres"
	"github.com/takalawang/corporate-catering-system/services/api/internal/order"
	opg "github.com/takalawang/corporate-catering-system/services/api/internal/order/postgres"
	qpg "github.com/takalawang/corporate-catering-system/services/api/internal/quota/postgres"
	vpg "github.com/takalawang/corporate-catering-system/services/api/internal/vendors/postgres"
)

const missingUUID = "99999999-9999-9999-9999-999999999999"

// newSvcWithClock builds a Service over the live pool with a caller-chosen
// clock, used by the no-show sweep tests that need "now" decoupled from the
// shared testClockTime.
func newSvcWithClock(t *testing.T, pool *pgxpool.Pool, clock order.Clock) *order.Service {
	t.Helper()
	orderRepo := opg.NewOrderRepo(pool)
	stateRepo := opg.NewStateEventRepo(pool)
	auditRepo := opg.NewAuditRepo(pool)
	outboxRepo := opg.NewOutboxRepo(pool)
	return &order.Service{
		Pool:        pool,
		Orders:      orderRepo,
		OrdersTx:    orderRepo,
		StateEvents: stateRepo,
		StateTx:     stateRepo,
		Audit:       auditRepo,
		AuditTx:     auditRepo,
		Outbox:      outboxRepo,
		OutboxTx:    outboxRepo,
		QuotaTx:     qpg.NewSupplyRepo(pool),
		Items:       mpg.NewItemRepo(pool),
		Plants:      vpg.NewPlantMappingRepo(pool),
		Vendors:     vpg.NewVendorRepo(pool),
		Clock:       clock,
	}
}

func TestService_Get_NotFound(t *testing.T) {
	_, svc, cleanup := setup(t)
	defer cleanup()

	_, err := svc.Get(context.Background(), missingUUID, missingUUID)
	assert.ErrorIs(t, err, order.ErrOrderNotFound)
}

func TestService_Get_NotOwner(t *testing.T) {
	pool, svc, cleanup := setup(t)
	defer cleanup()

	_, itemID, userID := seedScenario(t, pool, 5)
	o, err := svc.Place(context.Background(), order.PlaceOrderInput{
		UserID: userID, Plant: testPlant, SupplyDate: testSupplyDate,
		Items: []order.PlaceItem{{MenuItemID: itemID, Qty: 1}},
	})
	require.NoError(t, err)

	_, err = svc.Get(context.Background(), o.ID, missingUUID)
	assert.ErrorIs(t, err, order.ErrForbidden)
}

func TestService_Cancel_NotFound(t *testing.T) {
	_, svc, cleanup := setup(t)
	defer cleanup()

	err := svc.Cancel(context.Background(), missingUUID, missingUUID)
	assert.ErrorIs(t, err, order.ErrOrderNotFound)
}

func TestService_Cancel_NotPlaced(t *testing.T) {
	pool, svc, cleanup := setup(t)
	defer cleanup()

	_, itemID, userID := seedScenario(t, pool, 5)
	o, err := svc.Place(context.Background(), order.PlaceOrderInput{
		UserID: userID, Plant: testPlant, SupplyDate: testSupplyDate,
		Items: []order.PlaceItem{{MenuItemID: itemID, Qty: 1}},
	})
	require.NoError(t, err)
	// Force READY: users may only cancel PLACED orders.
	forceStatus(t, pool, o.ID, order.StatusReady)

	err = svc.Cancel(context.Background(), o.ID, userID)
	assert.ErrorIs(t, err, order.ErrInvalidTransition)
}

func TestService_Place_InvalidQty(t *testing.T) {
	pool, svc, cleanup := setup(t)
	defer cleanup()

	_, itemID, userID := seedScenario(t, pool, 5)
	_, err := svc.Place(context.Background(), order.PlaceOrderInput{
		UserID: userID, Plant: testPlant, SupplyDate: testSupplyDate,
		Items: []order.PlaceItem{{MenuItemID: itemID, Qty: 0}},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "qty must be positive")
}

func TestService_Place_MenuItemNotFound(t *testing.T) {
	pool, svc, cleanup := setup(t)
	defer cleanup()

	_, _, userID := seedScenario(t, pool, 5)
	_, err := svc.Place(context.Background(), order.PlaceOrderInput{
		UserID: userID, Plant: testPlant, SupplyDate: testSupplyDate,
		Items: []order.PlaceItem{{MenuItemID: missingUUID, Qty: 1}},
	})
	assert.ErrorIs(t, err, menu.ErrItemNotFound)
}

func TestService_Place_MultiVendor(t *testing.T) {
	pool, svc, cleanup := setup(t)
	defer cleanup()

	vendorID, itemA, userID := seedScenario(t, pool, 5)
	// A second vendor's item — mixing vendors in one order is rejected before
	// the transaction opens.
	var otherVendor string
	require.NoError(t, pool.QueryRow(context.Background(), `
INSERT INTO vendor (display_name, legal_name, contact_email, status)
VALUES ('Other', 'Other Ltd', 'mv@vendor.com', 'approved')
RETURNING id`).Scan(&otherVendor))
	foreignItem := seedExtraItem(t, pool, otherVendor, 150, 10)
	_ = vendorID

	_, err := svc.Place(context.Background(), order.PlaceOrderInput{
		UserID: userID, Plant: testPlant, SupplyDate: testSupplyDate,
		Items: []order.PlaceItem{{MenuItemID: itemA, Qty: 1}, {MenuItemID: foreignItem, Qty: 1}},
	})
	assert.ErrorIs(t, err, order.ErrMultiVendor)
}

func TestService_ListByVendorDay(t *testing.T) {
	pool, svc, cleanup := setup(t)
	defer cleanup()

	vendorID, itemID, userID := seedScenario(t, pool, 5)
	o, err := svc.Place(context.Background(), order.PlaceOrderInput{
		UserID: userID, Plant: testPlant, SupplyDate: testSupplyDate,
		Items: []order.PlaceItem{{MenuItemID: itemID, Qty: 1}},
	})
	require.NoError(t, err)

	// No status filter returns the placed order.
	all, err := svc.ListByVendorDay(context.Background(), vendorID, testSupplyDate, nil)
	require.NoError(t, err)
	require.Len(t, all, 1)
	assert.Equal(t, o.ID, all[0].ID)

	// Filtering to a status the order is not in returns nothing.
	none, err := svc.ListByVendorDay(context.Background(), vendorID, testSupplyDate,
		[]order.Status{order.StatusReady})
	require.NoError(t, err)
	assert.Empty(t, none)
}

func TestService_MarkReady_EmptyBatch(t *testing.T) {
	pool, svc, cleanup := setup(t)
	defer cleanup()

	vendorID, _, userID := seedScenario(t, pool, 5)
	// An empty batch is a no-op success — the tx commits with nothing and the
	// observability emit is skipped (len(orderIDs)==0).
	require.NoError(t, svc.MarkReady(context.Background(), vendorID, nil, userID))
}

func TestService_MarkReady_OrderNotFound(t *testing.T) {
	pool, svc, cleanup := setup(t)
	defer cleanup()

	vendorID, _, _ := seedScenario(t, pool, 5)
	// A non-existent order id surfaces ErrOrderNotFound from inside the tx,
	// rolling the whole batch back.
	err := svc.MarkReady(context.Background(), vendorID, []string{missingUUID},
		"00000000-0000-0000-0000-000000000000")
	assert.ErrorIs(t, err, order.ErrOrderNotFound)
}

func TestService_MarkNoShow_Happy(t *testing.T) {
	pool, svc, cleanup := setup(t)
	defer cleanup()
	ctx := context.Background()

	_, itemID, userID := seedScenario(t, pool, 5)

	// Place with the default (pre-cutoff) clock, then sweep with a later clock
	// so the threshold (sweepNow - cutoffAge) lands after each order's ready_at.
	var ids []string
	for i := 0; i < 2; i++ {
		o, err := svc.Place(ctx, order.PlaceOrderInput{
			UserID: userID, Plant: testPlant, SupplyDate: testSupplyDate,
			Items: []order.PlaceItem{{MenuItemID: itemID, Qty: 1}},
		})
		require.NoError(t, err)
		ids = append(ids, o.ID)
	}

	sweepNow := time.Date(2026, 5, 14, 12, 0, 0, 0, time.UTC)
	stale := sweepNow.Add(-2 * time.Hour)
	for _, id := range ids {
		_, err := pool.Exec(ctx, `
UPDATE "order" SET status='ready', ready_at=$2, updated_at=now() WHERE id=$1`, id, stale)
		require.NoError(t, err)
	}

	sweeper := newSvcWithClock(t, pool, fixedClock{T: sweepNow})
	// cutoffAge 30m: threshold = sweepNow-30m, both ready_at (sweepNow-2h) are older.
	n, err := sweeper.MarkNoShow(ctx, 30*time.Minute)
	require.NoError(t, err)
	assert.Equal(t, 2, n)

	for _, id := range ids {
		var status string
		require.NoError(t, pool.QueryRow(ctx,
			`SELECT status FROM "order" WHERE id=$1`, id).Scan(&status))
		assert.Equal(t, "no_show", status)
	}

	// A fresh ready order (ready_at = sweepNow) is NOT swept.
	fresh, err := svc.Place(ctx, order.PlaceOrderInput{
		UserID: userID, Plant: testPlant, SupplyDate: testSupplyDate,
		Items: []order.PlaceItem{{MenuItemID: itemID, Qty: 1}},
	})
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `
UPDATE "order" SET status='ready', ready_at=$2, updated_at=now() WHERE id=$1`, fresh.ID, sweepNow)
	require.NoError(t, err)

	n, err = sweeper.MarkNoShow(ctx, 30*time.Minute)
	require.NoError(t, err)
	assert.Equal(t, 0, n, "a fresh ready order must not be swept")
}

func TestService_MarkNoShow_NonePending(t *testing.T) {
	pool, _, cleanup := setup(t)
	defer cleanup()

	svc := newSvcWithClock(t, pool, fixedClock{T: time.Date(2026, 5, 14, 12, 0, 0, 0, time.UTC)})
	n, err := svc.MarkNoShow(context.Background(), time.Hour)
	require.NoError(t, err)
	assert.Equal(t, 0, n)
}

func TestService_PrepSheet_Happy(t *testing.T) {
	pool, svc, cleanup := setup(t)
	defer cleanup()
	ctx := context.Background()

	vendorID, itemA, userID := seedScenario(t, pool, 5)
	itemB := seedExtraItem(t, pool, vendorID, 200, 10)

	_, err := svc.Place(ctx, order.PlaceOrderInput{
		UserID: userID, Plant: testPlant, SupplyDate: testSupplyDate,
		Items: []order.PlaceItem{{MenuItemID: itemA, Qty: 2}, {MenuItemID: itemB, Qty: 1}},
	})
	require.NoError(t, err)

	// A second order re-uses itemA — its name is hydrated once then deduped
	// (the "already seen → continue" branch in PrepSheet's name loop).
	var user2 string
	require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO "user" (primary_email, display_name, role, status, plant)
VALUES ('prep2@test.com', 'Prep2', 'employee', 'active', $1)
RETURNING id`, testPlant).Scan(&user2))
	_, err = svc.Place(ctx, order.PlaceOrderInput{
		UserID: user2, Plant: testPlant, SupplyDate: testSupplyDate,
		Items: []order.PlaceItem{{MenuItemID: itemA, Qty: 1}},
	})
	require.NoError(t, err)

	sheet, err := svc.PrepSheet(ctx, vendorID, testSupplyDate)
	require.NoError(t, err)
	require.NotNil(t, sheet)
	assert.Equal(t, vendorID, sheet.VendorID)
	assert.Equal(t, 2, sheet.TotalOrders)
	assert.Equal(t, 4, sheet.TotalPortions) // (2 + 1) + 1
	require.Len(t, sheet.Plants, 1)
	assert.Equal(t, testPlant, sheet.Plants[0].Plant)
	// Names are hydrated from the menu repo.
	nameByID := map[string]string{}
	for _, it := range sheet.Plants[0].Items {
		nameByID[it.MenuItemID] = it.Name
	}
	assert.Equal(t, "Item", nameByID[itemA])
	assert.Equal(t, "Item B", nameByID[itemB])
}

func TestService_PrepSheet_EmptyDay(t *testing.T) {
	pool, svc, cleanup := setup(t)
	defer cleanup()

	vendorID, _, _ := seedScenario(t, pool, 5)
	sheet, err := svc.PrepSheet(context.Background(), vendorID,
		time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC))
	require.NoError(t, err)
	assert.Equal(t, 0, sheet.TotalOrders)
	assert.Empty(t, sheet.Plants)
}
