package postgres_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/takalawang/corporate-catering-system/services/api/internal/identity"
	"github.com/takalawang/corporate-catering-system/services/api/internal/identity/postgres"
)

func TestVendorInviteRepo_Get(t *testing.T) {
	pool, cleanup := setupPostgres(t)
	defer cleanup()
	ctx := context.Background()

	vendorID := uuid.NewString()
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

	vendorID := uuid.NewString()
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
