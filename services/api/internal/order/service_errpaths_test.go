package order_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	menu "github.com/Agentic-Build/corporate-catering-system/services/api/internal/menu"
	"github.com/Agentic-Build/corporate-catering-system/services/api/internal/order"
	plaudit "github.com/Agentic-Build/corporate-catering-system/services/api/internal/platform/audit"
	vendor "github.com/Agentic-Build/corporate-catering-system/services/api/internal/vendors"
)

// errBoom is the sentinel injected fakes return to drive a repo error branch.
var errBoom = errors.New("boom: injected repo failure")

// ---- narrow-interface fakes (delegate the rest, error on one method) ----

// errStateTx satisfies order.StateEventAppender and always fails AppendTx.
type errStateTx struct{ order.StateEventAppender }

func (errStateTx) AppendTx(context.Context, pgx.Tx, *order.StateEvent) error { return errBoom }

// errOutboxTx satisfies order.OutboxAppender and always fails AppendTx.
type errOutboxTx struct{ order.OutboxAppender }

func (errOutboxTx) AppendTx(context.Context, pgx.Tx, string, string, string, map[string]any, map[string]any) error {
	return errBoom
}

// errAuditTx satisfies order.AuditTxWriter and always fails WriteTx.
type errAuditTx struct{ order.AuditTxWriter }

func (errAuditTx) WriteTx(context.Context, pgx.Tx, plaudit.Entry) error { return errBoom }

// errOrderTx wraps a real order.OrderTx, failing exactly one of its methods.
type errOrderTx struct {
	order.OrderTx
	failCreate     bool
	failMarkReady  bool
	failUpdate     bool
	failReplace    bool
	failMarkNoShow bool
}

func (e errOrderTx) CreateTx(ctx context.Context, tx pgx.Tx, o *order.Order) error {
	if e.failCreate {
		return errBoom
	}
	return e.OrderTx.CreateTx(ctx, tx, o)
}

func (e errOrderTx) MarkReadyTx(ctx context.Context, tx pgx.Tx, id string) error {
	if e.failMarkReady {
		return errBoom
	}
	return e.OrderTx.MarkReadyTx(ctx, tx, id)
}

func (e errOrderTx) UpdateStatusTx(ctx context.Context, tx pgx.Tx, id string, from, to order.Status) error {
	if e.failUpdate {
		return errBoom
	}
	return e.OrderTx.UpdateStatusTx(ctx, tx, id, from, to)
}

func (e errOrderTx) ReplaceItemsTx(ctx context.Context, tx pgx.Tx, orderID string, items []order.Item, totalMinor int64, notes string) error {
	if e.failReplace {
		return errBoom
	}
	return e.OrderTx.ReplaceItemsTx(ctx, tx, orderID, items, totalMinor, notes)
}

func (e errOrderTx) MarkNoShowTx(ctx context.Context, tx pgx.Tx, id string) error {
	if e.failMarkNoShow {
		return errBoom
	}
	return e.OrderTx.MarkNoShowTx(ctx, tx, id)
}

// deadlockErr is a Postgres 40P01 (deadlock_detected) that MaybeConcurrencyErr
// classifies as ErrConcurrentModification.
var deadlockErr = &pgconn.PgError{Code: "40P01", Message: "deadlock detected"}

// deadlockOrderTx fails CreateTx / MarkPickedUpTx with a deadlock error to
// drive the concurrent_modification outcome branches.
type deadlockOrderTx struct {
	order.OrderTx
	onCreate   bool
	onPickedUp bool
}

func (d deadlockOrderTx) CreateTx(ctx context.Context, tx pgx.Tx, o *order.Order) error {
	if d.onCreate {
		return deadlockErr
	}
	return d.OrderTx.CreateTx(ctx, tx, o)
}

func (d deadlockOrderTx) MarkPickedUpTx(ctx context.Context, tx pgx.Tx, id string) error {
	if d.onPickedUp {
		return deadlockErr
	}
	return d.OrderTx.MarkPickedUpTx(ctx, tx, id)
}

// errQuotaTx wraps a real order.QuotaTx, failing RestoreTx on demand.
type errQuotaTx struct {
	order.QuotaTx
	failRestore bool
}

func (e errQuotaTx) RestoreTx(ctx context.Context, tx pgx.Tx, itemID string, date time.Time, n int) error {
	if e.failRestore {
		return errBoom
	}
	return e.QuotaTx.RestoreTx(ctx, tx, itemID, date, n)
}

// errItems satisfies menu.ItemRepository, failing GetByID for a specific id.
type errItems struct {
	menu.ItemRepository
	failID string
}

func (e errItems) GetByID(ctx context.Context, id string) (*menu.Item, error) {
	if id == e.failID {
		return nil, errBoom
	}
	return e.ItemRepository.GetByID(ctx, id)
}

// errPlants satisfies vendor.PlantMappingRepository, always failing ListByVendor.
type errPlants struct{ vendor.PlantMappingRepository }

func (errPlants) ListByVendor(context.Context, string) ([]*vendor.PlantMapping, error) {
	return nil, errBoom
}

// errVendors satisfies order.VendorReader, always failing GetByID.
type errVendors struct{}

func (errVendors) GetByID(context.Context, string) (*vendor.Vendor, error) { return nil, errBoom }

// errReadyRepo wraps order.Repository, failing ListReadyOlderThan.
type errReadyRepo struct{ order.Repository }

func (errReadyRepo) ListReadyOlderThan(context.Context, time.Time) ([]*order.Order, error) {
	return nil, errBoom
}

// errVendorDayRepo wraps order.Repository, failing ListByVendorDay.
type errVendorDayRepo struct{ order.Repository }

func (errVendorDayRepo) ListByVendorDay(context.Context, string, time.Time, []order.Status) ([]*order.Order, error) {
	return nil, errBoom
}

// ---- Place error branches ----

func TestService_Place_PlantsLookupFails(t *testing.T) {
	pool, svc, cleanup := setup(t)
	defer cleanup()
	_, itemID, userID := seedScenario(t, pool, 5)

	svc.Plants = errPlants{svc.Plants}
	_, err := svc.Place(context.Background(), order.PlaceOrderInput{
		UserID: userID, Plant: testPlant, SupplyDate: testSupplyDate,
		Items: []order.PlaceItem{{MenuItemID: itemID, Qty: 1}},
	})
	assert.ErrorIs(t, err, errBoom)
}

func TestService_Place_VendorLookupFails(t *testing.T) {
	pool, svc, cleanup := setup(t)
	defer cleanup()
	_, itemID, userID := seedScenario(t, pool, 5)

	svc.Vendors = errVendors{}
	_, err := svc.Place(context.Background(), order.PlaceOrderInput{
		UserID: userID, Plant: testPlant, SupplyDate: testSupplyDate,
		Items: []order.PlaceItem{{MenuItemID: itemID, Qty: 1}},
	})
	assert.ErrorIs(t, err, errBoom)
}

func TestService_Place_CutoffPassed(t *testing.T) {
	pool, _, cleanup := setup(t)
	defer cleanup()
	_, itemID, userID := seedScenario(t, pool, 5)

	// Clock past the vendor cutoff (day before supply, 17:00 UTC).
	svc := newSvcWithClock(t, pool, fixedClock{T: testCutoffAt.Add(time.Hour)})
	_, err := svc.Place(context.Background(), order.PlaceOrderInput{
		UserID: userID, Plant: testPlant, SupplyDate: testSupplyDate,
		Items: []order.PlaceItem{{MenuItemID: itemID, Qty: 1}},
	})
	assert.ErrorIs(t, err, order.ErrCutoffPassed)
}

func TestService_Place_CreateTxFails(t *testing.T) {
	pool, svc, cleanup := setup(t)
	defer cleanup()
	_, itemID, userID := seedScenario(t, pool, 5)

	svc.OrdersTx = errOrderTx{OrderTx: svc.OrdersTx, failCreate: true}
	_, err := svc.Place(context.Background(), order.PlaceOrderInput{
		UserID: userID, Plant: testPlant, SupplyDate: testSupplyDate,
		Items: []order.PlaceItem{{MenuItemID: itemID, Qty: 1}},
	})
	assert.ErrorIs(t, err, errBoom)
	// Rolled back: quota restored, no order.
	assert.Equal(t, 5, remainOf(t, pool, itemID))
}

func TestService_Place_StateAppendFails(t *testing.T) {
	pool, svc, cleanup := setup(t)
	defer cleanup()
	_, itemID, userID := seedScenario(t, pool, 5)

	svc.StateTx = errStateTx{svc.StateTx}
	_, err := svc.Place(context.Background(), order.PlaceOrderInput{
		UserID: userID, Plant: testPlant, SupplyDate: testSupplyDate,
		Items: []order.PlaceItem{{MenuItemID: itemID, Qty: 1}},
	})
	assert.ErrorIs(t, err, errBoom)
	assert.Equal(t, 5, remainOf(t, pool, itemID))
}

func TestService_Place_OutboxAppendFails(t *testing.T) {
	pool, svc, cleanup := setup(t)
	defer cleanup()
	_, itemID, userID := seedScenario(t, pool, 5)

	svc.OutboxTx = errOutboxTx{svc.OutboxTx}
	_, err := svc.Place(context.Background(), order.PlaceOrderInput{
		UserID: userID, Plant: testPlant, SupplyDate: testSupplyDate,
		Items: []order.PlaceItem{{MenuItemID: itemID, Qty: 1}},
	})
	assert.ErrorIs(t, err, errBoom)
	assert.Equal(t, 5, remainOf(t, pool, itemID))
}

func TestService_Place_AuditWriteFails(t *testing.T) {
	pool, svc, cleanup := setup(t)
	defer cleanup()
	_, itemID, userID := seedScenario(t, pool, 5)

	svc.AuditTx = errAuditTx{svc.AuditTx}
	_, err := svc.Place(context.Background(), order.PlaceOrderInput{
		UserID: userID, Plant: testPlant, SupplyDate: testSupplyDate,
		Items: []order.PlaceItem{{MenuItemID: itemID, Qty: 1}},
	})
	assert.ErrorIs(t, err, errBoom)
	assert.Equal(t, 5, remainOf(t, pool, itemID))
}

func TestService_Place_ConcurrentModification(t *testing.T) {
	pool, svc, cleanup := setup(t)
	defer cleanup()
	_, itemID, userID := seedScenario(t, pool, 5)

	svc.OrdersTx = deadlockOrderTx{OrderTx: svc.OrdersTx, onCreate: true}
	_, err := svc.Place(context.Background(), order.PlaceOrderInput{
		UserID: userID, Plant: testPlant, SupplyDate: testSupplyDate,
		Items: []order.PlaceItem{{MenuItemID: itemID, Qty: 1}},
	})
	assert.ErrorIs(t, err, order.ErrConcurrentModification)
	// Rolled back: quota restored.
	assert.Equal(t, 5, remainOf(t, pool, itemID))
}

func TestService_Pickup_ConcurrentModification(t *testing.T) {
	pool, svc, cleanup := setup(t)
	defer cleanup()
	_, itemID, userID := seedScenario(t, pool, 5)
	orderID := pickupReadyOrder(t, pool, svc, itemID, userID)

	svc.OrdersTx = deadlockOrderTx{OrderTx: svc.OrdersTx, onPickedUp: true}
	err := svc.Pickup(context.Background(), orderID, userID)
	assert.ErrorIs(t, err, order.ErrConcurrentModification)
	// Order untouched.
	after, err := svc.Get(context.Background(), orderID, userID)
	require.NoError(t, err)
	assert.Equal(t, order.StatusReady, after.Status)
}

// ---- Cancel error branches ----

func TestService_Cancel_UpdateStatusFails(t *testing.T) {
	pool, svc, cleanup := setup(t)
	defer cleanup()
	_, itemID, userID := seedScenario(t, pool, 5)

	o, err := svc.Place(context.Background(), order.PlaceOrderInput{
		UserID: userID, Plant: testPlant, SupplyDate: testSupplyDate,
		Items: []order.PlaceItem{{MenuItemID: itemID, Qty: 1}},
	})
	require.NoError(t, err)

	svc.OrdersTx = errOrderTx{OrderTx: svc.OrdersTx, failUpdate: true}
	err = svc.Cancel(context.Background(), o.ID, userID)
	assert.ErrorIs(t, err, errBoom)
}

func TestService_Cancel_RestoreFails(t *testing.T) {
	pool, svc, cleanup := setup(t)
	defer cleanup()
	_, itemID, userID := seedScenario(t, pool, 5)

	o, err := svc.Place(context.Background(), order.PlaceOrderInput{
		UserID: userID, Plant: testPlant, SupplyDate: testSupplyDate,
		Items: []order.PlaceItem{{MenuItemID: itemID, Qty: 1}},
	})
	require.NoError(t, err)

	svc.QuotaTx = errQuotaTx{QuotaTx: svc.QuotaTx, failRestore: true}
	err = svc.Cancel(context.Background(), o.ID, userID)
	assert.ErrorIs(t, err, errBoom)
	// Status unchanged (tx rolled back).
	after, err := svc.Get(context.Background(), o.ID, userID)
	require.NoError(t, err)
	assert.Equal(t, order.StatusPlaced, after.Status)
}

func TestService_Cancel_StateAppendFails(t *testing.T) {
	pool, svc, cleanup := setup(t)
	defer cleanup()
	_, itemID, userID := seedScenario(t, pool, 5)

	o, err := svc.Place(context.Background(), order.PlaceOrderInput{
		UserID: userID, Plant: testPlant, SupplyDate: testSupplyDate,
		Items: []order.PlaceItem{{MenuItemID: itemID, Qty: 1}},
	})
	require.NoError(t, err)

	svc.StateTx = errStateTx{svc.StateTx}
	err = svc.Cancel(context.Background(), o.ID, userID)
	assert.ErrorIs(t, err, errBoom)
}

func TestService_Cancel_OutboxAppendFails(t *testing.T) {
	pool, svc, cleanup := setup(t)
	defer cleanup()
	_, itemID, userID := seedScenario(t, pool, 5)

	o, err := svc.Place(context.Background(), order.PlaceOrderInput{
		UserID: userID, Plant: testPlant, SupplyDate: testSupplyDate,
		Items: []order.PlaceItem{{MenuItemID: itemID, Qty: 1}},
	})
	require.NoError(t, err)

	svc.OutboxTx = errOutboxTx{svc.OutboxTx}
	err = svc.Cancel(context.Background(), o.ID, userID)
	assert.ErrorIs(t, err, errBoom)
}

func TestService_Cancel_AuditWriteFails(t *testing.T) {
	pool, svc, cleanup := setup(t)
	defer cleanup()
	_, itemID, userID := seedScenario(t, pool, 5)

	o, err := svc.Place(context.Background(), order.PlaceOrderInput{
		UserID: userID, Plant: testPlant, SupplyDate: testSupplyDate,
		Items: []order.PlaceItem{{MenuItemID: itemID, Qty: 1}},
	})
	require.NoError(t, err)

	svc.AuditTx = errAuditTx{svc.AuditTx}
	err = svc.Cancel(context.Background(), o.ID, userID)
	assert.ErrorIs(t, err, errBoom)
}

// ---- Modify error branches ----

func TestService_Modify_Empty(t *testing.T) {
	_, svc, cleanup := setup(t)
	defer cleanup()
	_, err := svc.Modify(context.Background(), order.ModifyOrderInput{
		OrderID: missingUUID, UserID: missingUUID, Items: nil,
	})
	assert.ErrorIs(t, err, order.ErrEmptyOrder)
}

func TestService_Modify_OrderNotFound(t *testing.T) {
	_, svc, cleanup := setup(t)
	defer cleanup()
	_, err := svc.Modify(context.Background(), order.ModifyOrderInput{
		OrderID: missingUUID, UserID: missingUUID,
		Items: []order.PlaceItem{{MenuItemID: missingUUID, Qty: 1}},
	})
	assert.ErrorIs(t, err, order.ErrOrderNotFound)
}

func TestService_Modify_InvalidQty(t *testing.T) {
	pool, svc, cleanup := setup(t)
	defer cleanup()
	_, itemID, userID := seedScenario(t, pool, 5)
	o, err := svc.Place(context.Background(), order.PlaceOrderInput{
		UserID: userID, Plant: testPlant, SupplyDate: testSupplyDate,
		Items: []order.PlaceItem{{MenuItemID: itemID, Qty: 1}},
	})
	require.NoError(t, err)

	_, err = svc.Modify(context.Background(), order.ModifyOrderInput{
		OrderID: o.ID, UserID: userID,
		Items: []order.PlaceItem{{MenuItemID: itemID, Qty: 0}},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "qty must be positive")
}

func TestService_Modify_MenuItemLookupFails(t *testing.T) {
	pool, svc, cleanup := setup(t)
	defer cleanup()
	_, itemID, userID := seedScenario(t, pool, 5)
	o, err := svc.Place(context.Background(), order.PlaceOrderInput{
		UserID: userID, Plant: testPlant, SupplyDate: testSupplyDate,
		Items: []order.PlaceItem{{MenuItemID: itemID, Qty: 1}},
	})
	require.NoError(t, err)

	svc.Items = errItems{ItemRepository: svc.Items, failID: itemID}
	_, err = svc.Modify(context.Background(), order.ModifyOrderInput{
		OrderID: o.ID, UserID: userID,
		Items: []order.PlaceItem{{MenuItemID: itemID, Qty: 2}},
	})
	assert.ErrorIs(t, err, errBoom)
}

func TestService_Modify_RestoreFails(t *testing.T) {
	pool, svc, cleanup := setup(t)
	defer cleanup()
	_, itemID, userID := seedScenario(t, pool, 5)
	o, err := svc.Place(context.Background(), order.PlaceOrderInput{
		UserID: userID, Plant: testPlant, SupplyDate: testSupplyDate,
		Items: []order.PlaceItem{{MenuItemID: itemID, Qty: 2}},
	})
	require.NoError(t, err)

	// Modify down (2→1) triggers a RestoreTx (negative delta) → fail it.
	svc.QuotaTx = errQuotaTx{QuotaTx: svc.QuotaTx, failRestore: true}
	_, err = svc.Modify(context.Background(), order.ModifyOrderInput{
		OrderID: o.ID, UserID: userID,
		Items: []order.PlaceItem{{MenuItemID: itemID, Qty: 1}},
	})
	assert.ErrorIs(t, err, errBoom)
}

func TestService_Modify_ReplaceItemsFails(t *testing.T) {
	pool, svc, cleanup := setup(t)
	defer cleanup()
	_, itemID, userID := seedScenario(t, pool, 5)
	o, err := svc.Place(context.Background(), order.PlaceOrderInput{
		UserID: userID, Plant: testPlant, SupplyDate: testSupplyDate,
		Items: []order.PlaceItem{{MenuItemID: itemID, Qty: 1}},
	})
	require.NoError(t, err)

	svc.OrdersTx = errOrderTx{OrderTx: svc.OrdersTx, failReplace: true}
	_, err = svc.Modify(context.Background(), order.ModifyOrderInput{
		OrderID: o.ID, UserID: userID,
		Items: []order.PlaceItem{{MenuItemID: itemID, Qty: 2}},
	})
	assert.ErrorIs(t, err, errBoom)
}

func TestService_Modify_OutboxAppendFails(t *testing.T) {
	pool, svc, cleanup := setup(t)
	defer cleanup()
	_, itemID, userID := seedScenario(t, pool, 5)
	o, err := svc.Place(context.Background(), order.PlaceOrderInput{
		UserID: userID, Plant: testPlant, SupplyDate: testSupplyDate,
		Items: []order.PlaceItem{{MenuItemID: itemID, Qty: 1}},
	})
	require.NoError(t, err)

	svc.OutboxTx = errOutboxTx{svc.OutboxTx}
	_, err = svc.Modify(context.Background(), order.ModifyOrderInput{
		OrderID: o.ID, UserID: userID,
		Items: []order.PlaceItem{{MenuItemID: itemID, Qty: 2}},
	})
	assert.ErrorIs(t, err, errBoom)
}

func TestService_Modify_AuditWriteFails(t *testing.T) {
	pool, svc, cleanup := setup(t)
	defer cleanup()
	_, itemID, userID := seedScenario(t, pool, 5)
	o, err := svc.Place(context.Background(), order.PlaceOrderInput{
		UserID: userID, Plant: testPlant, SupplyDate: testSupplyDate,
		Items: []order.PlaceItem{{MenuItemID: itemID, Qty: 1}},
	})
	require.NoError(t, err)

	svc.AuditTx = errAuditTx{svc.AuditTx}
	_, err = svc.Modify(context.Background(), order.ModifyOrderInput{
		OrderID: o.ID, UserID: userID,
		Items: []order.PlaceItem{{MenuItemID: itemID, Qty: 2}},
	})
	assert.ErrorIs(t, err, errBoom)
}

// ---- MarkReady error branches (inside markOneReady) ----

func TestService_MarkReady_MarkReadyTxFails(t *testing.T) {
	pool, svc, cleanup := setup(t)
	defer cleanup()
	vendorID, itemID, userID := seedScenario(t, pool, 5)
	o, err := svc.Place(context.Background(), order.PlaceOrderInput{
		UserID: userID, Plant: testPlant, SupplyDate: testSupplyDate,
		Items: []order.PlaceItem{{MenuItemID: itemID, Qty: 1}},
	})
	require.NoError(t, err)

	svc.OrdersTx = errOrderTx{OrderTx: svc.OrdersTx, failMarkReady: true}
	err = svc.MarkReady(context.Background(), vendorID, []string{o.ID}, userID)
	assert.ErrorIs(t, err, errBoom)
}

func TestService_MarkReady_StateAppendFails(t *testing.T) {
	pool, svc, cleanup := setup(t)
	defer cleanup()
	vendorID, itemID, userID := seedScenario(t, pool, 5)
	o, err := svc.Place(context.Background(), order.PlaceOrderInput{
		UserID: userID, Plant: testPlant, SupplyDate: testSupplyDate,
		Items: []order.PlaceItem{{MenuItemID: itemID, Qty: 1}},
	})
	require.NoError(t, err)

	svc.StateTx = errStateTx{svc.StateTx}
	err = svc.MarkReady(context.Background(), vendorID, []string{o.ID}, userID)
	assert.ErrorIs(t, err, errBoom)
}

func TestService_MarkReady_OutboxAppendFails(t *testing.T) {
	pool, svc, cleanup := setup(t)
	defer cleanup()
	vendorID, itemID, userID := seedScenario(t, pool, 5)
	o, err := svc.Place(context.Background(), order.PlaceOrderInput{
		UserID: userID, Plant: testPlant, SupplyDate: testSupplyDate,
		Items: []order.PlaceItem{{MenuItemID: itemID, Qty: 1}},
	})
	require.NoError(t, err)

	svc.OutboxTx = errOutboxTx{svc.OutboxTx}
	err = svc.MarkReady(context.Background(), vendorID, []string{o.ID}, userID)
	assert.ErrorIs(t, err, errBoom)
}

func TestService_MarkReady_AuditWriteFails(t *testing.T) {
	pool, svc, cleanup := setup(t)
	defer cleanup()
	vendorID, itemID, userID := seedScenario(t, pool, 5)
	o, err := svc.Place(context.Background(), order.PlaceOrderInput{
		UserID: userID, Plant: testPlant, SupplyDate: testSupplyDate,
		Items: []order.PlaceItem{{MenuItemID: itemID, Qty: 1}},
	})
	require.NoError(t, err)

	svc.AuditTx = errAuditTx{svc.AuditTx}
	err = svc.MarkReady(context.Background(), vendorID, []string{o.ID}, userID)
	assert.ErrorIs(t, err, errBoom)
}

// ---- Pickup error branches (inside the tx) ----

func TestService_Pickup_StateAppendFails(t *testing.T) {
	pool, svc, cleanup := setup(t)
	defer cleanup()
	_, itemID, userID := seedScenario(t, pool, 5)
	orderID := pickupReadyOrder(t, pool, svc, itemID, userID)

	svc.StateTx = errStateTx{svc.StateTx}
	err := svc.Pickup(context.Background(), orderID, userID)
	assert.ErrorIs(t, err, errBoom)
	// Rolled back: still READY.
	after, err := svc.Get(context.Background(), orderID, userID)
	require.NoError(t, err)
	assert.Equal(t, order.StatusReady, after.Status)
}

func TestService_Pickup_OutboxAppendFails(t *testing.T) {
	pool, svc, cleanup := setup(t)
	defer cleanup()
	_, itemID, userID := seedScenario(t, pool, 5)
	orderID := pickupReadyOrder(t, pool, svc, itemID, userID)

	svc.OutboxTx = errOutboxTx{svc.OutboxTx}
	err := svc.Pickup(context.Background(), orderID, userID)
	assert.ErrorIs(t, err, errBoom)
}

func TestService_Pickup_AuditWriteFails(t *testing.T) {
	pool, svc, cleanup := setup(t)
	defer cleanup()
	_, itemID, userID := seedScenario(t, pool, 5)
	orderID := pickupReadyOrder(t, pool, svc, itemID, userID)

	svc.AuditTx = errAuditTx{svc.AuditTx}
	err := svc.Pickup(context.Background(), orderID, userID)
	assert.ErrorIs(t, err, errBoom)
}

// ---- MarkNoShow error branches ----

func TestService_MarkNoShow_ListFails(t *testing.T) {
	pool, _, cleanup := setup(t)
	defer cleanup()

	svc := newSvcWithClock(t, pool, fixedClock{T: testClockTime})
	svc.Orders = errReadyRepo{svc.Orders}
	_, err := svc.MarkNoShow(context.Background(), time.Hour)
	assert.ErrorIs(t, err, errBoom)
}

// ---- PrepSheet error branches ----

func TestService_PrepSheet_ListFails(t *testing.T) {
	pool, svc, cleanup := setup(t)
	defer cleanup()
	vendorID, _, _ := seedScenario(t, pool, 5)

	svc.Orders = errVendorDayRepo{svc.Orders}
	_, err := svc.PrepSheet(context.Background(), vendorID, testSupplyDate)
	assert.ErrorIs(t, err, errBoom)
}

func TestService_PrepSheet_ItemNameLookupFails(t *testing.T) {
	pool, svc, cleanup := setup(t)
	defer cleanup()
	vendorID, itemID, userID := seedScenario(t, pool, 5)
	_, err := svc.Place(context.Background(), order.PlaceOrderInput{
		UserID: userID, Plant: testPlant, SupplyDate: testSupplyDate,
		Items: []order.PlaceItem{{MenuItemID: itemID, Qty: 1}},
	})
	require.NoError(t, err)

	// PrepSheet hydrates each distinct item's name via Items.GetByID; fail it.
	svc.Items = errItems{ItemRepository: svc.Items, failID: itemID}
	_, err = svc.PrepSheet(context.Background(), vendorID, testSupplyDate)
	assert.ErrorIs(t, err, errBoom)
}

func TestService_MarkNoShow_TxStepsFailAreSkipped(t *testing.T) {
	pool, svc, cleanup := setup(t)
	defer cleanup()
	ctx := context.Background()
	_, itemID, userID := seedScenario(t, pool, 5)

	o, err := svc.Place(ctx, order.PlaceOrderInput{
		UserID: userID, Plant: testPlant, SupplyDate: testSupplyDate,
		Items: []order.PlaceItem{{MenuItemID: itemID, Qty: 1}},
	})
	require.NoError(t, err)

	sweepNow := time.Date(2026, 5, 14, 12, 0, 0, 0, time.UTC)
	stale := sweepNow.Add(-2 * time.Hour)
	_, err = pool.Exec(ctx, `
UPDATE "order" SET status='ready', ready_at=$2, updated_at=now() WHERE id=$1`, o.ID, stale)
	require.NoError(t, err)

	// Each sub-case fails a different tx step; the sweep skips the order (n==0)
	// and the order must remain READY (the per-order tx rolled back).
	cases := []struct {
		name  string
		patch func(s *order.Service)
	}{
		{"markNoShowTx", func(s *order.Service) {
			s.OrdersTx = errOrderTx{OrderTx: s.OrdersTx, failMarkNoShow: true}
		}},
		{"stateAppend", func(s *order.Service) { s.StateTx = errStateTx{s.StateTx} }},
		{"outboxAppend", func(s *order.Service) { s.OutboxTx = errOutboxTx{s.OutboxTx} }},
		{"auditWrite", func(s *order.Service) { s.AuditTx = errAuditTx{s.AuditTx} }},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			sweeper := newSvcWithClock(t, pool, fixedClock{T: sweepNow})
			c.patch(sweeper)
			n, err := sweeper.MarkNoShow(ctx, 30*time.Minute)
			require.NoError(t, err)
			assert.Equal(t, 0, n, "errored order is skipped, not counted")
			var status string
			require.NoError(t, pool.QueryRow(ctx,
				`SELECT status FROM "order" WHERE id=$1`, o.ID).Scan(&status))
			assert.Equal(t, "ready", status, "rolled back, still ready")
		})
	}
}
