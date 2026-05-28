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

	"github.com/Agentic-Build/corporate-catering-system/services/api/internal/menu"
	"github.com/Agentic-Build/corporate-catering-system/services/api/internal/menu/postgres"
)

var favEmployeeCounter atomic.Uint64

// seedEmployeeForFavorites inserts an employee user with the given plant (or no plant when "").
func seedEmployeeForFavorites(t *testing.T, pool *pgxpool.Pool, plant string) string {
	t.Helper()
	n := favEmployeeCounter.Add(1)
	email := fmt.Sprintf("fav-emp-%d@test.com", n)
	name := fmt.Sprintf("fav-emp-%d", n)
	var id string
	if plant == "" {
		err := pool.QueryRow(context.Background(), `
INSERT INTO "user" (primary_email, display_name, role, status)
VALUES ($1, $2, 'employee', 'active')
RETURNING id`, email, name).Scan(&id)
		require.NoError(t, err)
		return id
	}
	err := pool.QueryRow(context.Background(), `
INSERT INTO "user" (primary_email, display_name, role, status, plant)
VALUES ($1, $2, 'employee', 'active', $3)
RETURNING id`, email, name, plant).Scan(&id)
	require.NoError(t, err)
	return id
}

// seedActiveMenuItem creates an active menu item for the vendor and returns its id.
func seedActiveMenuItem(t *testing.T, pool *pgxpool.Pool, vendorID, name string, priceMinor int64) string {
	t.Helper()
	repo := postgres.NewItemRepo(pool)
	it := &menu.Item{VendorID: vendorID, Name: name, PriceMinor: priceMinor, Status: menu.ItemStatusActive}
	require.NoError(t, repo.Create(context.Background(), it))
	require.NoError(t, repo.SetStatus(context.Background(), it.ID, menu.ItemStatusActive))
	return it.ID
}

func TestFavoriteRepo_AddIsIdempotent(t *testing.T) {
	pool, cleanup := setupPostgres(t)
	defer cleanup()
	ctx := context.Background()

	userID := seedEmployeeForFavorites(t, pool, "F12B-3F")
	vendorID := seedApprovedVendor(t, pool, "fav-idem")
	itemID := seedActiveMenuItem(t, pool, vendorID, "雞腿便當", 12000)

	repo := postgres.NewFavoriteRepo(pool)
	require.NoError(t, repo.Add(ctx, userID, itemID))
	// Second call must NOT error (ON CONFLICT DO NOTHING).
	require.NoError(t, repo.Add(ctx, userID, itemID))

	// Verify only a single row exists.
	var count int
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM favorite_item WHERE user_id=$1 AND menu_item_id=$2`,
		userID, itemID).Scan(&count))
	assert.Equal(t, 1, count)
}

func TestFavoriteRepo_RemoveIsIdempotent(t *testing.T) {
	pool, cleanup := setupPostgres(t)
	defer cleanup()
	ctx := context.Background()

	userID := seedEmployeeForFavorites(t, pool, "F12B-3F")
	vendorID := seedApprovedVendor(t, pool, "fav-rm")
	itemID := seedActiveMenuItem(t, pool, vendorID, "豬排便當", 13000)

	repo := postgres.NewFavoriteRepo(pool)
	// Remove without prior add: must not error.
	require.NoError(t, repo.Remove(ctx, userID, itemID))

	// Add then remove twice.
	require.NoError(t, repo.Add(ctx, userID, itemID))
	require.NoError(t, repo.Remove(ctx, userID, itemID))
	require.NoError(t, repo.Remove(ctx, userID, itemID))

	var count int
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM favorite_item WHERE user_id=$1 AND menu_item_id=$2`,
		userID, itemID).Scan(&count))
	assert.Equal(t, 0, count)
}

func TestFavoriteRepo_ListByUser_AvailabilityFlag(t *testing.T) {
	pool, cleanup := setupPostgres(t)
	defer cleanup()
	ctx := context.Background()

	plant := "F12B-3F"
	userID := seedEmployeeForFavorites(t, pool, plant)
	vendorID := seedApprovedVendor(t, pool, "fav-avail")
	seedPlantMapping(t, pool, vendorID, plant)

	available := seedActiveMenuItem(t, pool, vendorID, "今日有供應", 12000)
	noSupply := seedActiveMenuItem(t, pool, vendorID, "今日無供應", 11000)

	day := time.Date(2026, 5, 15, 0, 0, 0, 0, time.UTC)
	seedMealSupply(t, pool, available, day, 50, 30)
	// noSupply has no meal_supply row → available_today=false.

	repo := postgres.NewFavoriteRepo(pool)
	require.NoError(t, repo.Add(ctx, userID, available))
	require.NoError(t, repo.Add(ctx, userID, noSupply))

	chips, nextCursor, err := repo.ListByUser(ctx, userID, "2026-05-15", plant, 10, nil)
	require.NoError(t, err)
	require.Len(t, chips, 2)
	assert.Nil(t, nextCursor)

	// Build map by id to assert independent of order.
	byID := map[string]menu.FavoriteChip{}
	for _, c := range chips {
		byID[c.MenuItemID] = c
	}
	require.Contains(t, byID, available)
	require.Contains(t, byID, noSupply)
	assert.True(t, byID[available].AvailableToday, "item with meal_supply should be available")
	assert.False(t, byID[noSupply].AvailableToday, "item without meal_supply should NOT be available")
	assert.Equal(t, "今日有供應", byID[available].Name)
	assert.Equal(t, int64(12000), byID[available].UnitPrice)
	assert.Equal(t, vendorID, byID[available].VendorID)
}

func TestFavoriteRepo_ListByUser_VendorNotMappedToPlantIsUnavailable(t *testing.T) {
	pool, cleanup := setupPostgres(t)
	defer cleanup()
	ctx := context.Background()

	plant := "F12B-3F"
	userID := seedEmployeeForFavorites(t, pool, plant)
	vendorID := seedApprovedVendor(t, pool, "fav-noplant")
	// Intentionally no plant mapping for vendor.
	itemID := seedActiveMenuItem(t, pool, vendorID, "未授權門市", 10000)

	day := time.Date(2026, 5, 15, 0, 0, 0, 0, time.UTC)
	seedMealSupply(t, pool, itemID, day, 10, 10)

	repo := postgres.NewFavoriteRepo(pool)
	require.NoError(t, repo.Add(ctx, userID, itemID))

	chips, _, err := repo.ListByUser(ctx, userID, "2026-05-15", plant, 10, nil)
	require.NoError(t, err)
	require.Len(t, chips, 1)
	assert.False(t, chips[0].AvailableToday,
		"meal_supply exists but vendor not mapped to plant → not available")
}

func TestFavoriteRepo_ListByUser_ArchivedItemExcluded(t *testing.T) {
	pool, cleanup := setupPostgres(t)
	defer cleanup()
	ctx := context.Background()

	plant := "F12B-3F"
	userID := seedEmployeeForFavorites(t, pool, plant)
	vendorID := seedApprovedVendor(t, pool, "fav-arch")
	seedPlantMapping(t, pool, vendorID, plant)

	keepID := seedActiveMenuItem(t, pool, vendorID, "保留", 9000)
	archID := seedActiveMenuItem(t, pool, vendorID, "已下架", 9500)

	repo := postgres.NewFavoriteRepo(pool)
	require.NoError(t, repo.Add(ctx, userID, keepID))
	require.NoError(t, repo.Add(ctx, userID, archID))

	// Archive after favoriting.
	itemRepo := postgres.NewItemRepo(pool)
	require.NoError(t, itemRepo.SetStatus(ctx, archID, menu.ItemStatusArchived))

	chips, _, err := repo.ListByUser(ctx, userID, "2026-05-15", plant, 10, nil)
	require.NoError(t, err)
	require.Len(t, chips, 1)
	assert.Equal(t, keepID, chips[0].MenuItemID)
}

func TestFavoriteRepo_ListByUser_NewestFirstAndCursor(t *testing.T) {
	pool, cleanup := setupPostgres(t)
	defer cleanup()
	ctx := context.Background()

	plant := "F12B-3F"
	userID := seedEmployeeForFavorites(t, pool, plant)
	vendorID := seedApprovedVendor(t, pool, "fav-page")
	seedPlantMapping(t, pool, vendorID, plant)

	// Seed three items + three favorites with distinct created_at values.
	idA := seedActiveMenuItem(t, pool, vendorID, "A", 100)
	idB := seedActiveMenuItem(t, pool, vendorID, "B", 200)
	idC := seedActiveMenuItem(t, pool, vendorID, "C", 300)

	t0 := time.Date(2026, 5, 10, 12, 0, 0, 0, time.UTC)
	for _, x := range []struct {
		item string
		when time.Time
	}{
		{idA, t0},
		{idB, t0.Add(1 * time.Minute)},
		{idC, t0.Add(2 * time.Minute)},
	} {
		_, err := pool.Exec(ctx,
			`INSERT INTO favorite_item (user_id, menu_item_id, created_at) VALUES ($1, $2, $3)`,
			userID, x.item, x.when)
		require.NoError(t, err)
	}

	repo := postgres.NewFavoriteRepo(pool)

	// Page 1: limit 2 → expect C, B (newest first).
	page1, cur1, err := repo.ListByUser(ctx, userID, "2026-05-15", plant, 2, nil)
	require.NoError(t, err)
	require.Len(t, page1, 2)
	assert.Equal(t, idC, page1[0].MenuItemID)
	assert.Equal(t, idB, page1[1].MenuItemID)
	require.NotNil(t, cur1, "next_cursor should be non-nil when more rows exist")
	assert.True(t, cur1.Equal(page1[1].CreatedAt))

	// Page 2: cursor = last created_at → expect A.
	page2, cur2, err := repo.ListByUser(ctx, userID, "2026-05-15", plant, 2, cur1)
	require.NoError(t, err)
	require.Len(t, page2, 1)
	assert.Equal(t, idA, page2[0].MenuItemID)
	assert.Nil(t, cur2, "no more pages → next_cursor nil")
}

func TestFavoriteRepo_ListByUser_LimitClamping(t *testing.T) {
	pool, cleanup := setupPostgres(t)
	defer cleanup()
	ctx := context.Background()

	plant := "F12B-3F"
	userID := seedEmployeeForFavorites(t, pool, plant)
	vendorID := seedApprovedVendor(t, pool, "fav-clamp")
	seedPlantMapping(t, pool, vendorID, plant)
	itemID := seedActiveMenuItem(t, pool, vendorID, "X", 100)

	repo := postgres.NewFavoriteRepo(pool)
	require.NoError(t, repo.Add(ctx, userID, itemID))

	// limit <= 0 should clamp to 1+ (return the item; not error).
	chips, _, err := repo.ListByUser(ctx, userID, "2026-05-15", plant, 0, nil)
	require.NoError(t, err)
	require.Len(t, chips, 1)

	// limit > 50 should not error (clamp to 50; only 1 row available).
	chips, _, err = repo.ListByUser(ctx, userID, "2026-05-15", plant, 9999, nil)
	require.NoError(t, err)
	require.Len(t, chips, 1)
}

func TestFavoriteRepo_ListByUser_EmptyForOtherUser(t *testing.T) {
	pool, cleanup := setupPostgres(t)
	defer cleanup()
	ctx := context.Background()

	plant := "F12B-3F"
	owner := seedEmployeeForFavorites(t, pool, plant)
	other := seedEmployeeForFavorites(t, pool, plant)
	vendorID := seedApprovedVendor(t, pool, "fav-iso")
	seedPlantMapping(t, pool, vendorID, plant)
	itemID := seedActiveMenuItem(t, pool, vendorID, "私人", 100)

	repo := postgres.NewFavoriteRepo(pool)
	require.NoError(t, repo.Add(ctx, owner, itemID))

	chips, _, err := repo.ListByUser(ctx, other, "2026-05-15", plant, 10, nil)
	require.NoError(t, err)
	assert.Empty(t, chips, "other user should not see owner's favorites")
}
