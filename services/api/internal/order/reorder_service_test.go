package order_test

import (
	"context"
	"errors"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/takalawang/corporate-catering-system/services/api/internal/menu"
	mpg "github.com/takalawang/corporate-catering-system/services/api/internal/menu/postgres"
	"github.com/takalawang/corporate-catering-system/services/api/internal/order"
	opg "github.com/takalawang/corporate-catering-system/services/api/internal/order/postgres"
	"github.com/takalawang/corporate-catering-system/services/api/internal/quota"
	qpg "github.com/takalawang/corporate-catering-system/services/api/internal/quota/postgres"
	vpg "github.com/takalawang/corporate-catering-system/services/api/internal/vendors/postgres"
)

// itemRepoAdapter bridges *mpg.ItemRepo (menu.Item) → order.ReorderMenuItem,
// mapping menu.ErrItemNotFound → order.ErrReorderItemNotFound so the service
// can stay menu-package-agnostic.
type itemRepoAdapter struct{ inner *mpg.ItemRepo }

func (a itemRepoAdapter) GetByID(ctx context.Context, id string) (*order.ReorderMenuItem, error) {
	mi, err := a.inner.GetByID(ctx, id)
	if err != nil {
		if errors.Is(err, menu.ErrItemNotFound) {
			return nil, order.ErrReorderItemNotFound
		}
		return nil, err
	}
	return &order.ReorderMenuItem{
		ID:         mi.ID,
		Name:       mi.Name,
		PriceMinor: mi.PriceMinor,
		Archived:   mi.Status == menu.ItemStatusArchived || mi.ArchivedAt != nil,
	}, nil
}

// supplyRepoAdapter bridges *qpg.SupplyRepo → order.ReorderSupply, mapping
// quota.ErrSupplyNotFound → order.ErrReorderSupplyNotFound.
type supplyRepoAdapter struct{ inner *qpg.SupplyRepo }

func (a supplyRepoAdapter) Get(ctx context.Context, itemID string, date time.Time) (*order.ReorderSupply, error) {
	s, err := a.inner.Get(ctx, itemID, date)
	if err != nil {
		if errors.Is(err, quota.ErrSupplyNotFound) {
			return nil, order.ErrReorderSupplyNotFound
		}
		return nil, err
	}
	return &order.ReorderSupply{Remain: s.Remain, CutoffAt: s.CutoffAt}, nil
}

func (a supplyRepoAdapter) DecrementTx(ctx context.Context, tx pgx.Tx, itemID string, date time.Time, n int) (int, error) {
	return a.inner.DecrementTx(ctx, tx, itemID, date, n)
}

// migrationsDirReorder mirrors migrationsDir() in service_test.go. We duplicate
// (rather than share) because the controller forbids touching service_test.go,
// and Go runtime.Caller resolution is file-relative.
func migrationsDirReorder() string {
	_, thisFile, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(thisFile), "..", "..", "..", "..", "migrations")
}

// reorderTestEnv bundles the dependencies a ReorderService needs plus the
// Service used to seed a source order.
type reorderTestEnv struct {
	Pool     *pgxpool.Pool
	Reorder  *order.ReorderService
	Place    *order.Service
	Cleanup  func()
}

const reorderTestPlant = "F12B-3F"

var (
	reorderSourceDate = time.Date(2026, 5, 13, 0, 0, 0, 0, time.UTC)
	reorderTargetDate = time.Date(2026, 5, 14, 0, 0, 0, 0, time.UTC)
	// Clock is mid-day on the day before reorderSourceDate so we are well within
	// the 17:00 prev-day cutoff that Service.Place enforces, AND well before
	// reorderTargetDate's cutoff for typical supplies (cutoff_at = day before 17:00 UTC).
	reorderClockTime  = time.Date(2026, 5, 12, 10, 0, 0, 0, time.UTC)
	// targetCutoff is the meal_supply.cutoff_at for reorderTargetDate supplies.
	// Setting it AFTER reorderClockTime keeps target items available.
	reorderTargetCutoff = time.Date(2026, 5, 13, 17, 0, 0, 0, time.UTC)
	// sourceCutoff is the meal_supply.cutoff_at for reorderSourceDate (yesterday from
	// the clock's view). Placing the source order requires this to be in the future.
	reorderSourceCutoff = time.Date(2026, 5, 12, 17, 0, 0, 0, time.UTC)
)

func setupReorder(t *testing.T) reorderTestEnv {
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
	m, err := migrate.New("file://"+migrationsDirReorder(), dsn)
	require.NoError(t, err)
	require.NoError(t, m.Up())
	cfg, err := pgxpool.ParseConfig(dsn)
	require.NoError(t, err)
	cfg.MaxConns = 20
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	require.NoError(t, err)

	orderRepo := opg.NewOrderRepo(pool)
	stateRepo := opg.NewStateEventRepo(pool)
	auditRepo := opg.NewAuditRepo(pool)
	outboxRepo := opg.NewOutboxRepo(pool)
	supplyRepo := qpg.NewSupplyRepo(pool)
	itemRepo := mpg.NewItemRepo(pool)
	plantRepo := vpg.NewPlantMappingRepo(pool)

	placeSvc := &order.Service{
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
		Clock:       fixedClock{T: reorderClockTime},
	}

	reorderSvc := order.NewReorderService(
		pool,
		orderRepo,
		supplyRepoAdapter{inner: supplyRepo},
		itemRepoAdapter{inner: itemRepo},
		stateRepo,
		auditRepo,
		outboxRepo,
		fixedClock{T: reorderClockTime},
	)

	cleanup := func() {
		pool.Close()
		_ = container.Terminate(ctx)
	}
	return reorderTestEnv{Pool: pool, Reorder: reorderSvc, Place: placeSvc, Cleanup: cleanup}
}

// seedReorderScenario provisions vendor + plant mapping + N menu items + user.
// itemNames lets the test customise names so it can assert specific
// unavailable_items entries.
func seedReorderScenario(t *testing.T, pool *pgxpool.Pool, itemNames []string) (vendorID string, itemIDs []string, userID string) {
	t.Helper()
	ctx := context.Background()

	require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO vendor (display_name, legal_name, contact_email, status)
VALUES ('Vendor', 'Vendor Ltd', 'vendor@test.com', 'approved')
RETURNING id`).Scan(&vendorID))

	itemIDs = make([]string, len(itemNames))
	for i, name := range itemNames {
		require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO menu_item (vendor_id, name, description, price_minor, status, tags)
VALUES ($1, $2, '', 110, 'active', ARRAY[]::text[])
RETURNING id`, vendorID, name).Scan(&itemIDs[i]))
	}

	_, err := pool.Exec(ctx,
		`INSERT INTO plant (code, label) VALUES ($1, $1) ON CONFLICT DO NOTHING`, reorderTestPlant)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `
INSERT INTO vendor_plant_mapping (vendor_id, plant, active)
VALUES ($1, $2, true)`, vendorID, reorderTestPlant)
	require.NoError(t, err)

	require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO "user" (primary_email, display_name, role, status, plant)
VALUES ('employee@test.com', 'Employee', 'employee', 'active', $1)
RETURNING id`, reorderTestPlant).Scan(&userID))

	return vendorID, itemIDs, userID
}

// addSupply inserts a meal_supply row for an item on a given date.
func addSupply(t *testing.T, pool *pgxpool.Pool, itemID string, date time.Time, capacity, remain int, cutoff time.Time) {
	t.Helper()
	_, err := pool.Exec(context.Background(), `
INSERT INTO meal_supply (menu_item_id, supply_date, capacity, remain, pickup_window, eta_label, cutoff_at)
VALUES ($1, $2, $3, $4, '', '', $5)`, itemID, date, capacity, remain, cutoff)
	require.NoError(t, err)
}

func placeSourceOrder(t *testing.T, env reorderTestEnv, userID string, items []order.PlaceItem) *order.Order {
	t.Helper()
	o, err := env.Place.Place(context.Background(), order.PlaceOrderInput{
		UserID:     userID,
		Plant:      reorderTestPlant,
		SupplyDate: reorderSourceDate,
		Items:      items,
	})
	require.NoError(t, err)
	return o
}

func TestReorder_AllAvailable(t *testing.T) {
	env := setupReorder(t)
	defer env.Cleanup()

	_, itemIDs, userID := seedReorderScenario(t, env.Pool, []string{"A", "B"})
	// Source date supplies (needed to place the source order).
	addSupply(t, env.Pool, itemIDs[0], reorderSourceDate, 5, 5, reorderSourceCutoff)
	addSupply(t, env.Pool, itemIDs[1], reorderSourceDate, 5, 5, reorderSourceCutoff)
	// Target date supplies (needed for the reorder clone).
	addSupply(t, env.Pool, itemIDs[0], reorderTargetDate, 5, 5, reorderTargetCutoff)
	addSupply(t, env.Pool, itemIDs[1], reorderTargetDate, 5, 5, reorderTargetCutoff)

	src := placeSourceOrder(t, env, userID, []order.PlaceItem{
		{MenuItemID: itemIDs[0], Qty: 2},
		{MenuItemID: itemIDs[1], Qty: 1},
	})

	res, err := env.Reorder.Reorder(context.Background(), order.ReorderInput{
		UserID:        userID,
		SourceOrderID: src.ID,
		SupplyDate:    reorderTargetDate.Format("2006-01-02"),
		Plant:         reorderTestPlant,
	})
	require.NoError(t, err)
	require.NotNil(t, res)
	require.NotEmpty(t, res.NewOrderID, "new order should be created when all items available")
	assert.Empty(t, res.UnavailableItems)

	// New order has the same items + qty as the source.
	newOrder, err := env.Place.Get(context.Background(), res.NewOrderID, userID)
	require.NoError(t, err)
	assert.Equal(t, order.StatusPlaced, newOrder.Status)
	require.Len(t, newOrder.Items, 2)

	gotQty := map[string]int{}
	for _, it := range newOrder.Items {
		gotQty[it.MenuItemID] = it.Qty
	}
	assert.Equal(t, 2, gotQty[itemIDs[0]])
	assert.Equal(t, 1, gotQty[itemIDs[1]])

	// Target-day quota decremented.
	var remain0, remain1 int
	require.NoError(t, env.Pool.QueryRow(context.Background(),
		`SELECT remain FROM meal_supply WHERE menu_item_id=$1 AND supply_date=$2`,
		itemIDs[0], reorderTargetDate).Scan(&remain0))
	require.NoError(t, env.Pool.QueryRow(context.Background(),
		`SELECT remain FROM meal_supply WHERE menu_item_id=$1 AND supply_date=$2`,
		itemIDs[1], reorderTargetDate).Scan(&remain1))
	assert.Equal(t, 3, remain0)
	assert.Equal(t, 4, remain1)
}

func TestReorder_PartialUnavailable_NoSupply(t *testing.T) {
	env := setupReorder(t)
	defer env.Cleanup()

	_, itemIDs, userID := seedReorderScenario(t, env.Pool, []string{"A", "B", "Chicken"})
	// Source date: all three have supply.
	for _, id := range itemIDs {
		addSupply(t, env.Pool, id, reorderSourceDate, 5, 5, reorderSourceCutoff)
	}
	// Target date: only items 0 and 1 have supply. Item 2 ("Chicken") has no row → no_supply.
	addSupply(t, env.Pool, itemIDs[0], reorderTargetDate, 5, 5, reorderTargetCutoff)
	addSupply(t, env.Pool, itemIDs[1], reorderTargetDate, 5, 5, reorderTargetCutoff)

	src := placeSourceOrder(t, env, userID, []order.PlaceItem{
		{MenuItemID: itemIDs[0], Qty: 1},
		{MenuItemID: itemIDs[1], Qty: 1},
		{MenuItemID: itemIDs[2], Qty: 1},
	})

	res, err := env.Reorder.Reorder(context.Background(), order.ReorderInput{
		UserID:        userID,
		SourceOrderID: src.ID,
		SupplyDate:    reorderTargetDate.Format("2006-01-02"),
		Plant:         reorderTestPlant,
	})
	require.NoError(t, err)
	require.NotEmpty(t, res.NewOrderID)
	require.Len(t, res.UnavailableItems, 1)
	assert.Equal(t, itemIDs[2], res.UnavailableItems[0].MenuItemID)
	assert.Equal(t, "Chicken", res.UnavailableItems[0].Name)
	assert.Equal(t, "no_supply", res.UnavailableItems[0].Reason)

	newOrder, err := env.Place.Get(context.Background(), res.NewOrderID, userID)
	require.NoError(t, err)
	require.Len(t, newOrder.Items, 2)
}

func TestReorder_AllUnavailable(t *testing.T) {
	env := setupReorder(t)
	defer env.Cleanup()

	_, itemIDs, userID := seedReorderScenario(t, env.Pool, []string{"A"})
	addSupply(t, env.Pool, itemIDs[0], reorderSourceDate, 5, 5, reorderSourceCutoff)
	// Deliberately no target-date supply.

	src := placeSourceOrder(t, env, userID, []order.PlaceItem{
		{MenuItemID: itemIDs[0], Qty: 1},
	})

	res, err := env.Reorder.Reorder(context.Background(), order.ReorderInput{
		UserID:        userID,
		SourceOrderID: src.ID,
		SupplyDate:    reorderTargetDate.Format("2006-01-02"),
		Plant:         reorderTestPlant,
	})
	require.NoError(t, err, "all-unavailable is not a service error; handler maps to 409")
	require.NotNil(t, res)
	assert.Empty(t, res.NewOrderID, "no order should be created when zero items survive")
	require.Len(t, res.UnavailableItems, 1)
	assert.Equal(t, "no_supply", res.UnavailableItems[0].Reason)

	// Only the source order exists.
	var count int
	require.NoError(t, env.Pool.QueryRow(context.Background(),
		`SELECT count(*) FROM "order"`).Scan(&count))
	assert.Equal(t, 1, count)
}

func TestReorder_NotOwner(t *testing.T) {
	env := setupReorder(t)
	defer env.Cleanup()

	_, itemIDs, userID := seedReorderScenario(t, env.Pool, []string{"A"})
	addSupply(t, env.Pool, itemIDs[0], reorderSourceDate, 5, 5, reorderSourceCutoff)
	addSupply(t, env.Pool, itemIDs[0], reorderTargetDate, 5, 5, reorderTargetCutoff)

	src := placeSourceOrder(t, env, userID, []order.PlaceItem{
		{MenuItemID: itemIDs[0], Qty: 1},
	})

	// Different user attempts to reorder.
	var otherID string
	require.NoError(t, env.Pool.QueryRow(context.Background(), `
INSERT INTO "user" (primary_email, display_name, role, status, plant)
VALUES ('other@test.com', 'Other', 'employee', 'active', $1)
RETURNING id`, reorderTestPlant).Scan(&otherID))

	_, err := env.Reorder.Reorder(context.Background(), order.ReorderInput{
		UserID:        otherID,
		SourceOrderID: src.ID,
		SupplyDate:    reorderTargetDate.Format("2006-01-02"),
		Plant:         reorderTestPlant,
	})
	assert.ErrorIs(t, err, order.ErrForbidden)
}

func TestReorder_CutoffPassed(t *testing.T) {
	env := setupReorder(t)
	defer env.Cleanup()

	_, itemIDs, userID := seedReorderScenario(t, env.Pool, []string{"A"})
	addSupply(t, env.Pool, itemIDs[0], reorderSourceDate, 5, 5, reorderSourceCutoff)
	// Target supply exists but its cutoff is in the past relative to the clock.
	expiredCutoff := reorderClockTime.Add(-1 * time.Hour)
	addSupply(t, env.Pool, itemIDs[0], reorderTargetDate, 5, 5, expiredCutoff)

	src := placeSourceOrder(t, env, userID, []order.PlaceItem{
		{MenuItemID: itemIDs[0], Qty: 1},
	})

	res, err := env.Reorder.Reorder(context.Background(), order.ReorderInput{
		UserID:        userID,
		SourceOrderID: src.ID,
		SupplyDate:    reorderTargetDate.Format("2006-01-02"),
		Plant:         reorderTestPlant,
	})
	require.NoError(t, err)
	assert.Empty(t, res.NewOrderID)
	require.Len(t, res.UnavailableItems, 1)
	assert.Equal(t, "cutoff_passed", res.UnavailableItems[0].Reason)
}

func TestReorder_OutOfQuota(t *testing.T) {
	env := setupReorder(t)
	defer env.Cleanup()

	_, itemIDs, userID := seedReorderScenario(t, env.Pool, []string{"A"})
	addSupply(t, env.Pool, itemIDs[0], reorderSourceDate, 5, 5, reorderSourceCutoff)
	// Target supply has capacity=5 but remain=0 (already exhausted).
	addSupply(t, env.Pool, itemIDs[0], reorderTargetDate, 5, 0, reorderTargetCutoff)

	src := placeSourceOrder(t, env, userID, []order.PlaceItem{
		{MenuItemID: itemIDs[0], Qty: 1},
	})

	res, err := env.Reorder.Reorder(context.Background(), order.ReorderInput{
		UserID:        userID,
		SourceOrderID: src.ID,
		SupplyDate:    reorderTargetDate.Format("2006-01-02"),
		Plant:         reorderTestPlant,
	})
	require.NoError(t, err)
	assert.Empty(t, res.NewOrderID)
	require.Len(t, res.UnavailableItems, 1)
	assert.Equal(t, "out_of_quota", res.UnavailableItems[0].Reason)
}

func TestReorder_Archived(t *testing.T) {
	env := setupReorder(t)
	defer env.Cleanup()

	_, itemIDs, userID := seedReorderScenario(t, env.Pool, []string{"A"})
	addSupply(t, env.Pool, itemIDs[0], reorderSourceDate, 5, 5, reorderSourceCutoff)
	addSupply(t, env.Pool, itemIDs[0], reorderTargetDate, 5, 5, reorderTargetCutoff)

	src := placeSourceOrder(t, env, userID, []order.PlaceItem{
		{MenuItemID: itemIDs[0], Qty: 1},
	})

	// Archive the item AFTER the source order is placed but BEFORE reorder.
	_, err := env.Pool.Exec(context.Background(), `
UPDATE menu_item SET status='archived', archived_at=now() WHERE id=$1`, itemIDs[0])
	require.NoError(t, err)

	res, err := env.Reorder.Reorder(context.Background(), order.ReorderInput{
		UserID:        userID,
		SourceOrderID: src.ID,
		SupplyDate:    reorderTargetDate.Format("2006-01-02"),
		Plant:         reorderTestPlant,
	})
	require.NoError(t, err)
	assert.Empty(t, res.NewOrderID)
	require.Len(t, res.UnavailableItems, 1)
	assert.Equal(t, "archived", res.UnavailableItems[0].Reason)
}

func TestReorder_InvalidSupplyDate(t *testing.T) {
	env := setupReorder(t)
	defer env.Cleanup()

	_, itemIDs, userID := seedReorderScenario(t, env.Pool, []string{"A"})
	addSupply(t, env.Pool, itemIDs[0], reorderSourceDate, 5, 5, reorderSourceCutoff)

	src := placeSourceOrder(t, env, userID, []order.PlaceItem{
		{MenuItemID: itemIDs[0], Qty: 1},
	})

	_, err := env.Reorder.Reorder(context.Background(), order.ReorderInput{
		UserID:        userID,
		SourceOrderID: src.ID,
		SupplyDate:    "not-a-date",
		Plant:         reorderTestPlant,
	})
	assert.Error(t, err)
}
