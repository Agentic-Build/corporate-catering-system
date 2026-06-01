package postgres_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Agentic-Build/corporate-catering-system/services/api/internal/quota"
	"github.com/Agentic-Build/corporate-catering-system/services/api/internal/quota/postgres"
)

// TestSupplyRepo_SetSoldOut exercises the success, not-found, and (via a closed
// pool) the generic Exec-error branches of SetSoldOut.
func TestSupplyRepo_SetSoldOut(t *testing.T) {
	pool, cleanup := setupPostgres(t)
	defer cleanup()
	vendorID := seedApprovedVendor(t, pool)
	itemID := seedActiveItem(t, pool, vendorID)
	repo := postgres.NewSupplyRepo(pool)
	ctx := context.Background()
	day := time.Now().UTC().Truncate(24 * time.Hour)

	require.NoError(t, repo.Upsert(ctx, &quota.Supply{
		MenuItemID: itemID, SupplyDate: day,
		Capacity: 10, Remain: 10,
		PickupWindow: "x", ETALabel: "x", CutoffAt: time.Now().Add(24 * time.Hour),
	}))

	// Success: flag sold out, verify it persisted.
	require.NoError(t, repo.SetSoldOut(ctx, itemID, day, true))
	got, err := repo.Get(ctx, itemID, day)
	require.NoError(t, err)
	assert.True(t, got.SoldOut)

	// Clear it again (covers soldOut=false path / RowsAffected>0).
	require.NoError(t, repo.SetSoldOut(ctx, itemID, day, false))
	got, err = repo.Get(ctx, itemID, day)
	require.NoError(t, err)
	assert.False(t, got.SoldOut)

	// Not found: no supply row for a different (item) on this date.
	other := seedActiveItem(t, pool, vendorID)
	err = repo.SetSoldOut(ctx, other, day, true)
	assert.ErrorIs(t, err, quota.ErrSupplyNotFound)
}

// TestSupplyRepo_SetSoldOut_ExecError hits the `err != nil` branch by issuing the
// Exec against a closed pool.
func TestSupplyRepo_SetSoldOut_ExecError(t *testing.T) {
	pool, cleanup := setupPostgres(t)
	vendorID := seedApprovedVendor(t, pool)
	itemID := seedActiveItem(t, pool, vendorID)
	repo := postgres.NewSupplyRepo(pool)
	cleanup() // close pool + terminate container

	err := repo.SetSoldOut(context.Background(), itemID, time.Now().UTC().Truncate(24*time.Hour), true)
	require.Error(t, err)
	assert.NotErrorIs(t, err, quota.ErrSupplyNotFound)
}

// TestSupplyRepo_DecrementTx covers DecrementTx success, ErrOutOfStock (row
// exists but remain < n), and ErrSupplyNotFound (no row) branches inside a
// caller-owned transaction.
func TestSupplyRepo_DecrementTx(t *testing.T) {
	pool, cleanup := setupPostgres(t)
	defer cleanup()
	vendorID := seedApprovedVendor(t, pool)
	itemID := seedActiveItem(t, pool, vendorID)
	repo := postgres.NewSupplyRepo(pool)
	ctx := context.Background()
	day := time.Now().UTC().Truncate(24 * time.Hour)

	require.NoError(t, repo.Upsert(ctx, &quota.Supply{
		MenuItemID: itemID, SupplyDate: day,
		Capacity: 3, Remain: 3,
		PickupWindow: "x", ETALabel: "x", CutoffAt: time.Now().Add(24 * time.Hour),
	}))

	tx, err := pool.Begin(ctx)
	require.NoError(t, err)

	// Success path.
	newRemain, err := repo.DecrementTx(ctx, tx, itemID, day, 2)
	require.NoError(t, err)
	assert.Equal(t, 1, newRemain)

	// remain(1) < n(2) → ErrOutOfStock (row exists).
	_, err = repo.DecrementTx(ctx, tx, itemID, day, 2)
	assert.ErrorIs(t, err, quota.ErrOutOfStock)

	// Non-existent (item,date) → ErrSupplyNotFound.
	other := seedActiveItem(t, pool, vendorID)
	_, err = repo.DecrementTx(ctx, tx, other, day, 1)
	assert.ErrorIs(t, err, quota.ErrSupplyNotFound)

	require.NoError(t, tx.Commit(ctx))

	// Rollback semantics: the committed decrement of 2 persisted.
	got, err := repo.Get(ctx, itemID, day)
	require.NoError(t, err)
	assert.Equal(t, 1, got.Remain)

	// n <= 0 guard.
	tx2, err := pool.Begin(ctx)
	require.NoError(t, err)
	_, err = repo.DecrementTx(ctx, tx2, itemID, day, 0)
	require.Error(t, err)
	_ = tx2.Rollback(ctx)
}

// TestSupplyRepo_DecrementTx_RolledBack proves that aborting the tx reverts the
// decrement (the transactional contract).
func TestSupplyRepo_DecrementTx_RolledBack(t *testing.T) {
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
		PickupWindow: "x", ETALabel: "x", CutoffAt: time.Now().Add(24 * time.Hour),
	}))

	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	_, err = repo.DecrementTx(ctx, tx, itemID, day, 3)
	require.NoError(t, err)
	require.NoError(t, tx.Rollback(ctx))

	got, err := repo.Get(ctx, itemID, day)
	require.NoError(t, err)
	assert.Equal(t, 5, got.Remain, "rollback must revert the decrement")
}

// TestSupplyRepo_RestoreTx covers RestoreTx success, the capacity cap, and the
// n <= 0 guard.
func TestSupplyRepo_RestoreTx(t *testing.T) {
	pool, cleanup := setupPostgres(t)
	defer cleanup()
	vendorID := seedApprovedVendor(t, pool)
	itemID := seedActiveItem(t, pool, vendorID)
	repo := postgres.NewSupplyRepo(pool)
	ctx := context.Background()
	day := time.Now().UTC().Truncate(24 * time.Hour)
	require.NoError(t, repo.Upsert(ctx, &quota.Supply{
		MenuItemID: itemID, SupplyDate: day,
		Capacity: 10, Remain: 4,
		PickupWindow: "x", ETALabel: "x", CutoffAt: time.Now().Add(24 * time.Hour),
	}))

	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	require.NoError(t, repo.RestoreTx(ctx, tx, itemID, day, 3))
	// cap at capacity
	require.NoError(t, repo.RestoreTx(ctx, tx, itemID, day, 100))
	// n <= 0 guard
	err = repo.RestoreTx(ctx, tx, itemID, day, 0)
	require.Error(t, err)
	require.NoError(t, tx.Commit(ctx))

	got, err := repo.Get(ctx, itemID, day)
	require.NoError(t, err)
	assert.Equal(t, 10, got.Remain, "restore capped at capacity")
}

// TestSupplyRepo_DecrementOutOfStockRowExists hits Decrement's ErrOutOfStock
// branch where the row exists but remain < n.
func TestSupplyRepo_DecrementOutOfStockRowExists(t *testing.T) {
	pool, cleanup := setupPostgres(t)
	defer cleanup()
	vendorID := seedApprovedVendor(t, pool)
	itemID := seedActiveItem(t, pool, vendorID)
	repo := postgres.NewSupplyRepo(pool)
	ctx := context.Background()
	day := time.Now().UTC().Truncate(24 * time.Hour)
	require.NoError(t, repo.Upsert(ctx, &quota.Supply{
		MenuItemID: itemID, SupplyDate: day,
		Capacity: 2, Remain: 2,
		PickupWindow: "x", ETALabel: "x", CutoffAt: time.Now().Add(24 * time.Hour),
	}))

	_, err := repo.Decrement(ctx, itemID, day, 5)
	assert.ErrorIs(t, err, quota.ErrOutOfStock)
}

// TestSupplyRepo_NonPositiveN guards Decrement and Restore against n <= 0.
func TestSupplyRepo_NonPositiveN(t *testing.T) {
	pool, cleanup := setupPostgres(t)
	defer cleanup()
	vendorID := seedApprovedVendor(t, pool)
	itemID := seedActiveItem(t, pool, vendorID)
	repo := postgres.NewSupplyRepo(pool)
	ctx := context.Background()
	day := time.Now().UTC().Truncate(24 * time.Hour)

	_, err := repo.Decrement(ctx, itemID, day, 0)
	require.Error(t, err)
	_, err = repo.Decrement(ctx, itemID, day, -3)
	require.Error(t, err)

	err = repo.Restore(ctx, itemID, day, 0)
	require.Error(t, err)
	err = repo.Restore(ctx, itemID, day, -1)
	require.Error(t, err)
}

// TestSupplyRepo_TxErrorsOnClosedTx drives the generic DB-error branches of
// DecrementTx and RestoreTx by issuing them against an already-finished tx,
// which pgx reports as a "tx is closed" error.
func TestSupplyRepo_TxErrorsOnClosedTx(t *testing.T) {
	pool, cleanup := setupPostgres(t)
	defer cleanup()
	vendorID := seedApprovedVendor(t, pool)
	itemID := seedActiveItem(t, pool, vendorID)
	repo := postgres.NewSupplyRepo(pool)
	ctx := context.Background()
	day := time.Now().UTC().Truncate(24 * time.Hour)

	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	require.NoError(t, tx.Rollback(ctx)) // tx now closed; subsequent ops error

	_, err = repo.DecrementTx(ctx, tx, itemID, day, 1)
	require.Error(t, err)
	assert.NotErrorIs(t, err, quota.ErrSupplyNotFound)
	assert.NotErrorIs(t, err, quota.ErrOutOfStock)

	err = repo.RestoreTx(ctx, tx, itemID, day, 1)
	require.Error(t, err)
}

// TestSupplyRepo_QueryErrorsOnClosedPool drives the generic DB-error branches of
// Get, ListByVendor, Decrement and Restore by closing the pool first. No tx is
// checked out, so pool.Close() does not block.
func TestSupplyRepo_QueryErrorsOnClosedPool(t *testing.T) {
	pool, cleanup := setupPostgres(t)
	vendorID := seedApprovedVendor(t, pool)
	itemID := seedActiveItem(t, pool, vendorID)
	repo := postgres.NewSupplyRepo(pool)
	ctx := context.Background()
	day := time.Now().UTC().Truncate(24 * time.Hour)

	cleanup() // close pool + terminate container

	_, err := repo.Get(ctx, itemID, day)
	require.Error(t, err)
	assert.NotErrorIs(t, err, quota.ErrSupplyNotFound)

	_, err = repo.ListByVendor(ctx, vendorID, day)
	require.Error(t, err)

	_, err = repo.Decrement(ctx, itemID, day, 1)
	require.Error(t, err)

	err = repo.Restore(ctx, itemID, day, 1)
	require.Error(t, err)
}
