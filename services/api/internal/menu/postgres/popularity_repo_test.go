package postgres_test

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Agentic-Build/corporate-catering-system/services/api/internal/menu/postgres"
)

// --- helpers reused by popularity/affinity/recent_orders tests ---

var employeeCounter atomic.Uint64

// seedEmployeeForOrders inserts an employee user (optionally with plant). Local
// to this package to avoid colliding with seedEmployeeForFavorites.
func seedEmployeeForOrders(t *testing.T, pool *pgxpool.Pool, plant string) string {
	t.Helper()
	n := employeeCounter.Add(1)
	email := fmt.Sprintf("home-emp-%d@test.com", n)
	name := fmt.Sprintf("home-emp-%d", n)
	var id string
	if plant == "" {
		require.NoError(t, pool.QueryRow(context.Background(), `
INSERT INTO "user" (primary_email, display_name, role, status)
VALUES ($1,$2,'employee','active') RETURNING id`, email, name).Scan(&id))
		return id
	}
	require.NoError(t, pool.QueryRow(context.Background(), `
INSERT INTO "user" (primary_email, display_name, role, status, plant)
VALUES ($1,$2,'employee','active',$3) RETURNING id`, email, name, plant).Scan(&id))
	return id
}

// seedConfirmedOrder inserts a confirmed-equivalent order (status=placed) with
// one order_item row, then optionally promotes the order to a richer status.
// For popularity/affinity tests we only need the join to fire; the status
// must be in {confirmed,ready,picked_up}. The schema has no 'confirmed' enum
// member — the closest counterparts are placed→ready→picked_up. The
// PlantPopularity query uses ('confirmed','ready','picked_up'); 'placed' is
// the pre-cutoff state and is NOT counted. So we use 'ready' to seed.
func seedConfirmedOrder(
	t *testing.T,
	pool *pgxpool.Pool,
	userID, vendorID, plant string,
	supplyDate time.Time,
	items map[string]int, // menu_item_id → qty
	status string,
) string {
	t.Helper()
	ctx := context.Background()
	secret := make([]byte, 32)
	for i := range secret {
		secret[i] = 0xab
	}
	var orderID string
	require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO "order"
  (user_id, vendor_id, plant, supply_date, status, total_price_minor, cutoff_at, totp_secret, placed_at, ready_at)
VALUES ($1,$2,$3,$4,$5::order_status,0,$6,$7,$6,$6)
RETURNING id`,
		userID, vendorID, plant, supplyDate, status, supplyDate.Add(10*time.Hour), secret,
	).Scan(&orderID))
	for itemID, qty := range items {
		_, err := pool.Exec(ctx, `
INSERT INTO order_item (order_id, menu_item_id, qty, unit_price_minor)
VALUES ($1,$2,$3,0)`, orderID, itemID, qty)
		require.NoError(t, err)
	}
	return orderID
}

func TestPopularityRepo_AggregatesQtyByItemForPlantAndDay(t *testing.T) {
	pool, cleanup := setupPostgres(t)
	defer cleanup()
	ctx := context.Background()
	plant := "F12B-3F"

	vendor := seedApprovedVendor(t, pool, "pop-v")
	item1 := seedActiveMenuItem(t, pool, vendor, "雞腿便當", 12000)
	item2 := seedActiveMenuItem(t, pool, vendor, "豬排便當", 13000)

	day := time.Date(2026, 5, 15, 0, 0, 0, 0, time.UTC)

	// 3 employees ordering 5 items each across two menu_items.
	e1 := seedEmployeeForOrders(t, pool, plant)
	e2 := seedEmployeeForOrders(t, pool, plant)
	e3 := seedEmployeeForOrders(t, pool, plant)

	// e1: item1 x2, item2 x3
	seedConfirmedOrder(t, pool, e1, vendor, plant, day, map[string]int{item1: 2, item2: 3}, "ready")
	// e2: item1 x1, item2 x4
	seedConfirmedOrder(t, pool, e2, vendor, plant, day, map[string]int{item1: 1, item2: 4}, "picked_up")
	// e3: item1 x5
	seedConfirmedOrder(t, pool, e3, vendor, plant, day, map[string]int{item1: 5}, "ready")

	repo := postgres.NewPopularityRepo(pool)
	got, err := repo.PlantPopularity(ctx, plant, day)
	require.NoError(t, err)

	// item1: 2+1+5 = 8, item2: 3+4 = 7.
	require.Contains(t, got, item1)
	require.Contains(t, got, item2)
	assert.InDelta(t, 8.0, got[item1], 1e-9)
	assert.InDelta(t, 7.0, got[item2], 1e-9)
}

func TestPopularityRepo_ExcludesOtherPlantAndOtherDay(t *testing.T) {
	pool, cleanup := setupPostgres(t)
	defer cleanup()
	ctx := context.Background()

	vendor := seedApprovedVendor(t, pool, "pop-x")
	item := seedActiveMenuItem(t, pool, vendor, "排骨便當", 12000)
	day := time.Date(2026, 5, 15, 0, 0, 0, 0, time.UTC)
	otherPlant := "F12B-4F"
	otherDay := day.AddDate(0, 0, 1)

	e1 := seedEmployeeForOrders(t, pool, "F12B-3F")
	// Same item, but at other plant → excluded
	seedConfirmedOrder(t, pool, e1, vendor, otherPlant, day, map[string]int{item: 9}, "ready")
	// Same item, target plant, but other day → excluded
	seedConfirmedOrder(t, pool, e1, vendor, "F12B-3F", otherDay, map[string]int{item: 9}, "ready")

	repo := postgres.NewPopularityRepo(pool)
	got, err := repo.PlantPopularity(ctx, "F12B-3F", day)
	require.NoError(t, err)
	assert.Empty(t, got)
}

func TestPopularityRepo_ExcludesNonContributingStatuses(t *testing.T) {
	pool, cleanup := setupPostgres(t)
	defer cleanup()
	ctx := context.Background()
	plant := "F12B-3F"

	vendor := seedApprovedVendor(t, pool, "pop-st")
	item := seedActiveMenuItem(t, pool, vendor, "便當", 12000)
	day := time.Date(2026, 5, 15, 0, 0, 0, 0, time.UTC)

	e1 := seedEmployeeForOrders(t, pool, plant)
	// 'placed' is pre-cutoff and should NOT count toward popularity.
	seedConfirmedOrder(t, pool, e1, vendor, plant, day, map[string]int{item: 4}, "placed")
	// 'cancelled' should not count either.
	seedConfirmedOrder(t, pool, e1, vendor, plant, day, map[string]int{item: 5}, "cancelled")
	// 'no_show' should not count (the user did not pick up).
	seedConfirmedOrder(t, pool, e1, vendor, plant, day, map[string]int{item: 6}, "no_show")

	repo := postgres.NewPopularityRepo(pool)
	got, err := repo.PlantPopularity(ctx, plant, day)
	require.NoError(t, err)
	assert.Empty(t, got)
}

func TestAffinityRepo_CountsOrdersPerVendorForUserInLast30Days(t *testing.T) {
	pool, cleanup := setupPostgres(t)
	defer cleanup()
	ctx := context.Background()
	plant := "F12B-3F"

	vendorA := seedApprovedVendor(t, pool, "aff-a")
	vendorB := seedApprovedVendor(t, pool, "aff-b")
	itemA := seedActiveMenuItem(t, pool, vendorA, "A 排骨", 12000)
	itemB := seedActiveMenuItem(t, pool, vendorB, "B 雞腿", 12500)

	user := seedEmployeeForOrders(t, pool, plant)
	// 3 orders vendor A, 1 order vendor B in the last 30 days (use a date inside the window).
	today := time.Now().UTC().Truncate(24 * time.Hour)
	for i := 0; i < 3; i++ {
		seedConfirmedOrder(t, pool, user, vendorA, plant, today.AddDate(0, 0, -i), map[string]int{itemA: 1}, "ready")
	}
	seedConfirmedOrder(t, pool, user, vendorB, plant, today.AddDate(0, 0, -1), map[string]int{itemB: 1}, "picked_up")

	repo := postgres.NewAffinityRepo(pool)
	got, err := repo.UserVendorAffinity(ctx, user)
	require.NoError(t, err)
	assert.InDelta(t, 3.0, got[vendorA], 1e-9)
	assert.InDelta(t, 1.0, got[vendorB], 1e-9)
}

func TestAffinityRepo_ExcludesOrdersOlderThan30Days(t *testing.T) {
	pool, cleanup := setupPostgres(t)
	defer cleanup()
	ctx := context.Background()
	plant := "F12B-3F"

	vendor := seedApprovedVendor(t, pool, "aff-old")
	item := seedActiveMenuItem(t, pool, vendor, "便當", 12000)
	user := seedEmployeeForOrders(t, pool, plant)
	old := time.Now().UTC().Truncate(24*time.Hour).AddDate(0, 0, -60)
	seedConfirmedOrder(t, pool, user, vendor, plant, old, map[string]int{item: 1}, "ready")

	repo := postgres.NewAffinityRepo(pool)
	got, err := repo.UserVendorAffinity(ctx, user)
	require.NoError(t, err)
	assert.Empty(t, got)
}

func TestMetaRepo_FetchByIDsReturnsActiveItemsOnly(t *testing.T) {
	pool, cleanup := setupPostgres(t)
	defer cleanup()
	ctx := context.Background()

	vendorID := seedApprovedVendor(t, pool, "meta-v")
	active := seedActiveMenuItem(t, pool, vendorID, "雞腿便當", 12000)
	archived := seedActiveMenuItem(t, pool, vendorID, "已下架", 10000)
	// Archive it.
	_, err := pool.Exec(ctx, `UPDATE menu_item SET status='archived', archived_at=now() WHERE id=$1`, archived)
	require.NoError(t, err)

	repo := postgres.NewPopularityRepo(pool)
	metas, err := repo.MetaByIDs(ctx, []string{active, archived, "00000000-0000-0000-0000-000000000000"})
	require.NoError(t, err)
	require.Len(t, metas, 1)
	assert.Equal(t, active, metas[0].ID)
	assert.Equal(t, "雞腿便當", metas[0].Name)
	assert.Equal(t, int64(12000), metas[0].UnitPrice)
	assert.Equal(t, vendorID, metas[0].VendorID)
}

func TestCutoffPassed_TrueWhenAllSuppliesPastCutoff(t *testing.T) {
	pool, cleanup := setupPostgres(t)
	defer cleanup()
	ctx := context.Background()
	plant := "F12B-3F"

	vendor := seedApprovedVendor(t, pool, "cut-v")
	seedPlantMapping(t, pool, vendor, plant)
	item1 := seedActiveMenuItem(t, pool, vendor, "便當 A", 12000)
	item2 := seedActiveMenuItem(t, pool, vendor, "便當 B", 12000)
	day := time.Date(2026, 5, 15, 0, 0, 0, 0, time.UTC)
	// Helper inserts cutoff_at = day + 10h.
	seedMealSupply(t, pool, item1, day, 50, 30)
	seedMealSupply(t, pool, item2, day, 20, 20)

	repo := postgres.NewPopularityRepo(pool)
	// now = day + 11h → all cutoffs (day + 10h) have passed.
	passed, err := repo.AllCutoffsPassed(ctx, plant, day, day.Add(11*time.Hour))
	require.NoError(t, err)
	assert.True(t, passed)

	// now = day + 9h → not yet
	passed, err = repo.AllCutoffsPassed(ctx, plant, day, day.Add(9*time.Hour))
	require.NoError(t, err)
	assert.False(t, passed)
}

func TestCutoffPassed_FalseWhenNoSuppliesExist(t *testing.T) {
	// No menu rows for the plant + day at all → "all passed" is false (there is
	// nothing to push us to tomorrow; the caller falls through to "show today").
	pool, cleanup := setupPostgres(t)
	defer cleanup()
	ctx := context.Background()

	repo := postgres.NewPopularityRepo(pool)
	passed, err := repo.AllCutoffsPassed(ctx, "F12B-3F",
		time.Date(2026, 5, 15, 0, 0, 0, 0, time.UTC),
		time.Date(2026, 5, 15, 23, 0, 0, 0, time.UTC),
	)
	require.NoError(t, err)
	assert.False(t, passed)
}
