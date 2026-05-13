package postgres_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/takalawang/corporate-catering-system/services/api/internal/payroll"
	pgrepo "github.com/takalawang/corporate-catering-system/services/api/internal/payroll/postgres"
)

// makeBatch inserts a draft batch for a unique month so each test stays
// independent of any others sharing the pool.
func makeBatch(t *testing.T, pool *pgxpool.Pool, year int, month time.Month) *payroll.Batch {
	t.Helper()
	repo := pgrepo.NewBatchRepo(pool)
	start := time.Date(year, month, 1, 0, 0, 0, 0, time.UTC)
	end := start.AddDate(0, 1, -1)
	b := &payroll.Batch{PeriodStart: start, PeriodEnd: end, Status: payroll.BatchStatusDraft}
	require.NoError(t, repo.Create(context.Background(), b))
	return b
}

func TestEntryRepo_CreateAndGet(t *testing.T) {
	pool, cleanup := setupPostgres(t)
	defer cleanup()
	ctx := context.Background()
	repo := pgrepo.NewEntryRepo(pool)

	batch := makeBatch(t, pool, 2027, time.January)
	user := seedEmployeeUser(t, pool)
	vendor := seedApprovedVendor(t, pool)
	o1 := seedOrder(t, pool, user, vendor)
	o2 := seedOrder(t, pool, user, vendor)

	entry := &payroll.Entry{
		BatchID:     batch.ID,
		UserID:      user,
		OrderIDs:    []string{o1, o2},
		AmountMinor: 24000,
	}
	require.NoError(t, pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
		return repo.CreateTx(ctx, tx, entry)
	}))
	require.NotEmpty(t, entry.ID)

	got, err := repo.GetByID(ctx, entry.ID)
	require.NoError(t, err)
	assert.Equal(t, batch.ID, got.BatchID)
	assert.Equal(t, user, got.UserID)
	assert.Equal(t, int64(24000), got.AmountMinor)
	assert.Equal(t, int64(0), got.RefundedMinor)
	require.Len(t, got.OrderIDs, 2)
	assert.ElementsMatch(t, []string{o1, o2}, got.OrderIDs)
}

func TestEntryRepo_GetByID_NotFound(t *testing.T) {
	pool, cleanup := setupPostgres(t)
	defer cleanup()
	repo := pgrepo.NewEntryRepo(pool)
	_, err := repo.GetByID(context.Background(), "00000000-0000-0000-0000-000000000000")
	assert.ErrorIs(t, err, payroll.ErrEntryNotFound)
}

func TestEntryRepo_ListByBatch(t *testing.T) {
	pool, cleanup := setupPostgres(t)
	defer cleanup()
	ctx := context.Background()
	repo := pgrepo.NewEntryRepo(pool)

	batch := makeBatch(t, pool, 2027, time.February)
	vendor := seedApprovedVendor(t, pool)

	for i := 0; i < 3; i++ {
		user := seedEmployeeUser(t, pool)
		o := seedOrder(t, pool, user, vendor)
		entry := &payroll.Entry{
			BatchID:     batch.ID,
			UserID:      user,
			OrderIDs:    []string{o},
			AmountMinor: int64(10000 * (i + 1)),
		}
		require.NoError(t, pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
			return repo.CreateTx(ctx, tx, entry)
		}))
	}

	list, err := repo.ListByBatch(ctx, batch.ID)
	require.NoError(t, err)
	require.Len(t, list, 3)
	var sum int64
	for _, e := range list {
		assert.Equal(t, batch.ID, e.BatchID)
		sum += e.AmountMinor
	}
	assert.Equal(t, int64(60000), sum)
}

func TestEntryRepo_IncrementRefunded(t *testing.T) {
	pool, cleanup := setupPostgres(t)
	defer cleanup()
	ctx := context.Background()
	repo := pgrepo.NewEntryRepo(pool)

	batch := makeBatch(t, pool, 2027, time.March)
	user := seedEmployeeUser(t, pool)
	vendor := seedApprovedVendor(t, pool)
	o := seedOrder(t, pool, user, vendor)

	entry := &payroll.Entry{
		BatchID:     batch.ID,
		UserID:      user,
		OrderIDs:    []string{o},
		AmountMinor: 12000,
	}
	require.NoError(t, pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
		return repo.CreateTx(ctx, tx, entry)
	}))

	require.NoError(t, pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
		return repo.IncrementRefundedTx(ctx, tx, entry.ID, 3000)
	}))
	got, err := repo.GetByID(ctx, entry.ID)
	require.NoError(t, err)
	assert.Equal(t, int64(3000), got.RefundedMinor)

	// Increment again — should add, not overwrite.
	require.NoError(t, pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
		return repo.IncrementRefundedTx(ctx, tx, entry.ID, 2000)
	}))
	got, err = repo.GetByID(ctx, entry.ID)
	require.NoError(t, err)
	assert.Equal(t, int64(5000), got.RefundedMinor)
}

func TestEntryRepo_FindByOrderForUser(t *testing.T) {
	pool, cleanup := setupPostgres(t)
	defer cleanup()
	ctx := context.Background()
	repo := pgrepo.NewEntryRepo(pool)

	batch := makeBatch(t, pool, 2027, time.May)
	user := seedEmployeeUser(t, pool)
	other := seedEmployeeUser(t, pool)
	vendor := seedApprovedVendor(t, pool)
	o1 := seedOrder(t, pool, user, vendor)
	o2 := seedOrder(t, pool, user, vendor)
	oOther := seedOrder(t, pool, other, vendor)

	entry := &payroll.Entry{
		BatchID:     batch.ID,
		UserID:      user,
		OrderIDs:    []string{o1, o2},
		AmountMinor: 24000,
	}
	require.NoError(t, pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
		return repo.CreateTx(ctx, tx, entry)
	}))

	// Happy: each of user's orders maps back to the same entry.
	got, err := repo.FindByOrderForUser(ctx, user, o1)
	require.NoError(t, err)
	assert.Equal(t, entry.ID, got)
	got, err = repo.FindByOrderForUser(ctx, user, o2)
	require.NoError(t, err)
	assert.Equal(t, entry.ID, got)

	// Other user cannot match user's entry.
	_, err = repo.FindByOrderForUser(ctx, other, o1)
	assert.ErrorIs(t, err, payroll.ErrEntryNotFound)

	// Order that isn't aggregated into any entry yields not-found.
	_, err = repo.FindByOrderForUser(ctx, other, oOther)
	assert.ErrorIs(t, err, payroll.ErrEntryNotFound)
}

func TestEntryRepo_NoDelete(t *testing.T) {
	pool, cleanup := setupPostgres(t)
	defer cleanup()
	ctx := context.Background()
	repo := pgrepo.NewEntryRepo(pool)

	batch := makeBatch(t, pool, 2027, time.April)
	user := seedEmployeeUser(t, pool)
	vendor := seedApprovedVendor(t, pool)
	o := seedOrder(t, pool, user, vendor)

	entry := &payroll.Entry{
		BatchID:     batch.ID,
		UserID:      user,
		OrderIDs:    []string{o},
		AmountMinor: 12000,
	}
	require.NoError(t, pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
		return repo.CreateTx(ctx, tx, entry)
	}))

	_, err := pool.Exec(ctx, `DELETE FROM payroll_entry WHERE id=$1`, entry.ID)
	require.Error(t, err)
	assert.True(t, strings.Contains(err.Error(), "not deletable"),
		"expected append-only trigger error containing 'not deletable', got: %v", err)
}
