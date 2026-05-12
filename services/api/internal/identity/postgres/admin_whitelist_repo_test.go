package postgres_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/takalawang/corporate-catering-system/services/api/internal/identity/postgres"
)

func TestAdminWhitelistRepo_IsAllowed_True(t *testing.T) {
	pool, cleanup := setupPostgres(t)
	defer cleanup()
	ctx := context.Background()

	_, err := pool.Exec(ctx, `
INSERT INTO admin_email_whitelist (email, added_by)
VALUES ($1, $2)`, "boss@example.com", "system")
	require.NoError(t, err)

	repo := postgres.NewAdminWhitelistRepo(pool)
	ok, err := repo.IsAllowed(ctx, "boss@example.com")
	require.NoError(t, err)
	assert.True(t, ok)
}

func TestAdminWhitelistRepo_IsAllowed_False(t *testing.T) {
	pool, cleanup := setupPostgres(t)
	defer cleanup()
	repo := postgres.NewAdminWhitelistRepo(pool)
	ok, err := repo.IsAllowed(context.Background(), "stranger@example.com")
	require.NoError(t, err)
	assert.False(t, ok)
}
