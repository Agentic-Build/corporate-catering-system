package postgres_test

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Agentic-Build/corporate-catering-system/services/api/internal/feedback"
	fpg "github.com/Agentic-Build/corporate-catering-system/services/api/internal/feedback/postgres"
)

// closedPool spins up a real Postgres, runs migrations, then closes the pool so
// every subsequent pool operation (Query/QueryRow) fails deterministically with
// a "closed pool" error. This drives the query/scan error-return branches that a
// successful query never reaches, without relying on flaky timing.
func closedPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	pool, cleanup := setupPostgres(t)
	t.Cleanup(cleanup)
	pool.Close()
	return pool
}

// ---- complaint_repo.go ----

func TestComplaintRepo_CreateTx_NilTx(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test")
	}
	repo := fpg.NewComplaintRepo(nil)
	err := repo.CreateTx(context.Background(), nil, &feedback.Complaint{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "requires a tx")
}

func TestComplaintRepo_CreateTx_InsertError(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test")
	}
	pool, cleanup := setupPostgres(t)
	defer cleanup()
	ctx := context.Background()
	repo := fpg.NewComplaintRepo(pool)

	// order_id/user_id/vendor_id reference nothing → FK violation makes the
	// INSERT...RETURNING fail, hitting the "create complaint" wrap.
	c := &feedback.Complaint{
		OrderID:  "00000000-0000-0000-0000-000000000000",
		UserID:   "00000000-0000-0000-0000-000000000000",
		VendorID: "00000000-0000-0000-0000-000000000000",
		Category: feedback.CategoryQuality, Description: "x",
	}
	err := pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
		return repo.CreateTx(ctx, tx, c)
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "create complaint")
}

func TestComplaintRepo_GetByID_QueryError(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test")
	}
	pool := closedPool(t)
	repo := fpg.NewComplaintRepo(pool)
	_, err := repo.GetByID(context.Background(), "00000000-0000-0000-0000-000000000001")
	require.Error(t, err)
	// Closed pool is not ErrNoRows; it falls through to the "scan complaint" wrap.
	assert.NotErrorIs(t, err, feedback.ErrComplaintNotFound)
	assert.Contains(t, err.Error(), "scan complaint")
}

func TestComplaintRepo_UpdateStatusTx_NilTx(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test")
	}
	repo := fpg.NewComplaintRepo(nil)
	err := repo.UpdateStatusTx(context.Background(), nil, "id",
		feedback.StatusOpen, feedback.StatusResolved, feedback.ComplaintUpdate{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "requires a tx")
}

func TestComplaintRepo_UpdateStatusTx_ExecError(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test")
	}
	pool, cleanup := setupPostgres(t)
	defer cleanup()
	ctx := context.Background()
	repo := fpg.NewComplaintRepo(pool)

	// Begin a tx, then roll it back so the subsequent Exec fails (tx already
	// closed). This exercises the "update complaint status" error wrap.
	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	require.NoError(t, tx.Rollback(ctx))

	err = repo.UpdateStatusTx(ctx, tx, "00000000-0000-0000-0000-000000000000",
		feedback.StatusOpen, feedback.StatusResolved, feedback.ComplaintUpdate{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "update complaint status")
}

func TestComplaintRepo_ListByUser_QueryError(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test")
	}
	pool := closedPool(t)
	repo := fpg.NewComplaintRepo(pool)
	_, err := repo.ListByUser(context.Background(), "00000000-0000-0000-0000-000000000000")
	require.Error(t, err)
}

func TestComplaintRepo_ListByVendor_QueryError(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test")
	}
	pool := closedPool(t)
	repo := fpg.NewComplaintRepo(pool)
	// Pass a status filter so the WHERE...IN branch is built before the query
	// fails on the closed pool.
	_, err := repo.ListByVendor(context.Background(), "00000000-0000-0000-0000-000000000000",
		[]feedback.ComplaintStatus{feedback.StatusOpen})
	require.Error(t, err)
}

func TestComplaintRepo_ListByStatus_QueryError(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test")
	}
	pool := closedPool(t)
	repo := fpg.NewComplaintRepo(pool)
	_, err := repo.ListByStatus(context.Background(),
		[]feedback.ComplaintStatus{feedback.StatusOpen})
	require.Error(t, err)
}

func TestComplaintRepo_CountByVendorSince_QueryError(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test")
	}
	pool := closedPool(t)
	repo := fpg.NewComplaintRepo(pool)
	_, err := repo.CountByVendorSince(context.Background(), time.Now().Add(-time.Hour))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "count complaints")
}

// TestComplaintRepo_CountByVendorSince_ScanError forces the per-row rows.Scan
// error return. vendor_id is the GROUP BY key scanned into a non-pointer string;
// NULLing it makes pgx fail with "cannot scan NULL". Each test owns its
// container, so the schema edit is isolated.
func TestComplaintRepo_CountByVendorSince_ScanError(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test")
	}
	pool, cleanup := setupPostgres(t)
	defer cleanup()
	ctx := context.Background()
	repo := fpg.NewComplaintRepo(pool)

	user := seedEmployeeUser(t, pool)
	vendor := seedVendor(t, pool)
	o := seedOrder(t, pool, user, vendor)
	c := &feedback.Complaint{OrderID: o, UserID: user, VendorID: vendor, Category: feedback.CategoryQuality, Description: "x"}
	require.NoError(t, pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error { return repo.CreateTx(ctx, tx, c) }))

	_, err := pool.Exec(ctx, `ALTER TABLE meal_complaint ALTER COLUMN vendor_id DROP NOT NULL`)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `UPDATE meal_complaint SET vendor_id = NULL`)
	require.NoError(t, err)

	_, err = repo.CountByVendorSince(ctx, time.Now().Add(-time.Hour))
	require.Error(t, err)
}

// TestComplaintRepo_List_ScanError drives the in-loop rows.Scan error inside
// collectComplaints (shared by ListByUser/ListByVendor/ListByStatus). category
// is scanned into a non-pointer string; NULLing it makes pgx fail.
func TestComplaintRepo_List_ScanError(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test")
	}
	pool, cleanup := setupPostgres(t)
	defer cleanup()
	ctx := context.Background()
	repo := fpg.NewComplaintRepo(pool)

	user := seedEmployeeUser(t, pool)
	vendor := seedVendor(t, pool)
	o := seedOrder(t, pool, user, vendor)
	c := &feedback.Complaint{OrderID: o, UserID: user, VendorID: vendor, Category: feedback.CategoryQuality, Description: "x"}
	require.NoError(t, pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error { return repo.CreateTx(ctx, tx, c) }))

	_, err := pool.Exec(ctx, `ALTER TABLE meal_complaint ALTER COLUMN category DROP NOT NULL`)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `UPDATE meal_complaint SET category = NULL`)
	require.NoError(t, err)

	_, err = repo.ListByUser(ctx, user)
	require.Error(t, err)
}

// ---- rating_repo.go ----

func TestRatingRepo_CreateTx_NilTx(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test")
	}
	repo := fpg.NewRatingRepo(nil)
	err := repo.CreateTx(context.Background(), nil, &feedback.Rating{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "requires a tx")
}

func TestRatingRepo_CreateTx_InsertError(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test")
	}
	pool, cleanup := setupPostgres(t)
	defer cleanup()
	ctx := context.Background()
	repo := fpg.NewRatingRepo(pool)

	// FK violation → INSERT...RETURNING fails, hitting the "create rating" wrap.
	r := &feedback.Rating{
		OrderID:  "00000000-0000-0000-0000-000000000000",
		UserID:   "00000000-0000-0000-0000-000000000000",
		VendorID: "00000000-0000-0000-0000-000000000000",
		Score:    3,
	}
	err := pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
		return repo.CreateTx(ctx, tx, r)
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "create rating")
}

func TestRatingRepo_GetByOrder_QueryError(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test")
	}
	pool := closedPool(t)
	repo := fpg.NewRatingRepo(pool)
	_, err := repo.GetByOrder(context.Background(), "00000000-0000-0000-0000-000000000001")
	require.Error(t, err)
	assert.NotErrorIs(t, err, feedback.ErrRatingNotFound)
	assert.Contains(t, err.Error(), "scan rating")
}

func TestRatingRepo_AggregateByVendorSince_QueryError(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test")
	}
	pool := closedPool(t)
	repo := fpg.NewRatingRepo(pool)
	_, err := repo.AggregateByVendorSince(context.Background(), time.Now().Add(-time.Hour))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "aggregate ratings")
}

// TestRatingRepo_AggregateByVendorSince_ScanError forces the per-row rows.Scan
// error return. vendor_id is the GROUP BY key scanned into a non-pointer string;
// NULLing it makes pgx fail with "cannot scan NULL".
func TestRatingRepo_AggregateByVendorSince_ScanError(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test")
	}
	pool, cleanup := setupPostgres(t)
	defer cleanup()
	ctx := context.Background()
	repo := fpg.NewRatingRepo(pool)

	user := seedEmployeeUser(t, pool)
	vendor := seedVendor(t, pool)
	o := seedOrder(t, pool, user, vendor)
	r := &feedback.Rating{OrderID: o, UserID: user, VendorID: vendor, Score: 4}
	require.NoError(t, pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error { return repo.CreateTx(ctx, tx, r) }))

	_, err := pool.Exec(ctx, `ALTER TABLE meal_rating ALTER COLUMN vendor_id DROP NOT NULL`)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `UPDATE meal_rating SET vendor_id = NULL`)
	require.NoError(t, err)

	_, err = repo.AggregateByVendorSince(ctx, time.Now().Add(-time.Hour))
	require.Error(t, err)
}

// ---- order_reader.go ----

func TestOrderReader_GetOrderInfo_QueryError(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test")
	}
	pool := closedPool(t)
	reader := fpg.NewOrderReader(pool)
	_, err := reader.GetOrderInfo(context.Background(), "00000000-0000-0000-0000-000000000001")
	require.Error(t, err)
	assert.NotErrorIs(t, err, feedback.ErrOrderNotFound)
	assert.Contains(t, err.Error(), "get order info")
}
