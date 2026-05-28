package postgres_test

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Agentic-Build/corporate-catering-system/services/api/internal/payroll"
	pgrepo "github.com/Agentic-Build/corporate-catering-system/services/api/internal/payroll/postgres"
)

func TestEntryRepo_ListByUser(t *testing.T) {
	pool, cleanup := setupPostgres(t)
	defer cleanup()
	ctx := context.Background()
	entryRepo := pgrepo.NewEntryRepo(pool)

	user := seedEmployeeUser(t, pool)
	other := seedEmployeeUser(t, pool)
	vendor := seedApprovedVendor(t, pool)

	// Two batches for user (Jan earlier, Feb later) + one for another user.
	b1 := makeBatch(t, pool, 2030, time.January)
	b2 := makeBatch(t, pool, 2030, time.February)
	bOther := makeBatch(t, pool, 2030, time.March)

	o1 := seedOrder(t, pool, user, vendor)
	o2a := seedOrder(t, pool, user, vendor)
	o2b := seedOrder(t, pool, user, vendor)
	oOther := seedOrder(t, pool, other, vendor)

	e1 := &payroll.Entry{BatchID: b1.ID, UserID: user, OrderIDs: []string{o1}, AmountMinor: 10000}
	e2 := &payroll.Entry{BatchID: b2.ID, UserID: user, OrderIDs: []string{o2a, o2b}, AmountMinor: 25000, RefundedMinor: 5000}
	eOther := &payroll.Entry{BatchID: bOther.ID, UserID: other, OrderIDs: []string{oOther}, AmountMinor: 9000}
	for _, e := range []*payroll.Entry{e1, e2, eOther} {
		e := e
		require.NoError(t, pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
			return entryRepo.CreateTx(ctx, tx, e)
		}))
	}

	list, err := entryRepo.ListByUser(ctx, user)
	require.NoError(t, err)
	require.Len(t, list, 2)
	// Newest period first → Feb (b2) then Jan (b1).
	assert.Equal(t, e2.ID, list[0].EntryID)
	assert.Equal(t, b2.ID, list[0].BatchID)
	assert.Equal(t, 2, list[0].OrderCount)
	assert.Equal(t, int64(25000), list[0].AmountMinor)
	assert.Equal(t, int64(5000), list[0].RefundedMinor)
	assert.Equal(t, payroll.BatchStatusDraft, list[0].BatchStatus)
	assert.True(t, list[0].PeriodStart.Equal(b2.PeriodStart))

	assert.Equal(t, e1.ID, list[1].EntryID)
	assert.Equal(t, 1, list[1].OrderCount)

	// User with no entries → empty.
	empty, err := entryRepo.ListByUser(ctx, seedEmployeeUser(t, pool))
	require.NoError(t, err)
	assert.Empty(t, empty)
}

func TestEntryRepo_TxNilGuards(t *testing.T) {
	pool, cleanup := setupPostgres(t)
	defer cleanup()
	ctx := context.Background()
	repo := pgrepo.NewEntryRepo(pool)

	err := repo.CreateTx(ctx, nil, &payroll.Entry{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "requires a tx")

	err = repo.IncrementRefundedTx(ctx, nil, "00000000-0000-0000-0000-000000000000", 100)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "requires a tx")
}

func TestEntryRepo_IncrementRefunded_NotFound(t *testing.T) {
	pool, cleanup := setupPostgres(t)
	defer cleanup()
	ctx := context.Background()
	repo := pgrepo.NewEntryRepo(pool)

	err := pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
		return repo.IncrementRefundedTx(ctx, tx, "00000000-0000-0000-0000-000000000000", 100)
	})
	assert.ErrorIs(t, err, payroll.ErrEntryNotFound)
}

func TestBatchRepo_CreateTx(t *testing.T) {
	pool, cleanup := setupPostgres(t)
	defer cleanup()
	ctx := context.Background()
	repo := pgrepo.NewBatchRepo(pool)

	start := time.Date(2031, time.January, 1, 0, 0, 0, 0, time.UTC)
	end := start.AddDate(0, 1, -1)
	b := &payroll.Batch{PeriodStart: start, PeriodEnd: end}
	require.NoError(t, pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
		return repo.CreateTx(ctx, tx, b)
	}))
	require.NotEmpty(t, b.ID)
	assert.Equal(t, payroll.BatchStatusDraft, b.Status) // defaulted

	got, err := repo.GetByID(ctx, b.ID)
	require.NoError(t, err)
	assert.Equal(t, payroll.BatchStatusDraft, got.Status)

	// Duplicate period inside a tx → ErrBatchPeriodExists.
	dup := &payroll.Batch{PeriodStart: start, PeriodEnd: end}
	err = pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
		return repo.CreateTx(ctx, tx, dup)
	})
	assert.ErrorIs(t, err, payroll.ErrBatchPeriodExists)
}

func TestBatchRepo_UpdateStatusTx_NonLockedTransition(t *testing.T) {
	pool, cleanup := setupPostgres(t)
	defer cleanup()
	ctx := context.Background()
	repo := pgrepo.NewBatchRepo(pool)
	admin := seedAdminUser(t, pool)

	b := makeBatch(t, pool, 2031, time.February)
	// draft → locked (sets locked_*), then locked → exported (non-locked branch).
	require.NoError(t, pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
		return repo.UpdateStatusTx(ctx, tx, b.ID, payroll.BatchStatusDraft, payroll.BatchStatusLocked, &admin)
	}))
	require.NoError(t, pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
		return repo.UpdateStatusTx(ctx, tx, b.ID, payroll.BatchStatusLocked, payroll.BatchStatusExported, nil)
	}))
	got, err := repo.GetByID(ctx, b.ID)
	require.NoError(t, err)
	assert.Equal(t, payroll.BatchStatusExported, got.Status)

	// Wrong `from` on the non-locked branch → ErrInvalidTransition.
	err = pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
		return repo.UpdateStatusTx(ctx, tx, b.ID, payroll.BatchStatusDraft, payroll.BatchStatusExported, nil)
	})
	assert.ErrorIs(t, err, payroll.ErrInvalidTransition)
}

func TestDisputeRepo_UpdateStatusTx_Guards(t *testing.T) {
	pool, cleanup := setupPostgres(t)
	defer cleanup()
	ctx := context.Background()
	repo := pgrepo.NewDisputeRepo(pool)
	admin := seedAdminUser(t, pool)

	// nil tx guard.
	err := repo.UpdateStatusTx(ctx, nil, "00000000-0000-0000-0000-000000000000",
		payroll.DisputeStatusResolvedReject, &admin, "x", 0)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "requires a tx")

	// Unknown id → not found.
	err = pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
		return repo.UpdateStatusTx(ctx, tx, "00000000-0000-0000-0000-000000000000",
			payroll.DisputeStatusResolvedReject, &admin, "x", 0)
	})
	assert.ErrorIs(t, err, payroll.ErrDisputeNotFound)
}
