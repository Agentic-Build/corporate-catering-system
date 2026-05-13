package postgres_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/takalawang/corporate-catering-system/services/api/internal/menu"
	"github.com/takalawang/corporate-catering-system/services/api/internal/menu/postgres"
)

func TestCategoryRepo_CreateAndList(t *testing.T) {
	pool, cleanup := setupPostgres(t)
	defer cleanup()
	vendorID := seedApprovedVendor(t, pool)
	repo := postgres.NewCategoryRepo(pool)
	ctx := context.Background()

	c1 := &menu.Category{VendorID: vendorID, Name: "熱門便當", SortOrder: 0}
	c2 := &menu.Category{VendorID: vendorID, Name: "健康低卡", SortOrder: 1}
	require.NoError(t, repo.Create(ctx, c1))
	require.NoError(t, repo.Create(ctx, c2))
	require.NotEmpty(t, c1.ID)
	require.False(t, c1.CreatedAt.IsZero())

	list, err := repo.ListByVendor(ctx, vendorID)
	require.NoError(t, err)
	require.Len(t, list, 2)
	assert.Equal(t, "熱門便當", list[0].Name)
	assert.Equal(t, "健康低卡", list[1].Name)
}

func TestCategoryRepo_Update(t *testing.T) {
	pool, cleanup := setupPostgres(t)
	defer cleanup()
	vendorID := seedApprovedVendor(t, pool)
	repo := postgres.NewCategoryRepo(pool)
	ctx := context.Background()

	c := &menu.Category{VendorID: vendorID, Name: "A", SortOrder: 0}
	require.NoError(t, repo.Create(ctx, c))
	c.Name = "A-updated"
	c.SortOrder = 5
	require.NoError(t, repo.Update(ctx, c))

	got, err := repo.GetByID(ctx, c.ID)
	require.NoError(t, err)
	assert.Equal(t, "A-updated", got.Name)
	assert.Equal(t, 5, got.SortOrder)
}

func TestCategoryRepo_Delete(t *testing.T) {
	pool, cleanup := setupPostgres(t)
	defer cleanup()
	vendorID := seedApprovedVendor(t, pool)
	repo := postgres.NewCategoryRepo(pool)
	ctx := context.Background()

	c := &menu.Category{VendorID: vendorID, Name: "X", SortOrder: 0}
	require.NoError(t, repo.Create(ctx, c))
	require.NoError(t, repo.Delete(ctx, c.ID))
	_, err := repo.GetByID(ctx, c.ID)
	assert.ErrorIs(t, err, menu.ErrCategoryNotFound)
}
