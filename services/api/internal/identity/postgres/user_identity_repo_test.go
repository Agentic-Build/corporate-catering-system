package postgres_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/takalawang/corporate-catering-system/services/api/internal/identity"
	"github.com/takalawang/corporate-catering-system/services/api/internal/identity/postgres"
)

func TestUserIdentityRepo_LinkAndGet(t *testing.T) {
	pool, cleanup := setupPostgres(t)
	defer cleanup()
	ctx := context.Background()

	users := postgres.NewUserRepo(pool)
	u := &identity.User{
		PrimaryEmail: "owner@example.com",
		DisplayName:  "Owner",
		Role:         identity.RoleEmployee,
		Status:       identity.StatusActive,
	}
	require.NoError(t, users.Create(ctx, u))

	repo := postgres.NewUserIdentityRepo(pool)
	ui := &identity.UserIdentity{
		UserID:          u.ID,
		Provider:        identity.ProviderGoogle,
		ExternalSubject: "google-sub-1",
		RawClaims:       map[string]any{"email": "owner@example.com", "hd": "example.com"},
	}
	require.NoError(t, repo.Link(ctx, ui))
	require.NotEmpty(t, ui.ID)
	require.False(t, ui.LinkedAt.IsZero())

	got, err := repo.GetByProviderSubject(ctx, identity.ProviderGoogle, "google-sub-1")
	require.NoError(t, err)
	assert.Equal(t, u.ID, got.UserID)
	assert.Equal(t, "owner@example.com", got.RawClaims["email"])
}

func TestUserIdentityRepo_GetByProviderSubject_NotFound(t *testing.T) {
	pool, cleanup := setupPostgres(t)
	defer cleanup()
	repo := postgres.NewUserIdentityRepo(pool)
	_, err := repo.GetByProviderSubject(context.Background(), identity.ProviderGoogle, "no-such-sub")
	assert.ErrorIs(t, err, identity.ErrIdentityNotFound)
}

func TestUserIdentityRepo_ListByUser(t *testing.T) {
	pool, cleanup := setupPostgres(t)
	defer cleanup()
	ctx := context.Background()

	users := postgres.NewUserRepo(pool)
	u := &identity.User{
		PrimaryEmail: "multi@example.com",
		DisplayName:  "Multi",
		Role:         identity.RoleEmployee,
		Status:       identity.StatusActive,
	}
	require.NoError(t, users.Create(ctx, u))

	repo := postgres.NewUserIdentityRepo(pool)
	require.NoError(t, repo.Link(ctx, &identity.UserIdentity{
		UserID: u.ID, Provider: identity.ProviderGoogle, ExternalSubject: "g-1", RawClaims: map[string]any{},
	}))
	require.NoError(t, repo.Link(ctx, &identity.UserIdentity{
		UserID: u.ID, Provider: identity.ProviderGitHub, ExternalSubject: "gh-1", RawClaims: map[string]any{},
	}))

	list, err := repo.ListByUser(ctx, u.ID)
	require.NoError(t, err)
	assert.Len(t, list, 2)
}
