package postgres_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Agentic-Build/corporate-catering-system/services/api/internal/plants"
	"github.com/Agentic-Build/corporate-catering-system/services/api/internal/plants/postgres"
)

func TestPlantRepo_CreateAndGet(t *testing.T) {
	pool, cleanup := setupPostgres(t)
	defer cleanup()
	repo := postgres.NewPlantRepo(pool)
	ctx := context.Background()

	p := &plants.Plant{
		Code:      "HSP",
		Label:     "新竹科學園區",
		Address:   "新竹市東區力行路1號",
		Active:    true,
		SortOrder: 10,
	}
	require.NoError(t, repo.Create(ctx, p))

	got, err := repo.Get(ctx, "HSP")
	require.NoError(t, err)
	assert.Equal(t, "HSP", got.Code)
	assert.Equal(t, "新竹科學園區", got.Label)
	assert.Equal(t, "新竹市東區力行路1號", got.Address)
	assert.True(t, got.Active)
	assert.Equal(t, 10, got.SortOrder)
	assert.False(t, got.CreatedAt.IsZero())
	assert.False(t, got.UpdatedAt.IsZero())
}

func TestPlantRepo_GetNotFound(t *testing.T) {
	pool, cleanup := setupPostgres(t)
	defer cleanup()
	repo := postgres.NewPlantRepo(pool)
	_, err := repo.Get(context.Background(), "NOPE")
	assert.ErrorIs(t, err, plants.ErrPlantNotFound)
}

func TestPlantRepo_DuplicateCode(t *testing.T) {
	pool, cleanup := setupPostgres(t)
	defer cleanup()
	repo := postgres.NewPlantRepo(pool)
	ctx := context.Background()

	p := &plants.Plant{Code: "DUP", Label: "First", Active: true}
	require.NoError(t, repo.Create(ctx, p))

	err := repo.Create(ctx, &plants.Plant{Code: "DUP", Label: "Second", Active: true})
	assert.ErrorIs(t, err, plants.ErrDuplicateCode)
	assert.Contains(t, err.Error(), "DUP")
}

func TestPlantRepo_List(t *testing.T) {
	pool, cleanup := setupPostgres(t)
	defer cleanup()
	repo := postgres.NewPlantRepo(pool)
	ctx := context.Background()

	// Insert out of sort order to verify ORDER BY sort_order, code.
	require.NoError(t, repo.Create(ctx, &plants.Plant{Code: "TNP", Label: "台南", Active: true, SortOrder: 20}))
	require.NoError(t, repo.Create(ctx, &plants.Plant{Code: "HSP", Label: "新竹", Active: true, SortOrder: 10}))
	require.NoError(t, repo.Create(ctx, &plants.Plant{Code: "OLD", Label: "停用", Active: false, SortOrder: 5}))

	all, err := repo.List(ctx, false)
	require.NoError(t, err)
	require.Len(t, all, 3)
	assert.Equal(t, "OLD", all[0].Code)
	assert.Equal(t, "HSP", all[1].Code)
	assert.Equal(t, "TNP", all[2].Code)

	activeOnly, err := repo.List(ctx, true)
	require.NoError(t, err)
	require.Len(t, activeOnly, 2)
	assert.Equal(t, "HSP", activeOnly[0].Code)
	assert.Equal(t, "TNP", activeOnly[1].Code)
}

func TestPlantRepo_Update(t *testing.T) {
	pool, cleanup := setupPostgres(t)
	defer cleanup()
	repo := postgres.NewPlantRepo(pool)
	ctx := context.Background()

	p := &plants.Plant{Code: "UPD", Label: "Before", Address: "Old Addr", Active: true, SortOrder: 1}
	require.NoError(t, repo.Create(ctx, p))

	p.Label = "After"
	p.Address = "New Addr"
	p.Active = false
	p.SortOrder = 99
	require.NoError(t, repo.Update(ctx, p))

	got, err := repo.Get(ctx, "UPD")
	require.NoError(t, err)
	assert.Equal(t, "After", got.Label)
	assert.Equal(t, "New Addr", got.Address)
	assert.False(t, got.Active)
	assert.Equal(t, 99, got.SortOrder)
}

func TestPlantRepo_UpdateNotFound(t *testing.T) {
	pool, cleanup := setupPostgres(t)
	defer cleanup()
	repo := postgres.NewPlantRepo(pool)
	err := repo.Update(context.Background(), &plants.Plant{Code: "GHOST", Label: "x"})
	assert.ErrorIs(t, err, plants.ErrPlantNotFound)
}

// Closed pool exercises the generic DB-error return paths (non-PK errors) and
// drives isPKConflict/contains/indexString down the no-match branch.
func TestPlantRepo_QueryErrors(t *testing.T) {
	pool, cleanup := setupPostgres(t)
	defer cleanup()
	repo := postgres.NewPlantRepo(pool)
	ctx := context.Background()
	pool.Close()

	_, err := repo.List(ctx, false)
	assert.Error(t, err)
	assert.NotErrorIs(t, err, plants.ErrPlantNotFound)

	_, err = repo.Get(ctx, "X")
	assert.Error(t, err)
	assert.NotErrorIs(t, err, plants.ErrPlantNotFound)

	err = repo.Create(ctx, &plants.Plant{Code: "X", Label: "x"})
	assert.Error(t, err)
	assert.NotErrorIs(t, err, plants.ErrDuplicateCode)

	err = repo.Update(ctx, &plants.Plant{Code: "X", Label: "x"})
	assert.Error(t, err)
	assert.NotErrorIs(t, err, plants.ErrPlantNotFound)
}
