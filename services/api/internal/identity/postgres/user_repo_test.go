package postgres_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Agentic-Build/corporate-catering-system/services/api/internal/identity"
	"github.com/Agentic-Build/corporate-catering-system/services/api/internal/identity/postgres"
)

func TestUserRepo_CreateAndGet(t *testing.T) {
	pool, cleanup := setupPostgres(t)
	defer cleanup()
	repo := postgres.NewUserRepo(pool)
	ctx := context.Background()

	empID := "E001"
	plant := "F12B-3F"
	u := &identity.User{
		PrimaryEmail: "test@example.com",
		DisplayName:  "Test User",
		Role:         identity.RoleEmployee,
		Status:       identity.StatusActive,
		EmployeeID:   &empID,
		Plant:        &plant,
	}
	require.NoError(t, repo.Create(ctx, u))
	require.NotEmpty(t, u.ID)
	require.False(t, u.CreatedAt.IsZero())

	got, err := repo.GetByEmail(ctx, "test@example.com")
	require.NoError(t, err)
	assert.Equal(t, "Test User", got.DisplayName)
	assert.Equal(t, identity.RoleEmployee, got.Role)
	assert.Equal(t, "E001", *got.EmployeeID)

	got2, err := repo.GetByID(ctx, u.ID)
	require.NoError(t, err)
	assert.Equal(t, u.PrimaryEmail, got2.PrimaryEmail)
}

func TestUserRepo_GetByEmail_NotFound(t *testing.T) {
	pool, cleanup := setupPostgres(t)
	defer cleanup()
	repo := postgres.NewUserRepo(pool)
	_, err := repo.GetByEmail(context.Background(), "nobody@example.com")
	assert.ErrorIs(t, err, identity.ErrUserNotFound)
}

func TestUserRepo_UpdateStatus(t *testing.T) {
	pool, cleanup := setupPostgres(t)
	defer cleanup()
	repo := postgres.NewUserRepo(pool)
	ctx := context.Background()
	u := &identity.User{
		PrimaryEmail: "suspend-me@example.com",
		DisplayName:  "Z",
		Role:         identity.RoleEmployee,
		Status:       identity.StatusActive,
	}
	require.NoError(t, repo.Create(ctx, u))
	require.NoError(t, repo.UpdateStatus(ctx, u.ID, identity.StatusSuspended))
	got, _ := repo.GetByID(ctx, u.ID)
	assert.Equal(t, identity.StatusSuspended, got.Status)
}
