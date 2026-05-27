package postgres_test

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/takalawang/corporate-catering-system/services/api/internal/vendors"
	"github.com/takalawang/corporate-catering-system/services/api/internal/vendors/postgres"
)

func seedAdminUser(t *testing.T, pool *pgxpool.Pool) string {
	t.Helper()
	var id string
	err := pool.QueryRow(context.Background(), `
INSERT INTO "user" (primary_email, display_name, role, status)
VALUES ('admin@test.com', 'Test Admin', 'welfare_admin', 'active')
RETURNING id`).Scan(&id)
	require.NoError(t, err)
	return id
}

func TestVendorRepo_CreateAndGet(t *testing.T) {
	pool, cleanup := setupPostgres(t)
	defer cleanup()
	repo := postgres.NewVendorRepo(pool)
	ctx := context.Background()

	v := &vendor.Vendor{
		DisplayName:  "稻禾家便當",
		LegalName:    "稻禾家便當有限公司",
		ContactEmail: "ops@daohe.tw",
		Status:       vendor.StatusPending,
	}
	require.NoError(t, repo.Create(ctx, v))
	require.NotEmpty(t, v.ID)
	require.False(t, v.CreatedAt.IsZero())

	got, err := repo.GetByID(ctx, v.ID)
	require.NoError(t, err)
	assert.Equal(t, "稻禾家便當", got.DisplayName)
	assert.Equal(t, vendor.StatusPending, got.Status)

	got2, err := repo.GetByEmail(ctx, "ops@daohe.tw")
	require.NoError(t, err)
	assert.Equal(t, v.ID, got2.ID)
}

func TestVendorRepo_NotFound(t *testing.T) {
	pool, cleanup := setupPostgres(t)
	defer cleanup()
	repo := postgres.NewVendorRepo(pool)
	_, err := repo.GetByID(context.Background(), "00000000-0000-0000-0000-000000000000")
	assert.ErrorIs(t, err, vendor.ErrVendorNotFound)
}

func TestVendorRepo_UpdateStatusAndList(t *testing.T) {
	pool, cleanup := setupPostgres(t)
	defer cleanup()
	repo := postgres.NewVendorRepo(pool)
	ctx := context.Background()

	a := &vendor.Vendor{DisplayName: "A", LegalName: "A Ltd", ContactEmail: "a@x.com", Status: vendor.StatusPending}
	b := &vendor.Vendor{DisplayName: "B", LegalName: "B Ltd", ContactEmail: "b@x.com", Status: vendor.StatusPending}
	require.NoError(t, repo.Create(ctx, a))
	require.NoError(t, repo.Create(ctx, b))

	adminID := seedAdminUser(t, pool)
	require.NoError(t, repo.UpdateStatus(ctx, a.ID, vendor.StatusApproved, &adminID))

	approved, err := repo.List(ctx, []vendor.Status{vendor.StatusApproved})
	require.NoError(t, err)
	assert.Len(t, approved, 1)
	assert.Equal(t, "A", approved[0].DisplayName)
	require.NotNil(t, approved[0].ApprovedAt)
	require.NotNil(t, approved[0].ApprovedBy)
	assert.Equal(t, adminID, *approved[0].ApprovedBy)

	// List all (empty filter)
	all, err := repo.List(ctx, nil)
	require.NoError(t, err)
	assert.Len(t, all, 2)
}

func TestVendorRepo_UpdateStatusNonApproved(t *testing.T) {
	pool, cleanup := setupPostgres(t)
	defer cleanup()
	repo := postgres.NewVendorRepo(pool)
	ctx := context.Background()

	v := &vendor.Vendor{DisplayName: "C", LegalName: "C Ltd", ContactEmail: "c@x.com", Status: vendor.StatusPending}
	require.NoError(t, repo.Create(ctx, v))

	require.NoError(t, repo.UpdateStatus(ctx, v.ID, vendor.StatusSuspended, nil))
	got, err := repo.GetByID(ctx, v.ID)
	require.NoError(t, err)
	assert.Equal(t, vendor.StatusSuspended, got.Status)
	assert.Nil(t, got.ApprovedAt)
	assert.Nil(t, got.ApprovedBy)
}

func TestVendorRepo_UpdateSettings(t *testing.T) {
	pool, cleanup := setupPostgres(t)
	defer cleanup()
	repo := postgres.NewVendorRepo(pool)
	ctx := context.Background()

	v := &vendor.Vendor{DisplayName: "D", LegalName: "D Ltd", ContactEmail: "d@x.com", Status: vendor.StatusApproved}
	require.NoError(t, repo.Create(ctx, v))

	require.NoError(t, repo.UpdateSettings(ctx, v.ID, 14, 7))
	got, err := repo.GetByID(ctx, v.ID)
	require.NoError(t, err)
	assert.Equal(t, 14, got.CutoffHour)
	assert.Equal(t, 7, got.PreorderWindowDays)

	err = repo.UpdateSettings(ctx, "00000000-0000-0000-0000-000000000000", 10, 3)
	assert.ErrorIs(t, err, vendor.ErrVendorNotFound)
}
