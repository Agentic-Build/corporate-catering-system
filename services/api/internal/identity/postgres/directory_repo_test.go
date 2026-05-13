package postgres_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/takalawang/corporate-catering-system/services/api/internal/identity"
	"github.com/takalawang/corporate-catering-system/services/api/internal/identity/postgres"
)

func TestDirectoryRepo_GetByEmail(t *testing.T) {
	pool, cleanup := setupPostgres(t)
	defer cleanup()
	ctx := context.Background()

	_, err := pool.Exec(ctx, `
INSERT INTO employee_directory (employee_id, primary_email, display_name, plant, department, status)
VALUES ($1, $2, $3, $4, $5, $6)`,
		"E100", "alice@example.com", "Alice", "F12B-3F", "ENG", string(identity.StatusActive))
	require.NoError(t, err)

	repo := postgres.NewDirectoryRepo(pool)
	got, err := repo.GetByEmail(ctx, "alice@example.com")
	require.NoError(t, err)
	assert.Equal(t, "E100", got.EmployeeID)
	assert.Equal(t, "Alice", got.DisplayName)
	require.NotNil(t, got.Plant)
	assert.Equal(t, "F12B-3F", *got.Plant)
	require.NotNil(t, got.Department)
	assert.Equal(t, "ENG", *got.Department)
	assert.Equal(t, identity.StatusActive, got.Status)
}

func TestDirectoryRepo_GetByEmail_NotInDirectory(t *testing.T) {
	pool, cleanup := setupPostgres(t)
	defer cleanup()
	repo := postgres.NewDirectoryRepo(pool)
	_, err := repo.GetByEmail(context.Background(), "ghost@example.com")
	assert.ErrorIs(t, err, identity.ErrNotInDirectory)
}
