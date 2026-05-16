package postgres_test

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/takalawang/corporate-catering-system/services/api/internal/feedback"
	fpg "github.com/takalawang/corporate-catering-system/services/api/internal/feedback/postgres"
)

func TestRatingRepo_CreateAndGetByOrder(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test")
	}
	pool, cleanup := setupPostgres(t)
	defer cleanup()
	ctx := context.Background()

	repo := fpg.NewRatingRepo(pool)
	user := seedEmployeeUser(t, pool)
	vendor := seedVendor(t, pool)
	orderID := seedOrder(t, pool, user, vendor)

	r := &feedback.Rating{
		OrderID:  orderID,
		UserID:   user,
		VendorID: vendor,
		Score:    4,
		Comment:  "decent",
	}
	err := pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
		return repo.CreateTx(ctx, tx, r)
	})
	require.NoError(t, err)
	require.NotEmpty(t, r.ID)

	got, err := repo.GetByOrder(ctx, orderID)
	require.NoError(t, err)
	assert.Equal(t, r.ID, got.ID)
	assert.Equal(t, 4, got.Score)
	assert.Equal(t, "decent", got.Comment)
	assert.Equal(t, vendor, got.VendorID)
}

func TestRatingRepo_GetByOrder_NotFound(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test")
	}
	pool, cleanup := setupPostgres(t)
	defer cleanup()

	repo := fpg.NewRatingRepo(pool)
	_, err := repo.GetByOrder(context.Background(), "00000000-0000-0000-0000-000000000000")
	assert.ErrorIs(t, err, feedback.ErrRatingNotFound)
}

func TestRatingRepo_DuplicateOrder_Rejected(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test")
	}
	pool, cleanup := setupPostgres(t)
	defer cleanup()
	ctx := context.Background()

	repo := fpg.NewRatingRepo(pool)
	user := seedEmployeeUser(t, pool)
	vendor := seedVendor(t, pool)
	orderID := seedOrder(t, pool, user, vendor)

	first := &feedback.Rating{OrderID: orderID, UserID: user, VendorID: vendor, Score: 5}
	require.NoError(t, pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
		return repo.CreateTx(ctx, tx, first)
	}))

	second := &feedback.Rating{OrderID: orderID, UserID: user, VendorID: vendor, Score: 3}
	err := pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
		return repo.CreateTx(ctx, tx, second)
	})
	require.Error(t, err, "UNIQUE order_id must reject a second rating")
}

func TestRatingRepo_AggregateByVendorSince(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test")
	}
	pool, cleanup := setupPostgres(t)
	defer cleanup()
	ctx := context.Background()

	repo := fpg.NewRatingRepo(pool)
	user := seedEmployeeUser(t, pool)
	vendorA := seedVendor(t, pool)
	vendorB := seedVendor(t, pool)

	// vendorA: scores 2,4 → avg 3.0, count 2
	insertRating(t, pool, ctx, repo, user, vendorA, 2)
	insertRating(t, pool, ctx, repo, user, vendorA, 4)
	// vendorB: score 5 → avg 5.0, count 1
	insertRating(t, pool, ctx, repo, user, vendorB, 5)

	stats, err := repo.AggregateByVendorSince(ctx, time.Now().Add(-24*time.Hour))
	require.NoError(t, err)

	byVendor := map[string]feedback.VendorRatingStat{}
	for _, s := range stats {
		byVendor[s.VendorID] = s
	}
	require.Contains(t, byVendor, vendorA)
	assert.Equal(t, 2, byVendor[vendorA].SampleCount)
	assert.InDelta(t, 3.0, byVendor[vendorA].AvgScore, 0.001)
	require.Contains(t, byVendor, vendorB)
	assert.Equal(t, 1, byVendor[vendorB].SampleCount)
	assert.InDelta(t, 5.0, byVendor[vendorB].AvgScore, 0.001)
}

func insertRating(t *testing.T, pool *pgxpool.Pool, ctx context.Context, repo *fpg.RatingRepo, user, vendor string, score int) {
	t.Helper()
	orderID := seedOrder(t, pool, user, vendor)
	r := &feedback.Rating{OrderID: orderID, UserID: user, VendorID: vendor, Score: score}
	require.NoError(t, pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
		return repo.CreateTx(ctx, tx, r)
	}))
}
