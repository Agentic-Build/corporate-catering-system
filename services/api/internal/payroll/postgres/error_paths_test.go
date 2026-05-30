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

// cancelledCtx returns a context that is already cancelled, so any pool/tx
// query made with it fails immediately. This drives the generic DB-error
// branches (return fmt.Errorf("..."), return nil, err) that the happy-path
// tests never reach.
func cancelledCtx() context.Context {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	return ctx
}

func TestBatchRepo_Create_DefaultStatus(t *testing.T) {
	pool, cleanup := setupPostgres(t)
	defer cleanup()
	ctx := context.Background()
	repo := pgrepo.NewBatchRepo(pool)

	start := time.Date(2040, time.January, 1, 0, 0, 0, 0, time.UTC)
	end := start.AddDate(0, 1, -1)
	// Status left empty → Create defaults it to draft.
	b := &payroll.Batch{PeriodStart: start, PeriodEnd: end}
	require.NoError(t, repo.Create(ctx, b))
	assert.Equal(t, payroll.BatchStatusDraft, b.Status)

	got, err := repo.GetByID(ctx, b.ID)
	require.NoError(t, err)
	assert.Equal(t, payroll.BatchStatusDraft, got.Status)
}

func TestBatchRepo_ErrorPaths(t *testing.T) {
	pool, cleanup := setupPostgres(t)
	defer cleanup()
	repo := pgrepo.NewBatchRepo(pool)
	cctx := cancelledCtx()
	start := time.Date(2041, time.January, 1, 0, 0, 0, 0, time.UTC)
	end := start.AddDate(0, 1, -1)

	// Create generic error (not the unique-index branch).
	err := repo.Create(cctx, &payroll.Batch{PeriodStart: start, PeriodEnd: end})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "create batch")

	// CreateTx generic error: open a real tx, then run with a cancelled ctx.
	err = pgx.BeginFunc(context.Background(), pool, func(tx pgx.Tx) error {
		return repo.CreateTx(cctx, tx, &payroll.Batch{PeriodStart: start, PeriodEnd: end})
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "create batch tx")

	// scanOne generic error (GetByID): cancelled ctx → Scan fails (not ErrNoRows).
	_, err = repo.GetByID(cctx, "00000000-0000-0000-0000-000000000000")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "scan batch")

	// UpdateStatusTx Exec error (locked branch).
	err = pgx.BeginFunc(context.Background(), pool, func(tx pgx.Tx) error {
		return repo.UpdateStatusTx(cctx, tx, "00000000-0000-0000-0000-000000000000",
			payroll.BatchStatusDraft, payroll.BatchStatusLocked, nil)
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "update status")

	// SetExportInfoTx Exec error.
	err = pgx.BeginFunc(context.Background(), pool, func(tx pgx.Tx) error {
		return repo.SetExportInfoTx(cctx, tx, "00000000-0000-0000-0000-000000000000", "s3://x", time.Now())
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "set export info")

	// List Query error.
	_, err = repo.List(cctx, nil)
	require.Error(t, err)
}

func TestDisputeRepo_ErrorPaths(t *testing.T) {
	pool, cleanup := setupPostgres(t)
	defer cleanup()
	repo := pgrepo.NewDisputeRepo(pool)
	cctx := cancelledCtx()

	// Create generic error.
	err := repo.Create(cctx, &payroll.Dispute{OrderID: "00000000-0000-0000-0000-000000000000", OpenedBy: "00000000-0000-0000-0000-000000000000", Reason: "x"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "create dispute")

	// GetByID generic error (cancelled ctx, not ErrNoRows).
	_, err = repo.GetByID(cctx, "00000000-0000-0000-0000-000000000000")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "scan dispute")

	// UpdateStatusTx Exec error.
	err = pgx.BeginFunc(context.Background(), pool, func(tx pgx.Tx) error {
		return repo.UpdateStatusTx(cctx, tx, "00000000-0000-0000-0000-000000000000",
			payroll.DisputeStatusResolvedReject, nil, "x", 0)
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "update dispute status")

	// ListByStatus Query error.
	_, err = repo.ListByStatus(cctx, []payroll.DisputeStatus{payroll.DisputeStatusOpen})
	require.Error(t, err)

	// ListByUser Query error.
	_, err = repo.ListByUser(cctx, "00000000-0000-0000-0000-000000000000")
	require.Error(t, err)
}

func TestEntryRepo_ErrorPaths(t *testing.T) {
	pool, cleanup := setupPostgres(t)
	defer cleanup()
	repo := pgrepo.NewEntryRepo(pool)
	cctx := cancelledCtx()

	// CreateTx generic error.
	err := pgx.BeginFunc(context.Background(), pool, func(tx pgx.Tx) error {
		return repo.CreateTx(cctx, tx, &payroll.Entry{
			BatchID: "00000000-0000-0000-0000-000000000000",
			UserID:  "00000000-0000-0000-0000-000000000000",
		})
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "create entry")

	// GetByID generic error.
	_, err = repo.GetByID(cctx, "00000000-0000-0000-0000-000000000000")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "scan entry")

	// ListByBatch Query error.
	_, err = repo.ListByBatch(cctx, "00000000-0000-0000-0000-000000000000")
	require.Error(t, err)

	// FindByOrderForUser generic error.
	_, err = repo.FindByOrderForUser(cctx, "00000000-0000-0000-0000-000000000000", "00000000-0000-0000-0000-000000000000")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "find entry by order")

	// ListByUser Query error.
	_, err = repo.ListByUser(cctx, "00000000-0000-0000-0000-000000000000")
	require.Error(t, err)

	// IncrementRefundedTx Exec error.
	err = pgx.BeginFunc(context.Background(), pool, func(tx pgx.Tx) error {
		return repo.IncrementRefundedTx(cctx, tx, "00000000-0000-0000-0000-000000000000", 100)
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "increment refunded")
}

func TestExceptionRepo_ErrorPaths(t *testing.T) {
	pool, cleanup := setupPostgres(t)
	defer cleanup()
	repo := pgrepo.NewExceptionRepo(pool)
	cctx := cancelledCtx()

	// UpsertDepartedTx Exec error.
	err := pgx.BeginFunc(context.Background(), pool, func(tx pgx.Tx) error {
		return repo.UpsertDepartedTx(cctx, tx, "00000000-0000-0000-0000-000000000000")
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "upsert departed exceptions")

	// UpsertDeparted Exec error.
	err = repo.UpsertDeparted(cctx, "00000000-0000-0000-0000-000000000000")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "upsert departed exceptions")

	// Create generic error.
	err = repo.Create(cctx, &payroll.Exception{
		BatchID: "00000000-0000-0000-0000-000000000000",
		EntryID: "00000000-0000-0000-0000-000000000000",
		UserID:  "00000000-0000-0000-0000-000000000000",
		Kind:    payroll.ExceptionDeductionFailed,
		Detail:  "x",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "create exception")

	// GetByID generic error.
	_, err = repo.GetByID(cctx, "00000000-0000-0000-0000-000000000000")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "get exception")

	// ListByBatch Query error.
	_, err = repo.ListByBatch(cctx, "00000000-0000-0000-0000-000000000000")
	require.Error(t, err)

	// Resolve Exec error.
	err = repo.Resolve(cctx, "00000000-0000-0000-0000-000000000000",
		payroll.ExceptionResolved, "x", "00000000-0000-0000-0000-000000000000")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "resolve exception")
}
