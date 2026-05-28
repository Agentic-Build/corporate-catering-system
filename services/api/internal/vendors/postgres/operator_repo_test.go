package postgres_test

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Agentic-Build/corporate-catering-system/services/api/internal/vendors"
	"github.com/Agentic-Build/corporate-catering-system/services/api/internal/vendors/postgres"
)

func seedVendor(t *testing.T, pool *pgxpool.Pool, email string) string {
	t.Helper()
	repo := postgres.NewVendorRepo(pool)
	v := &vendor.Vendor{DisplayName: "V", LegalName: "V Ltd", ContactEmail: email, Status: vendor.StatusApproved}
	require.NoError(t, repo.Create(context.Background(), v))
	return v.ID
}

func TestOperatorRepo_UpsertAndGet(t *testing.T) {
	pool, cleanup := setupPostgres(t)
	defer cleanup()
	repo := postgres.NewOperatorRepo(pool)
	ctx := context.Background()
	vendorID := seedVendor(t, pool, "v1@x.com")

	op := &vendor.OperatorAccount{
		VendorID:    vendorID,
		Email:       "op1@x.com",
		DisplayName: "Operator One",
		Provider:    "authentik",
		Status:      vendor.OperatorStatusActive,
	}
	require.NoError(t, repo.Upsert(ctx, op))
	require.NotEmpty(t, op.ID)
	require.False(t, op.CreatedAt.IsZero())

	got, err := repo.Get(ctx, vendorID, op.ID)
	require.NoError(t, err)
	assert.Equal(t, "Operator One", got.DisplayName)
	assert.Equal(t, vendor.OperatorStatusActive, got.Status)
	assert.Equal(t, "authentik", got.Provider)

	// Upsert again on same (vendor_id, email) updates in place.
	sub := "ext-123"
	setup := "https://setup.example/abc"
	now := time.Now().UTC()
	op2 := &vendor.OperatorAccount{
		VendorID:        vendorID,
		Email:           "op1@x.com",
		DisplayName:     "Operator One Renamed",
		Provider:        "authentik",
		ExternalSubject: &sub,
		Status:          vendor.OperatorStatusSuspended,
		SetupURL:        &setup,
		LastSyncedAt:    &now,
	}
	require.NoError(t, repo.Upsert(ctx, op2))
	assert.Equal(t, op.ID, op2.ID)

	got2, err := repo.Get(ctx, vendorID, op.ID)
	require.NoError(t, err)
	assert.Equal(t, "Operator One Renamed", got2.DisplayName)
	assert.Equal(t, vendor.OperatorStatusSuspended, got2.Status)
	require.NotNil(t, got2.ExternalSubject)
	assert.Equal(t, "ext-123", *got2.ExternalSubject)
	require.NotNil(t, got2.SetupURL)
	assert.Equal(t, setup, *got2.SetupURL)
	require.NotNil(t, got2.LastSyncedAt)
}

func TestOperatorRepo_GetNotFound(t *testing.T) {
	pool, cleanup := setupPostgres(t)
	defer cleanup()
	repo := postgres.NewOperatorRepo(pool)
	vendorID := seedVendor(t, pool, "v2@x.com")
	_, err := repo.Get(context.Background(), vendorID, "00000000-0000-0000-0000-000000000000")
	assert.ErrorIs(t, err, vendor.ErrOperatorNotFound)
}

func TestOperatorRepo_ListByVendorAndStatus(t *testing.T) {
	pool, cleanup := setupPostgres(t)
	defer cleanup()
	repo := postgres.NewOperatorRepo(pool)
	ctx := context.Background()
	vendorID := seedVendor(t, pool, "v3@x.com")

	active := &vendor.OperatorAccount{VendorID: vendorID, Email: "a@x.com", DisplayName: "A", Provider: "authentik", Status: vendor.OperatorStatusActive}
	suspended := &vendor.OperatorAccount{VendorID: vendorID, Email: "b@x.com", DisplayName: "B", Provider: "authentik", Status: vendor.OperatorStatusSuspended}
	require.NoError(t, repo.Upsert(ctx, active))
	require.NoError(t, repo.Upsert(ctx, suspended))

	all, err := repo.ListByVendor(ctx, vendorID)
	require.NoError(t, err)
	assert.Len(t, all, 2)

	// Empty status filter falls back to ListByVendor.
	allViaStatus, err := repo.ListByVendorStatus(ctx, vendorID, nil)
	require.NoError(t, err)
	assert.Len(t, allViaStatus, 2)

	onlyActive, err := repo.ListByVendorStatus(ctx, vendorID, []vendor.OperatorStatus{vendor.OperatorStatusActive})
	require.NoError(t, err)
	require.Len(t, onlyActive, 1)
	assert.Equal(t, "A", onlyActive[0].DisplayName)

	both, err := repo.ListByVendorStatus(ctx, vendorID, []vendor.OperatorStatus{vendor.OperatorStatusActive, vendor.OperatorStatusSuspended})
	require.NoError(t, err)
	assert.Len(t, both, 2)

	// Unknown vendor yields empty.
	none, err := repo.ListByVendor(ctx, "00000000-0000-0000-0000-000000000000")
	require.NoError(t, err)
	assert.Empty(t, none)
}

func TestOperatorRepo_SetStatus(t *testing.T) {
	pool, cleanup := setupPostgres(t)
	defer cleanup()
	repo := postgres.NewOperatorRepo(pool)
	ctx := context.Background()
	vendorID := seedVendor(t, pool, "v4@x.com")

	op := &vendor.OperatorAccount{VendorID: vendorID, Email: "op@x.com", DisplayName: "Op", Provider: "authentik", Status: vendor.OperatorStatusActive}
	require.NoError(t, repo.Upsert(ctx, op))

	require.NoError(t, repo.SetStatus(ctx, vendorID, op.ID, vendor.OperatorStatusSuspended))
	got, err := repo.Get(ctx, vendorID, op.ID)
	require.NoError(t, err)
	assert.Equal(t, vendor.OperatorStatusSuspended, got.Status)

	// Unknown operator → not found.
	err = repo.SetStatus(ctx, vendorID, "00000000-0000-0000-0000-000000000000", vendor.OperatorStatusActive)
	assert.ErrorIs(t, err, vendor.ErrOperatorNotFound)
}

func TestOperatorRepo_SetStatuses(t *testing.T) {
	pool, cleanup := setupPostgres(t)
	defer cleanup()
	repo := postgres.NewOperatorRepo(pool)
	ctx := context.Background()
	vendorID := seedVendor(t, pool, "v5@x.com")

	a := &vendor.OperatorAccount{VendorID: vendorID, Email: "a@x.com", DisplayName: "A", Provider: "authentik", Status: vendor.OperatorStatusActive}
	b := &vendor.OperatorAccount{VendorID: vendorID, Email: "b@x.com", DisplayName: "B", Provider: "authentik", Status: vendor.OperatorStatusActive}
	require.NoError(t, repo.Upsert(ctx, a))
	require.NoError(t, repo.Upsert(ctx, b))

	// Empty from-set is a no-op.
	require.NoError(t, repo.SetStatuses(ctx, vendorID, nil, vendor.OperatorStatusVendorSuspended))
	still, err := repo.ListByVendorStatus(ctx, vendorID, []vendor.OperatorStatus{vendor.OperatorStatusActive})
	require.NoError(t, err)
	assert.Len(t, still, 2)

	require.NoError(t, repo.SetStatuses(ctx, vendorID, []vendor.OperatorStatus{vendor.OperatorStatusActive}, vendor.OperatorStatusVendorSuspended))
	vendorSuspended, err := repo.ListByVendorStatus(ctx, vendorID, []vendor.OperatorStatus{vendor.OperatorStatusVendorSuspended})
	require.NoError(t, err)
	assert.Len(t, vendorSuspended, 2)
	remainingActive, err := repo.ListByVendorStatus(ctx, vendorID, []vendor.OperatorStatus{vendor.OperatorStatusActive})
	require.NoError(t, err)
	assert.Empty(t, remainingActive)
}
