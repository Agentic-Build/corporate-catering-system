package postgres_test

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Agentic-Build/corporate-catering-system/services/api/internal/quota"
	"github.com/Agentic-Build/corporate-catering-system/services/api/internal/quota/postgres"
)

func TestSupplyRepo_UpsertAndGet(t *testing.T) {
	pool, cleanup := setupPostgres(t)
	defer cleanup()
	vendorID := seedApprovedVendor(t, pool)
	itemID := seedActiveItem(t, pool, vendorID)
	repo := postgres.NewSupplyRepo(pool)
	ctx := context.Background()

	day := time.Now().UTC().Truncate(24 * time.Hour)
	s := &quota.Supply{
		MenuItemID:   itemID,
		SupplyDate:   day,
		Capacity:     80,
		Remain:       80,
		PickupWindow: "11:50-12:10",
		ETALabel:     "11:50-12:10",
		CutoffAt:     time.Now().Add(24 * time.Hour),
	}
	require.NoError(t, repo.Upsert(ctx, s))
	require.NotEmpty(t, s.ID)

	got, err := repo.Get(ctx, itemID, day)
	require.NoError(t, err)
	assert.Equal(t, 80, got.Capacity)
	assert.Equal(t, 80, got.Remain)

	// Upsert again with new values — same row updated
	s.Capacity = 100
	s.Remain = 100
	require.NoError(t, repo.Upsert(ctx, s))
	got, _ = repo.Get(ctx, itemID, day)
	assert.Equal(t, 100, got.Capacity)
}

func TestSupplyRepo_GetNotFound(t *testing.T) {
	pool, cleanup := setupPostgres(t)
	defer cleanup()
	vendorID := seedApprovedVendor(t, pool)
	itemID := seedActiveItem(t, pool, vendorID)
	repo := postgres.NewSupplyRepo(pool)
	_, err := repo.Get(context.Background(), itemID, time.Now().UTC().Truncate(24*time.Hour))
	assert.ErrorIs(t, err, quota.ErrSupplyNotFound)
}

func TestSupplyRepo_Decrement(t *testing.T) {
	pool, cleanup := setupPostgres(t)
	defer cleanup()
	vendorID := seedApprovedVendor(t, pool)
	itemID := seedActiveItem(t, pool, vendorID)
	repo := postgres.NewSupplyRepo(pool)
	ctx := context.Background()
	day := time.Now().UTC().Truncate(24 * time.Hour)
	require.NoError(t, repo.Upsert(ctx, &quota.Supply{
		MenuItemID: itemID, SupplyDate: day,
		Capacity: 5, Remain: 5,
		PickupWindow: "11:50-12:10", ETALabel: "11:50-12:10",
		CutoffAt: time.Now().Add(24 * time.Hour),
	}))

	// 5 successful decrements
	for i := 0; i < 5; i++ {
		newRemain, err := repo.Decrement(ctx, itemID, day, 1)
		require.NoError(t, err)
		assert.Equal(t, 4-i, newRemain)
	}
	// 6th must fail
	_, err := repo.Decrement(ctx, itemID, day, 1)
	assert.ErrorIs(t, err, quota.ErrOutOfStock)
}

func TestSupplyRepo_DecrementNonExistentSupply(t *testing.T) {
	pool, cleanup := setupPostgres(t)
	defer cleanup()
	vendorID := seedApprovedVendor(t, pool)
	itemID := seedActiveItem(t, pool, vendorID)
	repo := postgres.NewSupplyRepo(pool)
	_, err := repo.Decrement(context.Background(), itemID, time.Now().UTC().Truncate(24*time.Hour), 1)
	assert.ErrorIs(t, err, quota.ErrSupplyNotFound)
}

func TestSupplyRepo_Restore(t *testing.T) {
	pool, cleanup := setupPostgres(t)
	defer cleanup()
	vendorID := seedApprovedVendor(t, pool)
	itemID := seedActiveItem(t, pool, vendorID)
	repo := postgres.NewSupplyRepo(pool)
	ctx := context.Background()
	day := time.Now().UTC().Truncate(24 * time.Hour)
	require.NoError(t, repo.Upsert(ctx, &quota.Supply{
		MenuItemID: itemID, SupplyDate: day,
		Capacity: 10, Remain: 5,
		PickupWindow: "x", ETALabel: "x", CutoffAt: time.Now().Add(24 * time.Hour),
	}))

	require.NoError(t, repo.Restore(ctx, itemID, day, 3))
	got, _ := repo.Get(ctx, itemID, day)
	assert.Equal(t, 8, got.Remain)

	// Restore beyond capacity caps at capacity (DB CHECK forbids remain > capacity)
	require.NoError(t, repo.Restore(ctx, itemID, day, 10))
	got, _ = repo.Get(ctx, itemID, day)
	assert.Equal(t, 10, got.Remain) // capped
}

func TestSupplyRepo_ListByVendor(t *testing.T) {
	pool, cleanup := setupPostgres(t)
	defer cleanup()
	vendorID := seedApprovedVendor(t, pool)
	item1 := seedActiveItem(t, pool, vendorID)
	item2 := seedActiveItem(t, pool, vendorID)
	repo := postgres.NewSupplyRepo(pool)
	ctx := context.Background()
	day := time.Now().UTC().Truncate(24 * time.Hour)
	require.NoError(t, repo.Upsert(ctx, &quota.Supply{MenuItemID: item1, SupplyDate: day, Capacity: 50, Remain: 50, PickupWindow: "x", ETALabel: "x", CutoffAt: time.Now().Add(24 * time.Hour)}))
	require.NoError(t, repo.Upsert(ctx, &quota.Supply{MenuItemID: item2, SupplyDate: day, Capacity: 80, Remain: 80, PickupWindow: "x", ETALabel: "x", CutoffAt: time.Now().Add(24 * time.Hour)}))

	list, err := repo.ListByVendor(ctx, vendorID, day)
	require.NoError(t, err)
	assert.Len(t, list, 2)
}

func TestSupplyRepo_DecrementNoOversell_500RacersOn100Capacity(t *testing.T) {
	pool, cleanup := setupPostgres(t)
	defer cleanup()
	vendorID := seedApprovedVendor(t, pool)
	itemID := seedActiveItem(t, pool, vendorID)
	repo := postgres.NewSupplyRepo(pool)
	ctx := context.Background()

	day := time.Now().UTC().Truncate(24 * time.Hour)
	require.NoError(t, repo.Upsert(ctx, &quota.Supply{
		MenuItemID: itemID, SupplyDate: day,
		Capacity: 100, Remain: 100,
		PickupWindow: "11:50-12:10", ETALabel: "11:50-12:10",
		CutoffAt: time.Now().Add(24 * time.Hour),
	}))

	const N = 500
	var wg sync.WaitGroup
	var succeeded, outOfStock, other atomic.Int32
	for i := 0; i < N; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := repo.Decrement(ctx, itemID, day, 1)
			switch {
			case err == nil:
				succeeded.Add(1)
			case errors.Is(err, quota.ErrOutOfStock):
				outOfStock.Add(1)
			default:
				other.Add(1)
				t.Logf("unexpected error: %v", err)
			}
		}()
	}
	wg.Wait()

	assert.Equal(t, int32(100), succeeded.Load(), "exactly 100 must succeed (capacity)")
	assert.Equal(t, int32(400), outOfStock.Load(), "exactly 400 must be ErrOutOfStock")
	assert.Equal(t, int32(0), other.Load(), "no other errors expected")

	final, err := repo.Get(ctx, itemID, day)
	require.NoError(t, err)
	assert.Equal(t, 0, final.Remain, "remain must be zero")
}
