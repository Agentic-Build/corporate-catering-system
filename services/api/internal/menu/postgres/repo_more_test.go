package postgres_test

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Agentic-Build/corporate-catering-system/services/api/internal/menu"
	"github.com/Agentic-Build/corporate-catering-system/services/api/internal/menu/postgres"
)

// closedPool spins up a real Postgres, runs migrations, then closes the pool so
// every subsequent pool operation (Query/QueryRow/Begin) fails deterministically
// with a "closed pool" error. This drives the query/exec error-return branches
// without depending on flaky timing. Mirrors order/postgres/repo_errorpaths_test.go.
func closedPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	pool, cleanup := setupPostgres(t)
	t.Cleanup(cleanup)
	pool.Close()
	return pool
}

// TestRepos_QueryErrors exercises every Query/QueryRow/Begin error-return branch
// across the package's repos against a single closed pool (one container).
func TestRepos_QueryErrors(t *testing.T) {
	pool := closedPool(t)
	ctx := context.Background()
	day := time.Date(2026, 5, 14, 0, 0, 0, 0, time.UTC)

	t.Run("AffinityRepo_UserVendorAffinity", func(t *testing.T) {
		_, err := postgres.NewAffinityRepo(pool).UserVendorAffinity(ctx, "u")
		require.Error(t, err)
	})

	t.Run("CategoryRepo_GetByID_GenericError", func(t *testing.T) {
		// Closed pool is not ErrNoRows → falls through to the "category scan" wrap.
		_, err := postgres.NewCategoryRepo(pool).GetByID(ctx, "00000000-0000-0000-0000-000000000001")
		require.Error(t, err)
		assert.NotErrorIs(t, err, menu.ErrCategoryNotFound)
		assert.Contains(t, err.Error(), "category scan")
	})

	t.Run("CategoryRepo_ListByVendor", func(t *testing.T) {
		_, err := postgres.NewCategoryRepo(pool).ListByVendor(ctx, "v")
		require.Error(t, err)
	})

	t.Run("FavoriteRepo_ListByUser", func(t *testing.T) {
		_, _, err := postgres.NewFavoriteRepo(pool).ListByUser(ctx, "u", "2026-05-14", "F12B-3F", 10, nil)
		require.Error(t, err)
	})

	t.Run("ImageRepo_ListByItem", func(t *testing.T) {
		_, err := postgres.NewImageRepo(pool).ListByItem(ctx, "i")
		require.Error(t, err)
	})

	t.Run("ImageRepo_ListByItems", func(t *testing.T) {
		_, err := postgres.NewImageRepo(pool).ListByItems(ctx, []string{"i"})
		require.Error(t, err)
	})

	t.Run("ImageRepo_ReplaceForItem_BeginError", func(t *testing.T) {
		err := postgres.NewImageRepo(pool).ReplaceForItem(ctx, "i", []string{"s3://x"})
		require.Error(t, err)
	})

	t.Run("ItemRepo_GetByID_GenericError", func(t *testing.T) {
		_, err := postgres.NewItemRepo(pool).GetByID(ctx, "00000000-0000-0000-0000-000000000001")
		require.Error(t, err)
		assert.NotErrorIs(t, err, menu.ErrItemNotFound)
		assert.Contains(t, err.Error(), "item scan")
	})

	t.Run("ItemRepo_ListByVendor", func(t *testing.T) {
		_, err := postgres.NewItemRepo(pool).ListByVendor(ctx, "v", true)
		require.Error(t, err)
	})

	t.Run("ItemRepo_ListActiveByPlant", func(t *testing.T) {
		_, err := postgres.NewItemRepo(pool).ListActiveByPlant(ctx, menu.EmployeeMenuFilter{Plant: "F12B-3F", Day: day})
		require.Error(t, err)
	})

	t.Run("PopularityRepo_PlantPopularity", func(t *testing.T) {
		_, err := postgres.NewPopularityRepo(pool).PlantPopularity(ctx, "F12B-3F", day)
		require.Error(t, err)
	})

	t.Run("PopularityRepo_MetaByIDs", func(t *testing.T) {
		_, err := postgres.NewPopularityRepo(pool).MetaByIDs(ctx, []string{"i"})
		require.Error(t, err)
	})

	t.Run("PopularityRepo_AllCutoffsPassed", func(t *testing.T) {
		_, err := postgres.NewPopularityRepo(pool).AllCutoffsPassed(ctx, "F12B-3F", day, time.Now())
		require.Error(t, err)
	})
}

// TestRepos_ReachableBranches covers non-error branches that the existing tests
// never hit: defaulted fields and the fast-return / sort-fallback paths.
func TestRepos_ReachableBranches(t *testing.T) {
	pool, cleanup := setupPostgres(t)
	defer cleanup()
	ctx := context.Background()
	vendorID := seedApprovedVendor(t, pool, "reach")
	itemRepo := postgres.NewItemRepo(pool)

	t.Run("ItemRepo_Create_DefaultsStatusAndTags", func(t *testing.T) {
		// Empty Status → ItemStatusDraft; nil Tags → []string{}.
		it := &menu.Item{VendorID: vendorID, Name: "default-status", PriceMinor: 100}
		require.NoError(t, itemRepo.Create(ctx, it))
		require.Equal(t, []string{}, it.Tags)
		require.Equal(t, menu.ItemStatusDraft, it.Status)

		got, err := itemRepo.GetByID(ctx, it.ID)
		require.NoError(t, err)
		assert.Equal(t, menu.ItemStatusDraft, got.Status)
		assert.Equal(t, []string{}, got.Tags)
	})

	t.Run("ItemRepo_Update_NilTagsDefaultsEmpty", func(t *testing.T) {
		it := &menu.Item{VendorID: vendorID, Name: "nil-tags", PriceMinor: 100, Tags: []string{"x"}}
		require.NoError(t, itemRepo.Create(ctx, it))

		it.Name = "nil-tags-updated"
		it.Tags = nil // triggers the nil→[]string{} default branch in Update
		require.NoError(t, itemRepo.Update(ctx, it))

		got, err := itemRepo.GetByID(ctx, it.ID)
		require.NoError(t, err)
		assert.Equal(t, "nil-tags-updated", got.Name)
		assert.Equal(t, []string{}, got.Tags)
	})

	t.Run("ItemRepo_ListActiveByPlant_UnknownSortFallsBackToDefault", func(t *testing.T) {
		vid := seedApprovedVendor(t, pool, "reach-sort")
		plant := "REACH-SORT"
		seedPlantMapping(t, pool, vid, plant)
		day := time.Date(2026, 5, 14, 0, 0, 0, 0, time.UTC)

		a := &menu.Item{VendorID: vid, Name: "ZZZ", PriceMinor: 100, Status: menu.ItemStatusActive}
		b := &menu.Item{VendorID: vid, Name: "AAA", PriceMinor: 100, Status: menu.ItemStatusActive}
		require.NoError(t, itemRepo.Create(ctx, a))
		require.NoError(t, itemRepo.Create(ctx, b))
		require.NoError(t, itemRepo.SetStatus(ctx, a.ID, menu.ItemStatusActive))
		require.NoError(t, itemRepo.SetStatus(ctx, b.ID, menu.ItemStatusActive))
		seedMealSupply(t, pool, a.ID, day, 10, 5)
		seedMealSupply(t, pool, b.ID, day, 10, 5)

		// An unrecognised Sort value falls back to the default (vendor, then name).
		rows, err := itemRepo.ListActiveByPlant(ctx, menu.EmployeeMenuFilter{
			Plant: plant, Day: day, Sort: menu.EmployeeMenuSort("totally-unknown"),
		})
		require.NoError(t, err)
		require.Len(t, rows, 2)
		// Default order within a single vendor is by mi.name → AAA before ZZZ.
		assert.Equal(t, "AAA", rows[0].Item.Name)
		assert.Equal(t, "ZZZ", rows[1].Item.Name)
	})

	t.Run("PopularityRepo_MetaByIDs_EmptyReturnsNil", func(t *testing.T) {
		out, err := postgres.NewPopularityRepo(pool).MetaByIDs(ctx, nil)
		require.NoError(t, err)
		assert.Nil(t, out)

		out, err = postgres.NewPopularityRepo(pool).MetaByIDs(ctx, []string{})
		require.NoError(t, err)
		assert.Nil(t, out)
	})
}

// TestRepos_ScanErrors drives the per-row Scan / rows.Err error branches. The
// technique: seed a real row so rows.Next() returns true, then make a scanned,
// value-typed column NULL (NULL cannot scan into a non-pointer destination) so
// the in-loop Scan fails. Schema mutations are reverted by t.Cleanup so subtests
// stay independent on the shared pool.
func TestRepos_ScanErrors(t *testing.T) {
	pool, cleanup := setupPostgres(t)
	defer cleanup()
	ctx := context.Background()

	dropNotNullThenNil := func(t *testing.T, table, col string) {
		t.Helper()
		_, err := pool.Exec(ctx, `ALTER TABLE `+table+` ALTER COLUMN `+col+` DROP NOT NULL`)
		require.NoError(t, err)
		_, err = pool.Exec(ctx, `UPDATE `+table+` SET `+col+` = NULL`)
		require.NoError(t, err)
		t.Cleanup(func() {
			// Best-effort restore so other subtests sharing the pool are unaffected.
			_, _ = pool.Exec(ctx, `DELETE FROM `+table+` WHERE `+col+` IS NULL`)
			_, _ = pool.Exec(ctx, `ALTER TABLE `+table+` ALTER COLUMN `+col+` SET NOT NULL`)
		})
	}

	t.Run("CategoryRepo_ListByVendor_ScanError", func(t *testing.T) {
		vid := seedApprovedVendor(t, pool, "scan-cat")
		_, err := pool.Exec(ctx, `INSERT INTO menu_category (vendor_id, name, sort_order) VALUES ($1,'c',0)`, vid)
		require.NoError(t, err)
		dropNotNullThenNil(t, "menu_category", "sort_order") // sort_order scans into int
		_, err = postgres.NewCategoryRepo(pool).ListByVendor(ctx, vid)
		require.Error(t, err)
	})

	t.Run("ImageRepo_ListByItem_ScanError", func(t *testing.T) {
		vid := seedApprovedVendor(t, pool, "scan-img1")
		itemID := seedActiveMenuItem(t, pool, vid, "img1", 100)
		_, err := pool.Exec(ctx,
			`INSERT INTO menu_item_image (menu_item_id, blob_uri, sort_order) VALUES ($1,'s3://x',0)`, itemID)
		require.NoError(t, err)
		dropNotNullThenNil(t, "menu_item_image", "sort_order")
		_, err = postgres.NewImageRepo(pool).ListByItem(ctx, itemID)
		require.Error(t, err)
	})

	t.Run("ImageRepo_ListByItems_ScanError", func(t *testing.T) {
		vid := seedApprovedVendor(t, pool, "scan-img2")
		itemID := seedActiveMenuItem(t, pool, vid, "img2", 100)
		_, err := pool.Exec(ctx,
			`INSERT INTO menu_item_image (menu_item_id, blob_uri, sort_order) VALUES ($1,'s3://y',0)`, itemID)
		require.NoError(t, err)
		dropNotNullThenNil(t, "menu_item_image", "sort_order")
		_, err = postgres.NewImageRepo(pool).ListByItems(ctx, []string{itemID})
		require.Error(t, err)
	})

	t.Run("ItemRepo_ListByVendor_ScanError", func(t *testing.T) {
		vid := seedApprovedVendor(t, pool, "scan-item")
		_ = seedActiveMenuItem(t, pool, vid, "iv", 100)
		dropNotNullThenNil(t, "menu_item", "name") // name scans into string
		_, err := postgres.NewItemRepo(pool).ListByVendor(ctx, vid, true)
		require.Error(t, err)
	})

	t.Run("ItemRepo_ListActiveByPlant_ScanError", func(t *testing.T) {
		vid := seedApprovedVendor(t, pool, "scan-active")
		plant := "SCAN-ACTIVE"
		seedPlantMapping(t, pool, vid, plant)
		day := time.Date(2026, 5, 14, 0, 0, 0, 0, time.UTC)
		it := &menu.Item{VendorID: vid, Name: "active-scan", PriceMinor: 100, Status: menu.ItemStatusActive}
		require.NoError(t, postgres.NewItemRepo(pool).Create(ctx, it))
		require.NoError(t, postgres.NewItemRepo(pool).SetStatus(ctx, it.ID, menu.ItemStatusActive))
		seedMealSupply(t, pool, it.ID, day, 10, 5)
		// capacity scans into int; NULL breaks the in-loop Scan after Next() is true.
		dropNotNullThenNil(t, "meal_supply", "capacity")
		_, err := postgres.NewItemRepo(pool).ListActiveByPlant(ctx, menu.EmployeeMenuFilter{Plant: plant, Day: day})
		require.Error(t, err)
	})

	t.Run("PopularityRepo_MetaByIDs_ScanError", func(t *testing.T) {
		vid := seedApprovedVendor(t, pool, "scan-meta")
		itemID := seedActiveMenuItem(t, pool, vid, "meta", 100)
		dropNotNullThenNil(t, "menu_item", "name")
		_, err := postgres.NewPopularityRepo(pool).MetaByIDs(ctx, []string{itemID})
		require.Error(t, err)
	})

	t.Run("PopularityRepo_PlantPopularity_ScanError", func(t *testing.T) {
		vid := seedApprovedVendor(t, pool, "scan-pop")
		itemID := seedActiveMenuItem(t, pool, vid, "pop", 100)
		uid := seedEmployeeForOrders(t, pool, "")
		plant := "SCAN-POP"
		day := time.Date(2026, 5, 14, 0, 0, 0, 0, time.UTC)
		seedConfirmedOrder(t, pool, uid, vid, plant, day, map[string]int{itemID: 1}, "ready")
		// oi.menu_item_id scans into string; NULL it on the order_item rows.
		_, err := pool.Exec(ctx, `ALTER TABLE order_item ALTER COLUMN menu_item_id DROP NOT NULL`)
		require.NoError(t, err)
		_, err = pool.Exec(ctx, `UPDATE order_item SET menu_item_id = NULL`)
		require.NoError(t, err)
		t.Cleanup(func() {
			_, _ = pool.Exec(ctx, `DELETE FROM order_item WHERE menu_item_id IS NULL`)
			_, _ = pool.Exec(ctx, `ALTER TABLE order_item ALTER COLUMN menu_item_id SET NOT NULL`)
		})
		_, err = postgres.NewPopularityRepo(pool).PlantPopularity(ctx, plant, day)
		require.Error(t, err)
	})

	t.Run("AffinityRepo_UserVendorAffinity_ScanError", func(t *testing.T) {
		vid := seedApprovedVendor(t, pool, "scan-aff")
		itemID := seedActiveMenuItem(t, pool, vid, "aff", 100)
		uid := seedEmployeeForOrders(t, pool, "")
		plant := "SCAN-AFF"
		day := time.Now().UTC().Truncate(24 * time.Hour)
		seedConfirmedOrder(t, pool, uid, vid, plant, day, map[string]int{itemID: 1}, "ready")
		// mi.vendor_id scans into string; NULL it so the GROUP BY yields a NULL key.
		_, err := pool.Exec(ctx, `ALTER TABLE menu_item ALTER COLUMN vendor_id DROP NOT NULL`)
		require.NoError(t, err)
		_, err = pool.Exec(ctx, `UPDATE menu_item SET vendor_id = NULL WHERE id = $1`, itemID)
		require.NoError(t, err)
		t.Cleanup(func() {
			_, _ = pool.Exec(ctx, `UPDATE menu_item SET vendor_id = $1 WHERE id = $2`, vid, itemID)
			_, _ = pool.Exec(ctx, `ALTER TABLE menu_item ALTER COLUMN vendor_id SET NOT NULL`)
		})
		_, err = postgres.NewAffinityRepo(pool).UserVendorAffinity(ctx, uid)
		require.Error(t, err)
	})

	t.Run("FavoriteRepo_ListByUser_ScanError", func(t *testing.T) {
		vid := seedApprovedVendor(t, pool, "scan-fav")
		itemID := seedActiveMenuItem(t, pool, vid, "fav", 100)
		uid := seedEmployeeForOrders(t, pool, "")
		_, err := pool.Exec(ctx,
			`INSERT INTO favorite_item (user_id, menu_item_id) VALUES ($1,$2)`, uid, itemID)
		require.NoError(t, err)
		// mi.price_minor scans into int64; NULL breaks the in-loop Scan.
		_, err = pool.Exec(ctx, `ALTER TABLE menu_item ALTER COLUMN price_minor DROP NOT NULL`)
		require.NoError(t, err)
		_, err = pool.Exec(ctx, `UPDATE menu_item SET price_minor = NULL WHERE id = $1`, itemID)
		require.NoError(t, err)
		t.Cleanup(func() {
			_, _ = pool.Exec(ctx, `UPDATE menu_item SET price_minor = 100 WHERE id = $1`, itemID)
			_, _ = pool.Exec(ctx, `ALTER TABLE menu_item ALTER COLUMN price_minor SET NOT NULL`)
		})
		_, _, err = postgres.NewFavoriteRepo(pool).ListByUser(ctx, uid, "2026-05-14", "ANY", 10, nil)
		require.Error(t, err)
	})

	// ReplaceForItem runs its own tx; cover the DELETE-exec and INSERT-exec error
	// branches here on the shared pool to avoid spinning extra containers.
	t.Run("ImageRepo_ReplaceForItem_InsertExecError_DanglingItemFK", func(t *testing.T) {
		// Non-existent menu_item_id: the DELETE removes zero rows (succeeds), then
		// the INSERT violates the menu_item_id FK → the insert-exec error branch.
		err := postgres.NewImageRepo(pool).ReplaceForItem(ctx,
			"00000000-0000-0000-0000-000000000000", []string{"s3://x"})
		require.Error(t, err)
	})

	t.Run("ImageRepo_ReplaceForItem_DeleteExecError_TableRenamed", func(t *testing.T) {
		vid := seedApprovedVendor(t, pool, "replace-del")
		itemID := seedActiveMenuItem(t, pool, vid, "replace-del", 100)
		_, err := pool.Exec(ctx, `ALTER TABLE menu_item_image RENAME TO menu_item_image_hidden`)
		require.NoError(t, err)
		t.Cleanup(func() {
			_, _ = pool.Exec(ctx, `ALTER TABLE menu_item_image_hidden RENAME TO menu_item_image`)
		})
		// Begin succeeds, but the first DELETE references the now-missing table.
		err = postgres.NewImageRepo(pool).ReplaceForItem(ctx, itemID, []string{"s3://x"})
		require.Error(t, err)
	})
}
