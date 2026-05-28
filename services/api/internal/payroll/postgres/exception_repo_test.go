package postgres_test

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Agentic-Build/corporate-catering-system/services/api/internal/payroll"
	pgrepo "github.com/Agentic-Build/corporate-catering-system/services/api/internal/payroll/postgres"
)

// makeExceptionEntry seeds a batch + entry pair and returns ids used by
// exception tests. The user's status is set so departed-detection can be
// exercised when departed=true.
func makeExceptionEntry(t *testing.T, pool *pgxpool.Pool, month time.Month, departed bool) (batchID, entryID, userID string) {
	t.Helper()
	ctx := context.Background()
	batchRepo := pgrepo.NewBatchRepo(pool)
	entryRepo := pgrepo.NewEntryRepo(pool)

	start := time.Date(2029, month, 1, 0, 0, 0, 0, time.UTC)
	end := start.AddDate(0, 1, -1)
	batch := &payroll.Batch{PeriodStart: start, PeriodEnd: end, Status: payroll.BatchStatusDraft}
	require.NoError(t, batchRepo.Create(ctx, batch))

	userID = seedEmployeeUser(t, pool)
	if departed {
		_, err := pool.Exec(ctx, `UPDATE "user" SET status='terminated' WHERE id=$1`, userID)
		require.NoError(t, err)
	}
	vendor := seedApprovedVendor(t, pool)
	order := seedOrder(t, pool, userID, vendor)

	entry := &payroll.Entry{
		BatchID:     batch.ID,
		UserID:      userID,
		OrderIDs:    []string{order},
		AmountMinor: 12000,
	}
	require.NoError(t, pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
		return entryRepo.CreateTx(ctx, tx, entry)
	}))
	return batch.ID, entry.ID, userID
}

func TestExceptionRepo_CreateAndGet(t *testing.T) {
	pool, cleanup := setupPostgres(t)
	defer cleanup()
	ctx := context.Background()
	repo := pgrepo.NewExceptionRepo(pool)

	batchID, entryID, userID := makeExceptionEntry(t, pool, time.January, false)

	e := &payroll.Exception{
		BatchID: batchID,
		EntryID: entryID,
		UserID:  userID,
		Kind:    payroll.ExceptionDeductionFailed,
		Detail:  "card declined",
	}
	require.NoError(t, repo.Create(ctx, e))
	require.NotEmpty(t, e.ID)
	assert.Equal(t, payroll.ExceptionOpen, e.Status) // defaulted

	got, err := repo.GetByID(ctx, e.ID)
	require.NoError(t, err)
	assert.Equal(t, batchID, got.BatchID)
	assert.Equal(t, entryID, got.EntryID)
	assert.Equal(t, userID, got.UserID)
	assert.Equal(t, payroll.ExceptionDeductionFailed, got.Kind)
	assert.Equal(t, payroll.ExceptionOpen, got.Status)
	assert.Equal(t, "card declined", got.Detail)
	assert.Equal(t, "", got.Resolution)
	assert.Nil(t, got.ResolvedBy)
	assert.Nil(t, got.ResolvedAt)
}

func TestExceptionRepo_Create_ExplicitStatus(t *testing.T) {
	pool, cleanup := setupPostgres(t)
	defer cleanup()
	ctx := context.Background()
	repo := pgrepo.NewExceptionRepo(pool)

	batchID, entryID, userID := makeExceptionEntry(t, pool, time.February, false)
	e := &payroll.Exception{
		BatchID: batchID,
		EntryID: entryID,
		UserID:  userID,
		Kind:    payroll.ExceptionDeductionFailed,
		Status:  payroll.ExceptionExcluded,
		Detail:  "dropped",
	}
	require.NoError(t, repo.Create(ctx, e))
	assert.Equal(t, payroll.ExceptionExcluded, e.Status)

	got, err := repo.GetByID(ctx, e.ID)
	require.NoError(t, err)
	assert.Equal(t, payroll.ExceptionExcluded, got.Status)
}

func TestExceptionRepo_GetByID_NotFound(t *testing.T) {
	pool, cleanup := setupPostgres(t)
	defer cleanup()
	repo := pgrepo.NewExceptionRepo(pool)
	_, err := repo.GetByID(context.Background(), "00000000-0000-0000-0000-000000000000")
	assert.ErrorIs(t, err, payroll.ErrExceptionNotFound)
}

func TestExceptionRepo_ListByBatch(t *testing.T) {
	pool, cleanup := setupPostgres(t)
	defer cleanup()
	ctx := context.Background()
	repo := pgrepo.NewExceptionRepo(pool)

	batchID, entryID, userID := makeExceptionEntry(t, pool, time.March, false)
	e1 := &payroll.Exception{BatchID: batchID, EntryID: entryID, UserID: userID, Kind: payroll.ExceptionDeductionFailed, Detail: "one"}
	require.NoError(t, repo.Create(ctx, e1))

	list, err := repo.ListByBatch(ctx, batchID)
	require.NoError(t, err)
	require.Len(t, list, 1)
	assert.Equal(t, e1.ID, list[0].ID)
	assert.Equal(t, "one", list[0].Detail)

	// Unknown batch → empty.
	empty, err := repo.ListByBatch(ctx, "00000000-0000-0000-0000-000000000000")
	require.NoError(t, err)
	assert.Empty(t, empty)
}

func TestExceptionRepo_Resolve(t *testing.T) {
	pool, cleanup := setupPostgres(t)
	defer cleanup()
	ctx := context.Background()
	repo := pgrepo.NewExceptionRepo(pool)
	admin := seedAdminUser(t, pool)

	batchID, entryID, userID := makeExceptionEntry(t, pool, time.April, false)
	e := &payroll.Exception{BatchID: batchID, EntryID: entryID, UserID: userID, Kind: payroll.ExceptionDeductionFailed, Detail: "x"}
	require.NoError(t, repo.Create(ctx, e))

	require.NoError(t, repo.Resolve(ctx, e.ID, payroll.ExceptionResolved, "handled by HR", admin))
	got, err := repo.GetByID(ctx, e.ID)
	require.NoError(t, err)
	assert.Equal(t, payroll.ExceptionResolved, got.Status)
	assert.Equal(t, "handled by HR", got.Resolution)
	require.NotNil(t, got.ResolvedBy)
	assert.Equal(t, admin, *got.ResolvedBy)
	require.NotNil(t, got.ResolvedAt)

	// Resolving an unknown id → not found.
	err = repo.Resolve(ctx, "00000000-0000-0000-0000-000000000000", payroll.ExceptionResolved, "n/a", admin)
	assert.ErrorIs(t, err, payroll.ErrExceptionNotFound)
}

func TestExceptionRepo_UpsertDeparted(t *testing.T) {
	pool, cleanup := setupPostgres(t)
	defer cleanup()
	ctx := context.Background()
	repo := pgrepo.NewExceptionRepo(pool)

	// One departed employee, one active employee on the same batch.
	batchID, _, departedUser := makeExceptionEntry(t, pool, time.May, true)
	entryRepo := pgrepo.NewEntryRepo(pool)
	activeUser := seedEmployeeUser(t, pool)
	vendor := seedApprovedVendor(t, pool)
	o := seedOrder(t, pool, activeUser, vendor)
	active := &payroll.Entry{BatchID: batchID, UserID: activeUser, OrderIDs: []string{o}, AmountMinor: 12000}
	require.NoError(t, pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
		return entryRepo.CreateTx(ctx, tx, active)
	}))

	require.NoError(t, repo.UpsertDeparted(ctx, batchID))

	list, err := repo.ListByBatch(ctx, batchID)
	require.NoError(t, err)
	require.Len(t, list, 1) // only the departed user flagged
	assert.Equal(t, departedUser, list[0].UserID)
	assert.Equal(t, payroll.ExceptionEmployeeDeparted, list[0].Kind)
	assert.Equal(t, payroll.ExceptionOpen, list[0].Status)

	// Idempotent: re-running does not duplicate.
	require.NoError(t, repo.UpsertDeparted(ctx, batchID))
	list, err = repo.ListByBatch(ctx, batchID)
	require.NoError(t, err)
	require.Len(t, list, 1)
}

func TestExceptionRepo_UpsertDepartedTx(t *testing.T) {
	pool, cleanup := setupPostgres(t)
	defer cleanup()
	ctx := context.Background()
	repo := pgrepo.NewExceptionRepo(pool)

	batchID, _, departedUser := makeExceptionEntry(t, pool, time.June, true)
	require.NoError(t, pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
		return repo.UpsertDepartedTx(ctx, tx, batchID)
	}))

	list, err := repo.ListByBatch(ctx, batchID)
	require.NoError(t, err)
	require.Len(t, list, 1)
	assert.Equal(t, departedUser, list[0].UserID)
	assert.Equal(t, payroll.ExceptionEmployeeDeparted, list[0].Kind)
}
