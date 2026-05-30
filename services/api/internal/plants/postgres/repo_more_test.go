package postgres_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Agentic-Build/corporate-catering-system/services/api/internal/plants"
	"github.com/Agentic-Build/corporate-catering-system/services/api/internal/plants/postgres"
)

// TestPlantRepo_ListScanError forces the rows.Scan error branch in List by
// making a column NULL that List scans into a non-pointer string field.
func TestPlantRepo_ListScanError(t *testing.T) {
	pool, cleanup := setupPostgres(t)
	defer cleanup()
	repo := postgres.NewPlantRepo(pool)
	ctx := context.Background()

	// Relax NOT NULL and insert a NULL label so the *string scan target fails.
	_, err := pool.Exec(ctx, `ALTER TABLE plant ALTER COLUMN label DROP NOT NULL`)
	require.NoError(t, err)
	_, err = pool.Exec(ctx,
		`INSERT INTO plant (code, label, active, sort_order) VALUES ($1, NULL, true, 1)`, "NUL")
	require.NoError(t, err)

	_, err = repo.List(ctx, false)
	assert.Error(t, err)
	assert.NotErrorIs(t, err, plants.ErrPlantNotFound)
}

// TestPlantRepo_CreateNonPKError drives Create into a non-PK DB error whose
// message is long enough that isPKConflict/contains actually scan via
// indexString and reach the no-match `return -1` path. A CHECK violation
// produces such a message ("violates check constraint ...") that contains
// neither "duplicate key" nor "unique constraint".
func TestPlantRepo_CreateNonPKError(t *testing.T) {
	pool, cleanup := setupPostgres(t)
	defer cleanup()
	repo := postgres.NewPlantRepo(pool)
	ctx := context.Background()

	_, err := pool.Exec(ctx,
		`ALTER TABLE plant ADD CONSTRAINT plant_code_not_bad CHECK (code <> 'BAD')`)
	require.NoError(t, err)

	err = repo.Create(ctx, &plants.Plant{Code: "BAD", Label: "x", Active: true})
	assert.Error(t, err)
	assert.NotErrorIs(t, err, plants.ErrDuplicateCode)
	assert.Contains(t, err.Error(), "check constraint")
}
