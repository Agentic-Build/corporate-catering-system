package postgres_test

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Agentic-Build/corporate-catering-system/services/api/internal/feedback"
	fpg "github.com/Agentic-Build/corporate-catering-system/services/api/internal/feedback/postgres"
)

func TestComplaintRepo_CreateAndGetByID(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test")
	}
	pool, cleanup := setupPostgres(t)
	defer cleanup()
	ctx := context.Background()

	repo := fpg.NewComplaintRepo(pool)
	user := seedEmployeeUser(t, pool)
	vendor := seedVendor(t, pool)
	orderID := seedOrder(t, pool, user, vendor)

	c := &feedback.Complaint{
		OrderID:     orderID,
		UserID:      user,
		VendorID:    vendor,
		Category:    feedback.CategoryMissingItem,
		Description: "missing the side dish",
	}
	require.NoError(t, pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
		return repo.CreateTx(ctx, tx, c)
	}))
	require.NotEmpty(t, c.ID)
	assert.Equal(t, feedback.StatusOpen, c.Status)

	got, err := repo.GetByID(ctx, c.ID)
	require.NoError(t, err)
	assert.Equal(t, feedback.CategoryMissingItem, got.Category)
	assert.Equal(t, feedback.StatusOpen, got.Status)
	assert.Equal(t, "missing the side dish", got.Description)
}

func TestComplaintRepo_GetByID_NotFound(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test")
	}
	pool, cleanup := setupPostgres(t)
	defer cleanup()

	repo := fpg.NewComplaintRepo(pool)
	_, err := repo.GetByID(context.Background(), "00000000-0000-0000-0000-000000000000")
	assert.ErrorIs(t, err, feedback.ErrComplaintNotFound)
}

// TestComplaintRepo_OneOpenPerOrder verifies the partial unique index:
// a second non-resolved complaint for the same order is rejected, but once
// the first is resolved a new one may be created.
func TestComplaintRepo_OneOpenPerOrder(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test")
	}
	pool, cleanup := setupPostgres(t)
	defer cleanup()
	ctx := context.Background()

	repo := fpg.NewComplaintRepo(pool)
	user := seedEmployeeUser(t, pool)
	vendor := seedVendor(t, pool)
	orderID := seedOrder(t, pool, user, vendor)

	first := &feedback.Complaint{
		OrderID: orderID, UserID: user, VendorID: vendor,
		Category: feedback.CategoryQuality, Description: "cold food",
	}
	require.NoError(t, pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
		return repo.CreateTx(ctx, tx, first)
	}))

	// A second open complaint for the same order must be rejected.
	second := &feedback.Complaint{
		OrderID: orderID, UserID: user, VendorID: vendor,
		Category: feedback.CategoryPortion, Description: "too small",
	}
	err := pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
		return repo.CreateTx(ctx, tx, second)
	})
	require.Error(t, err, "partial unique index must reject a 2nd unresolved complaint")

	// Resolve the first; the index no longer covers it.
	resolvedBy := user
	require.NoError(t, pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
		return repo.UpdateStatusTx(ctx, tx, first.ID, feedback.StatusOpen, feedback.StatusResolved,
			feedback.ComplaintUpdate{Resolution: "done", ResolvedBy: &resolvedBy})
	}))

	// Now a new complaint for the same order is allowed.
	third := &feedback.Complaint{
		OrderID: orderID, UserID: user, VendorID: vendor,
		Category: feedback.CategoryHygiene, Description: "hair found",
	}
	require.NoError(t, pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
		return repo.CreateTx(ctx, tx, third)
	}))
}

func TestComplaintRepo_UpdateStatus_ConditionalOnFrom(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test")
	}
	pool, cleanup := setupPostgres(t)
	defer cleanup()
	ctx := context.Background()

	repo := fpg.NewComplaintRepo(pool)
	user := seedEmployeeUser(t, pool)
	vendor := seedVendor(t, pool)
	orderID := seedOrder(t, pool, user, vendor)

	c := &feedback.Complaint{
		OrderID: orderID, UserID: user, VendorID: vendor,
		Category: feedback.CategoryOther, Description: "something off",
	}
	require.NoError(t, pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
		return repo.CreateTx(ctx, tx, c)
	}))

	// Wrong `from` → ErrInvalidTransition, no row updated.
	err := pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
		return repo.UpdateStatusTx(ctx, tx, c.ID, feedback.StatusEscalated, feedback.StatusResolved, feedback.ComplaintUpdate{})
	})
	assert.ErrorIs(t, err, feedback.ErrInvalidTransition)

	// Correct `from` → succeeds and writes the response + timestamp.
	require.NoError(t, pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
		return repo.UpdateStatusTx(ctx, tx, c.ID, feedback.StatusOpen, feedback.StatusVendorResponded,
			feedback.ComplaintUpdate{VendorResponse: "we will look into it"})
	}))
	got, err := repo.GetByID(ctx, c.ID)
	require.NoError(t, err)
	assert.Equal(t, feedback.StatusVendorResponded, got.Status)
	assert.Equal(t, "we will look into it", got.VendorResponse)
	require.NotNil(t, got.VendorRespondedAt)
}

func TestComplaintRepo_ListByUserVendorStatus(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test")
	}
	pool, cleanup := setupPostgres(t)
	defer cleanup()
	ctx := context.Background()

	repo := fpg.NewComplaintRepo(pool)
	user := seedEmployeeUser(t, pool)
	other := seedEmployeeUser(t, pool)
	vendor := seedVendor(t, pool)

	o1 := seedOrder(t, pool, user, vendor)
	o2 := seedOrder(t, pool, other, vendor)

	c1 := &feedback.Complaint{OrderID: o1, UserID: user, VendorID: vendor, Category: feedback.CategoryQuality, Description: "issue one"}
	c2 := &feedback.Complaint{OrderID: o2, UserID: other, VendorID: vendor, Category: feedback.CategoryQuality, Description: "issue two"}
	require.NoError(t, pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error { return repo.CreateTx(ctx, tx, c1) }))
	require.NoError(t, pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error { return repo.CreateTx(ctx, tx, c2) }))

	mine, err := repo.ListByUser(ctx, user)
	require.NoError(t, err)
	require.Len(t, mine, 1)
	assert.Equal(t, c1.ID, mine[0].ID)

	vendorAll, err := repo.ListByVendor(ctx, vendor, nil)
	require.NoError(t, err)
	assert.Len(t, vendorAll, 2)

	openOnly, err := repo.ListByVendor(ctx, vendor, []feedback.ComplaintStatus{feedback.StatusOpen})
	require.NoError(t, err)
	assert.Len(t, openOnly, 2)

	escalated, err := repo.ListByStatus(ctx, []feedback.ComplaintStatus{feedback.StatusEscalated})
	require.NoError(t, err)
	assert.Empty(t, escalated)
}

func TestComplaintRepo_CountByVendorSince(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test")
	}
	pool, cleanup := setupPostgres(t)
	defer cleanup()
	ctx := context.Background()

	repo := fpg.NewComplaintRepo(pool)
	user := seedEmployeeUser(t, pool)
	vendorA := seedVendor(t, pool)
	vendorB := seedVendor(t, pool)

	for i := 0; i < 3; i++ {
		o := seedOrder(t, pool, user, vendorA)
		c := &feedback.Complaint{OrderID: o, UserID: user, VendorID: vendorA, Category: feedback.CategoryQuality, Description: "vendorA issue"}
		require.NoError(t, pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error { return repo.CreateTx(ctx, tx, c) }))
	}
	o := seedOrder(t, pool, user, vendorB)
	c := &feedback.Complaint{OrderID: o, UserID: user, VendorID: vendorB, Category: feedback.CategoryQuality, Description: "vendorB issue"}
	require.NoError(t, pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error { return repo.CreateTx(ctx, tx, c) }))

	stats, err := repo.CountByVendorSince(ctx, time.Now().Add(-24*time.Hour))
	require.NoError(t, err)
	byVendor := map[string]int{}
	for _, s := range stats {
		byVendor[s.VendorID] = s.Count
	}
	assert.Equal(t, 3, byVendor[vendorA])
	assert.Equal(t, 1, byVendor[vendorB])
}
