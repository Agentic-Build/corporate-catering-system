package postgres_test

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/takalawang/corporate-catering-system/services/api/internal/identity"
	"github.com/takalawang/corporate-catering-system/services/api/internal/identity/postgres"
)

// seedVendor inserts a minimal vendor row (FK target for vendor_invite.vendor_id)
// and returns its uuid. Vendor_invite gained a FK to vendor(id) in 000002.
func seedVendor(t *testing.T, pool *pgxpool.Pool, email string) string {
	t.Helper()
	var id string
	require.NoError(t, pool.QueryRow(context.Background(), `
INSERT INTO vendor (display_name, legal_name, contact_email, status)
VALUES ('V', 'V Ltd', $1, 'approved')
RETURNING id`, email).Scan(&id))
	return id
}

func TestVendorInviteRepo_Get(t *testing.T) {
	pool, cleanup := setupPostgres(t)
	defer cleanup()
	ctx := context.Background()

	vendorID := seedVendor(t, pool, "get@example.com")
	expires := time.Now().Add(time.Hour).UTC()
	_, err := pool.Exec(ctx, `
INSERT INTO vendor_invite (code, vendor_id, email_hint, expires_at)
VALUES ($1, $2, $3, $4)`, "INVITE-1", vendorID, nil, expires)
	require.NoError(t, err)

	repo := postgres.NewVendorInviteRepo(pool)
	inv, err := repo.Get(ctx, "INVITE-1")
	require.NoError(t, err)
	assert.Equal(t, "INVITE-1", inv.Code)
	assert.Equal(t, vendorID, inv.VendorID)
	assert.Nil(t, inv.ConsumedAt)
	assert.Nil(t, inv.ConsumedBy)
	assert.WithinDuration(t, expires, inv.ExpiresAt, time.Second)
}

func TestVendorInviteRepo_Get_NotFound(t *testing.T) {
	pool, cleanup := setupPostgres(t)
	defer cleanup()
	repo := postgres.NewVendorInviteRepo(pool)
	_, err := repo.Get(context.Background(), "NOPE")
	assert.ErrorIs(t, err, identity.ErrInviteNotFound)
}

func TestVendorInviteRepo_Put(t *testing.T) {
	pool, cleanup := setupPostgres(t)
	defer cleanup()
	ctx := context.Background()

	vendorID := seedVendor(t, pool, "put@example.com")
	repo := postgres.NewVendorInviteRepo(pool)
	inv := &identity.VendorInvite{
		Code:      "TBI-TEST-001",
		VendorID:  vendorID,
		ExpiresAt: time.Now().Add(24 * time.Hour).UTC(),
	}
	require.NoError(t, repo.Put(ctx, inv))

	got, err := repo.Get(ctx, "TBI-TEST-001")
	require.NoError(t, err)
	assert.Equal(t, vendorID, got.VendorID)

	// Put again is idempotent (ON CONFLICT DO NOTHING).
	require.NoError(t, repo.Put(ctx, inv))
}

func TestVendorInviteRepo_Consume(t *testing.T) {
	pool, cleanup := setupPostgres(t)
	defer cleanup()
	ctx := context.Background()

	// Insert an active vendor_operator user (consumed_by FK).
	users := postgres.NewUserRepo(pool)
	u := &identity.User{
		PrimaryEmail: "vendor@example.com",
		DisplayName:  "Vendor Op",
		Role:         identity.RoleVendorOperator,
		Status:       identity.StatusActive,
	}
	require.NoError(t, users.Create(ctx, u))

	vendorID := seedVendor(t, pool, "consume@example.com")
	expires := time.Now().Add(time.Hour).UTC()
	_, err := pool.Exec(ctx, `
INSERT INTO vendor_invite (code, vendor_id, email_hint, expires_at)
VALUES ($1, $2, $3, $4)`, "INVITE-2", vendorID, nil, expires)
	require.NoError(t, err)

	repo := postgres.NewVendorInviteRepo(pool)
	require.NoError(t, repo.Consume(ctx, "INVITE-2", u.ID))

	inv, err := repo.Get(ctx, "INVITE-2")
	require.NoError(t, err)
	require.NotNil(t, inv.ConsumedAt)
	require.NotNil(t, inv.ConsumedBy)
	assert.Equal(t, u.ID, *inv.ConsumedBy)

	err = repo.Consume(ctx, "INVITE-2", u.ID)
	assert.ErrorIs(t, err, identity.ErrInviteAlreadyUsed)
}
