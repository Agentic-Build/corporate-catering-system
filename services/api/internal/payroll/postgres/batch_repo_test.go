package postgres_test

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/takalawang/corporate-catering-system/services/api/internal/payroll"
	pgrepo "github.com/takalawang/corporate-catering-system/services/api/internal/payroll/postgres"
)

func mkPeriod(t *testing.T, year int, month time.Month) (time.Time, time.Time) {
	t.Helper()
	start := time.Date(year, month, 1, 0, 0, 0, 0, time.UTC)
	end := start.AddDate(0, 1, -1)
	return start, end
}

func TestBatchRepo_CreateAndGet(t *testing.T) {
	pool, cleanup := setupPostgres(t)
	defer cleanup()
	ctx := context.Background()
	repo := pgrepo.NewBatchRepo(pool)

	start, end := mkPeriod(t, 2026, time.January)
	b := &payroll.Batch{PeriodStart: start, PeriodEnd: end, Status: payroll.BatchStatusDraft}
	require.NoError(t, repo.Create(ctx, b))
	require.NotEmpty(t, b.ID)

	got, err := repo.GetByID(ctx, b.ID)
	require.NoError(t, err)
	assert.Equal(t, payroll.BatchStatusDraft, got.Status)
	assert.True(t, got.PeriodStart.Equal(start))
	assert.True(t, got.PeriodEnd.Equal(end))
	assert.Nil(t, got.LockedAt)
	assert.Nil(t, got.LockedBy)
	assert.Nil(t, got.ExportedAt)
	assert.Nil(t, got.ExportURI)
}

func TestBatchRepo_GetByID_NotFound(t *testing.T) {
	pool, cleanup := setupPostgres(t)
	defer cleanup()
	repo := pgrepo.NewBatchRepo(pool)
	_, err := repo.GetByID(context.Background(), "00000000-0000-0000-0000-000000000000")
	assert.ErrorIs(t, err, payroll.ErrBatchNotFound)
}

func TestBatchRepo_GetByPeriod(t *testing.T) {
	pool, cleanup := setupPostgres(t)
	defer cleanup()
	ctx := context.Background()
	repo := pgrepo.NewBatchRepo(pool)

	start, end := mkPeriod(t, 2026, time.February)
	b := &payroll.Batch{PeriodStart: start, PeriodEnd: end, Status: payroll.BatchStatusDraft}
	require.NoError(t, repo.Create(ctx, b))

	got, err := repo.GetByPeriod(ctx, start, end)
	require.NoError(t, err)
	assert.Equal(t, b.ID, got.ID)

	// Wrong period: not found
	_, err = repo.GetByPeriod(ctx, start.AddDate(1, 0, 0), end.AddDate(1, 0, 0))
	assert.ErrorIs(t, err, payroll.ErrBatchNotFound)
}

func TestBatchRepo_UpdateStatus_Conditional(t *testing.T) {
	pool, cleanup := setupPostgres(t)
	defer cleanup()
	ctx := context.Background()
	repo := pgrepo.NewBatchRepo(pool)

	start, end := mkPeriod(t, 2026, time.March)
	b := &payroll.Batch{PeriodStart: start, PeriodEnd: end, Status: payroll.BatchStatusDraft}
	require.NoError(t, repo.Create(ctx, b))
	admin := seedAdminUser(t, pool)

	// Happy: draft → locked sets locked_by + locked_at
	require.NoError(t, pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
		return repo.UpdateStatusTx(ctx, tx, b.ID, payroll.BatchStatusDraft, payroll.BatchStatusLocked, &admin)
	}))
	got, err := repo.GetByID(ctx, b.ID)
	require.NoError(t, err)
	assert.Equal(t, payroll.BatchStatusLocked, got.Status)
	require.NotNil(t, got.LockedAt)
	require.NotNil(t, got.LockedBy)
	assert.Equal(t, admin, *got.LockedBy)

	// Conflict: locked → locked from=draft must fail
	err = pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
		return repo.UpdateStatusTx(ctx, tx, b.ID, payroll.BatchStatusDraft, payroll.BatchStatusLocked, &admin)
	})
	assert.ErrorIs(t, err, payroll.ErrInvalidTransition)
}

func TestBatchRepo_DuplicatePeriod_Rejected(t *testing.T) {
	pool, cleanup := setupPostgres(t)
	defer cleanup()
	ctx := context.Background()
	repo := pgrepo.NewBatchRepo(pool)

	start, end := mkPeriod(t, 2026, time.April)
	b1 := &payroll.Batch{PeriodStart: start, PeriodEnd: end, Status: payroll.BatchStatusDraft}
	require.NoError(t, repo.Create(ctx, b1))

	b2 := &payroll.Batch{PeriodStart: start, PeriodEnd: end, Status: payroll.BatchStatusDraft}
	err := repo.Create(ctx, b2)
	assert.ErrorIs(t, err, payroll.ErrBatchPeriodExists)
}

func TestBatchRepo_SetExportInfo(t *testing.T) {
	pool, cleanup := setupPostgres(t)
	defer cleanup()
	ctx := context.Background()
	repo := pgrepo.NewBatchRepo(pool)

	start, end := mkPeriod(t, 2026, time.May)
	b := &payroll.Batch{PeriodStart: start, PeriodEnd: end, Status: payroll.BatchStatusDraft}
	require.NoError(t, repo.Create(ctx, b))
	admin := seedAdminUser(t, pool)
	require.NoError(t, pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
		return repo.UpdateStatusTx(ctx, tx, b.ID, payroll.BatchStatusDraft, payroll.BatchStatusLocked, &admin)
	}))

	uri := "s3://payroll/2026-05.csv"
	exportedAt := time.Now().UTC().Truncate(time.Second)
	require.NoError(t, pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
		return repo.SetExportInfoTx(ctx, tx, b.ID, uri, exportedAt)
	}))

	got, err := repo.GetByID(ctx, b.ID)
	require.NoError(t, err)
	assert.Equal(t, payroll.BatchStatusExported, got.Status)
	require.NotNil(t, got.ExportURI)
	assert.Equal(t, uri, *got.ExportURI)
	require.NotNil(t, got.ExportedAt)
	assert.WithinDuration(t, exportedAt, got.ExportedAt.UTC(), 2*time.Second)
}

func TestBatchRepo_List_FilteredByStatus(t *testing.T) {
	pool, cleanup := setupPostgres(t)
	defer cleanup()
	ctx := context.Background()
	repo := pgrepo.NewBatchRepo(pool)
	admin := seedAdminUser(t, pool)

	s1, e1 := mkPeriod(t, 2026, time.June)
	s2, e2 := mkPeriod(t, 2026, time.July)
	s3, e3 := mkPeriod(t, 2026, time.August)

	b1 := &payroll.Batch{PeriodStart: s1, PeriodEnd: e1, Status: payroll.BatchStatusDraft}
	b2 := &payroll.Batch{PeriodStart: s2, PeriodEnd: e2, Status: payroll.BatchStatusDraft}
	b3 := &payroll.Batch{PeriodStart: s3, PeriodEnd: e3, Status: payroll.BatchStatusDraft}
	require.NoError(t, repo.Create(ctx, b1))
	require.NoError(t, repo.Create(ctx, b2))
	require.NoError(t, repo.Create(ctx, b3))

	// Lock b2 + b3
	for _, id := range []string{b2.ID, b3.ID} {
		require.NoError(t, pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
			return repo.UpdateStatusTx(ctx, tx, id, payroll.BatchStatusDraft, payroll.BatchStatusLocked, &admin)
		}))
	}

	// All
	all, err := repo.List(ctx, nil)
	require.NoError(t, err)
	require.Len(t, all, 3)
	// Sorted by period_end DESC → b3, b2, b1
	assert.Equal(t, b3.ID, all[0].ID)
	assert.Equal(t, b2.ID, all[1].ID)
	assert.Equal(t, b1.ID, all[2].ID)

	// Locked only
	locked, err := repo.List(ctx, []payroll.BatchStatus{payroll.BatchStatusLocked})
	require.NoError(t, err)
	require.Len(t, locked, 2)
	assert.Equal(t, b3.ID, locked[0].ID)
	assert.Equal(t, b2.ID, locked[1].ID)

	// Draft only
	draft, err := repo.List(ctx, []payroll.BatchStatus{payroll.BatchStatusDraft})
	require.NoError(t, err)
	require.Len(t, draft, 1)
	assert.Equal(t, b1.ID, draft[0].ID)
}
