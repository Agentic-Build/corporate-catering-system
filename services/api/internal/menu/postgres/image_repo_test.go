package postgres_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/takalawang/corporate-catering-system/services/api/internal/menu"
	"github.com/takalawang/corporate-catering-system/services/api/internal/menu/postgres"
)

func TestImageRepo_AddAndList(t *testing.T) {
	pool, cleanup := setupPostgres(t)
	defer cleanup()
	vendorID := seedApprovedVendor(t, pool)
	itemRepo := postgres.NewItemRepo(pool)
	imgRepo := postgres.NewImageRepo(pool)
	ctx := context.Background()

	item := &menu.Item{VendorID: vendorID, Name: "X", PriceMinor: 100, Status: menu.ItemStatusActive, Tags: []string{}, Badges: []string{}}
	require.NoError(t, itemRepo.Create(ctx, item))

	require.NoError(t, imgRepo.Add(ctx, &menu.Image{ItemID: item.ID, BlobURI: "s3://b/1.jpg", Alt: "a", SortOrder: 0}))
	require.NoError(t, imgRepo.Add(ctx, &menu.Image{ItemID: item.ID, BlobURI: "s3://b/2.jpg", Alt: "b", SortOrder: 1}))

	imgs, err := imgRepo.ListByItem(ctx, item.ID)
	require.NoError(t, err)
	require.Len(t, imgs, 2)
	assert.Equal(t, "s3://b/1.jpg", imgs[0].BlobURI)
	assert.Equal(t, "s3://b/2.jpg", imgs[1].BlobURI)
}

func TestImageRepo_Remove(t *testing.T) {
	pool, cleanup := setupPostgres(t)
	defer cleanup()
	vendorID := seedApprovedVendor(t, pool)
	itemRepo := postgres.NewItemRepo(pool)
	imgRepo := postgres.NewImageRepo(pool)
	ctx := context.Background()

	item := &menu.Item{VendorID: vendorID, Name: "X", PriceMinor: 100, Status: menu.ItemStatusActive, Tags: []string{}, Badges: []string{}}
	require.NoError(t, itemRepo.Create(ctx, item))

	img := &menu.Image{ItemID: item.ID, BlobURI: "s3://b/1.jpg", Alt: "a", SortOrder: 0}
	require.NoError(t, imgRepo.Add(ctx, img))

	require.NoError(t, imgRepo.Remove(ctx, img.ID))
	imgs, err := imgRepo.ListByItem(ctx, item.ID)
	require.NoError(t, err)
	assert.Empty(t, imgs)
}
