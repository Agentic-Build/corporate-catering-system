package postgres_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/takalawang/corporate-catering-system/services/api/internal/menu"
	"github.com/takalawang/corporate-catering-system/services/api/internal/menu/postgres"
)

func TestItemRepo_CreateGetUpdate(t *testing.T) {
	pool, cleanup := setupPostgres(t)
	defer cleanup()
	vendorID := seedApprovedVendor(t, pool)
	repo := postgres.NewItemRepo(pool)
	ctx := context.Background()

	item := &menu.Item{
		VendorID:    vendorID,
		Name:        "雞腿便當",
		Description: "招牌雞腿",
		PriceMinor:  12000,
		Tags:        []string{"招牌", "雞肉"},
		Badges:      []string{"熱門"},
		Status:      menu.ItemStatusDraft,
	}
	require.NoError(t, repo.Create(ctx, item))
	require.NotEmpty(t, item.ID)
	require.False(t, item.CreatedAt.IsZero())

	got, err := repo.GetByID(ctx, item.ID)
	require.NoError(t, err)
	assert.Equal(t, "雞腿便當", got.Name)
	assert.Equal(t, int64(12000), got.PriceMinor)
	assert.Equal(t, []string{"招牌", "雞肉"}, got.Tags)
	assert.Equal(t, []string{"熱門"}, got.Badges)
	assert.Equal(t, menu.ItemStatusDraft, got.Status)

	// Update
	got.Name = "豬排便當"
	got.PriceMinor = 13000
	got.Tags = []string{"豬肉"}
	require.NoError(t, repo.Update(ctx, got))

	got2, err := repo.GetByID(ctx, item.ID)
	require.NoError(t, err)
	assert.Equal(t, "豬排便當", got2.Name)
	assert.Equal(t, int64(13000), got2.PriceMinor)
	assert.Equal(t, []string{"豬肉"}, got2.Tags)
}

func TestItemRepo_GetByID_NotFound(t *testing.T) {
	pool, cleanup := setupPostgres(t)
	defer cleanup()
	repo := postgres.NewItemRepo(pool)
	_, err := repo.GetByID(context.Background(), "00000000-0000-0000-0000-000000000000")
	assert.ErrorIs(t, err, menu.ErrItemNotFound)
}

func TestItemRepo_SetStatusAndListFilter(t *testing.T) {
	pool, cleanup := setupPostgres(t)
	defer cleanup()
	vendorID := seedApprovedVendor(t, pool)
	repo := postgres.NewItemRepo(pool)
	ctx := context.Background()

	a := &menu.Item{VendorID: vendorID, Name: "A", PriceMinor: 100, Status: menu.ItemStatusDraft}
	b := &menu.Item{VendorID: vendorID, Name: "B", PriceMinor: 200, Status: menu.ItemStatusDraft}
	c := &menu.Item{VendorID: vendorID, Name: "C", PriceMinor: 300, Status: menu.ItemStatusDraft}
	require.NoError(t, repo.Create(ctx, a))
	require.NoError(t, repo.Create(ctx, b))
	require.NoError(t, repo.Create(ctx, c))

	require.NoError(t, repo.SetStatus(ctx, a.ID, menu.ItemStatusActive))
	require.NoError(t, repo.SetStatus(ctx, c.ID, menu.ItemStatusArchived))

	// includeArchived=false: only A (active) + B (draft)
	listed, err := repo.ListByVendor(ctx, vendorID, false)
	require.NoError(t, err)
	require.Len(t, listed, 2)
	names := []string{listed[0].Item.Name, listed[1].Item.Name}
	assert.Contains(t, names, "A")
	assert.Contains(t, names, "B")
	assert.NotContains(t, names, "C")

	// includeArchived=true: all 3
	all, err := repo.ListByVendor(ctx, vendorID, true)
	require.NoError(t, err)
	require.Len(t, all, 3)

	// Verify archived_at was set
	got, err := repo.GetByID(ctx, c.ID)
	require.NoError(t, err)
	require.NotNil(t, got.ArchivedAt)
}

func TestItemRepo_ListByVendor_LastUsedAndTotalSold(t *testing.T) {
	pool, cleanup := setupPostgres(t)
	defer cleanup()
	repo := postgres.NewItemRepo(pool)
	ctx := context.Background()

	vendorID := seedApprovedVendor(t, pool, "stats-v")
	plant := "F12B-3F"

	// Item with two supply rows on different dates + picked-up orders.
	withStats := &menu.Item{VendorID: vendorID, Name: "雞腿便當", PriceMinor: 12000, Status: menu.ItemStatusActive}
	require.NoError(t, repo.Create(ctx, withStats))
	require.NoError(t, repo.SetStatus(ctx, withStats.ID, menu.ItemStatusActive))

	earlier := time.Date(2026, 5, 10, 0, 0, 0, 0, time.UTC)
	later := time.Date(2026, 5, 14, 0, 0, 0, 0, time.UTC)
	seedMealSupply(t, pool, withStats.ID, earlier, 50, 50)
	seedMealSupply(t, pool, withStats.ID, later, 40, 40)

	emp := seedEmployeeForOrders(t, pool, plant)
	// Two picked_up orders: qty 3 + qty 4 = 7.
	seedConfirmedOrder(t, pool, emp, vendorID, plant, earlier, map[string]int{withStats.ID: 3}, "picked_up")
	seedConfirmedOrder(t, pool, emp, vendorID, plant, later, map[string]int{withStats.ID: 4}, "picked_up")
	// A non-picked-up order (status=ready) that must be EXCLUDED from total_sold.
	seedConfirmedOrder(t, pool, emp, vendorID, plant, later, map[string]int{withStats.ID: 99}, "ready")

	// Item that was never scheduled and never ordered.
	noStats := &menu.Item{VendorID: vendorID, Name: "新品便當", PriceMinor: 9000, Status: menu.ItemStatusDraft}
	require.NoError(t, repo.Create(ctx, noStats))

	rows, err := repo.ListByVendor(ctx, vendorID, false)
	require.NoError(t, err)
	require.Len(t, rows, 2)

	byID := make(map[string]*menu.MerchantItemRow, len(rows))
	for _, r := range rows {
		byID[r.Item.ID] = r
	}

	got := byID[withStats.ID]
	require.NotNil(t, got)
	require.NotNil(t, got.LastUsed, "last_used should be the most recent supply_date")
	assert.Equal(t, later, got.LastUsed.UTC())
	assert.Equal(t, 7, got.TotalSold, "total_sold should sum qty over picked_up orders only")

	never := byID[noStats.ID]
	require.NotNil(t, never)
	assert.Nil(t, never.LastUsed, "last_used should be nil when never scheduled")
	assert.Equal(t, 0, never.TotalSold, "total_sold should be 0 when never sold")
}

func TestItemRepo_ListActiveByPlant(t *testing.T) {
	pool, cleanup := setupPostgres(t)
	defer cleanup()
	repo := postgres.NewItemRepo(pool)
	ctx := context.Background()

	vendorA := seedApprovedVendor(t, pool, "a")
	vendorB := seedApprovedVendor(t, pool, "b")
	seedPlantMapping(t, pool, vendorA, "F12B-3F")
	// vendorB is NOT mapped to F12B-3F

	// Set vendor A display_name to be deterministic
	_, err := pool.Exec(ctx, `UPDATE vendor SET display_name='AA Foods' WHERE id=$1`, vendorA)
	require.NoError(t, err)

	day := time.Date(2026, 5, 13, 0, 0, 0, 0, time.UTC)

	// vendor A: active item with supply
	a1 := &menu.Item{VendorID: vendorA, Name: "雞腿", PriceMinor: 12000, Status: menu.ItemStatusActive}
	require.NoError(t, repo.Create(ctx, a1))
	require.NoError(t, repo.SetStatus(ctx, a1.ID, menu.ItemStatusActive))
	seedMealSupply(t, pool, a1.ID, day, 50, 30)

	// vendor A: archived item with supply -> excluded (item not active)
	a2 := &menu.Item{VendorID: vendorA, Name: "已下架", PriceMinor: 10000, Status: menu.ItemStatusDraft}
	require.NoError(t, repo.Create(ctx, a2))
	require.NoError(t, repo.SetStatus(ctx, a2.ID, menu.ItemStatusArchived))
	seedMealSupply(t, pool, a2.ID, day, 20, 20)

	// vendor B: active item with supply (but vendor not mapped to plant -> excluded)
	b1 := &menu.Item{VendorID: vendorB, Name: "Other", PriceMinor: 9000, Status: menu.ItemStatusActive}
	require.NoError(t, repo.Create(ctx, b1))
	require.NoError(t, repo.SetStatus(ctx, b1.ID, menu.ItemStatusActive))
	seedMealSupply(t, pool, b1.ID, day, 10, 10)

	rows, err := repo.ListActiveByPlant(ctx, menu.EmployeeMenuFilter{Plant: "F12B-3F", Day: day})
	require.NoError(t, err)
	require.Len(t, rows, 1)
	assert.Equal(t, a1.ID, rows[0].Item.ID)
	assert.Equal(t, "AA Foods", rows[0].VendorName)
	assert.Equal(t, 50, rows[0].Capacity)
	assert.Equal(t, 30, rows[0].Remain)
	assert.Equal(t, "11:50-12:10", rows[0].PickupWindow)
}

func TestItemRepo_ListActiveByPlant_SuspendedVendorExcluded(t *testing.T) {
	pool, cleanup := setupPostgres(t)
	defer cleanup()
	repo := postgres.NewItemRepo(pool)
	ctx := context.Background()

	suspended := seedVendorWithStatus(t, pool, "suspended", "sus")
	seedPlantMapping(t, pool, suspended, "F12B-3F")
	day := time.Date(2026, 5, 13, 0, 0, 0, 0, time.UTC)

	it := &menu.Item{VendorID: suspended, Name: "X", PriceMinor: 100, Status: menu.ItemStatusActive}
	require.NoError(t, repo.Create(ctx, it))
	require.NoError(t, repo.SetStatus(ctx, it.ID, menu.ItemStatusActive))
	seedMealSupply(t, pool, it.ID, day, 5, 5)

	rows, err := repo.ListActiveByPlant(ctx, menu.EmployeeMenuFilter{Plant: "F12B-3F", Day: day})
	require.NoError(t, err)
	assert.Empty(t, rows)
}

func TestItemRepo_ListActiveByPlant_NoSupplyExcluded(t *testing.T) {
	pool, cleanup := setupPostgres(t)
	defer cleanup()
	repo := postgres.NewItemRepo(pool)
	ctx := context.Background()

	vendorID := seedApprovedVendor(t, pool)
	seedPlantMapping(t, pool, vendorID, "F12B-3F")
	day := time.Date(2026, 5, 13, 0, 0, 0, 0, time.UTC)

	// Active item but no supply for the day -> excluded
	it := &menu.Item{VendorID: vendorID, Name: "NoSupply", PriceMinor: 100, Status: menu.ItemStatusActive}
	require.NoError(t, repo.Create(ctx, it))
	require.NoError(t, repo.SetStatus(ctx, it.ID, menu.ItemStatusActive))

	// Supply exists for a different day
	other := day.AddDate(0, 0, 1)
	seedMealSupply(t, pool, it.ID, other, 5, 5)

	rows, err := repo.ListActiveByPlant(ctx, menu.EmployeeMenuFilter{Plant: "F12B-3F", Day: day})
	require.NoError(t, err)
	assert.Empty(t, rows)
}
