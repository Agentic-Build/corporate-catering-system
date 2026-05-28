package postgres_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Agentic-Build/corporate-catering-system/services/api/internal/menu"
	"github.com/Agentic-Build/corporate-catering-system/services/api/internal/menu/postgres"
)

func TestImageRepo_AddAndList(t *testing.T) {
	pool, cleanup := setupPostgres(t)
	defer cleanup()
	vendorID := seedApprovedVendor(t, pool)
	itemRepo := postgres.NewItemRepo(pool)
	imgRepo := postgres.NewImageRepo(pool)
	ctx := context.Background()

	item := &menu.Item{VendorID: vendorID, Name: "X", PriceMinor: 100, Status: menu.ItemStatusActive, Tags: []string{}}
	require.NoError(t, itemRepo.Create(ctx, item))

	require.NoError(t, imgRepo.Add(ctx, &menu.Image{ItemID: item.ID, BlobURI: "s3://b/1.jpg", Alt: "a", SortOrder: 0}))
	require.NoError(t, imgRepo.Add(ctx, &menu.Image{ItemID: item.ID, BlobURI: "s3://b/2.jpg", Alt: "b", SortOrder: 1}))

	imgs, err := imgRepo.ListByItem(ctx, item.ID)
	require.NoError(t, err)
	require.Len(t, imgs, 2)
	assert.Equal(t, "s3://b/1.jpg", imgs[0].BlobURI)
	assert.Equal(t, "s3://b/2.jpg", imgs[1].BlobURI)
}

func TestImageRepo_ListByItems_GroupsAndBatches(t *testing.T) {
	pool, cleanup := setupPostgres(t)
	defer cleanup()
	vendorID := seedApprovedVendor(t, pool)
	itemRepo := postgres.NewItemRepo(pool)
	imgRepo := postgres.NewImageRepo(pool)
	ctx := context.Background()

	withImgs := &menu.Item{VendorID: vendorID, Name: "A", PriceMinor: 100, Status: menu.ItemStatusActive, Tags: []string{}}
	other := &menu.Item{VendorID: vendorID, Name: "B", PriceMinor: 100, Status: menu.ItemStatusActive, Tags: []string{}}
	noImgs := &menu.Item{VendorID: vendorID, Name: "C", PriceMinor: 100, Status: menu.ItemStatusActive, Tags: []string{}}
	require.NoError(t, itemRepo.Create(ctx, withImgs))
	require.NoError(t, itemRepo.Create(ctx, other))
	require.NoError(t, itemRepo.Create(ctx, noImgs))

	require.NoError(t, imgRepo.Add(ctx, &menu.Image{ItemID: withImgs.ID, BlobURI: "s3://b/1.jpg", SortOrder: 0}))
	require.NoError(t, imgRepo.Add(ctx, &menu.Image{ItemID: withImgs.ID, BlobURI: "s3://b/2.jpg", SortOrder: 1}))
	require.NoError(t, imgRepo.Add(ctx, &menu.Image{ItemID: other.ID, BlobURI: "s3://b/x.jpg", SortOrder: 0}))

	got, err := imgRepo.ListByItems(ctx, []string{withImgs.ID, other.ID, noImgs.ID})
	require.NoError(t, err)
	require.Len(t, got[withImgs.ID], 2)
	assert.Equal(t, "s3://b/1.jpg", got[withImgs.ID][0].BlobURI)
	assert.Equal(t, "s3://b/2.jpg", got[withImgs.ID][1].BlobURI)
	require.Len(t, got[other.ID], 1)
	assert.NotContains(t, got, noImgs.ID, "items with no images are absent")

	empty, err := imgRepo.ListByItems(ctx, nil)
	require.NoError(t, err)
	assert.Empty(t, empty)
}

func TestImageRepo_ReplaceForItem(t *testing.T) {
	pool, cleanup := setupPostgres(t)
	defer cleanup()
	vendorID := seedApprovedVendor(t, pool)
	itemRepo := postgres.NewItemRepo(pool)
	imgRepo := postgres.NewImageRepo(pool)
	ctx := context.Background()

	item := &menu.Item{VendorID: vendorID, Name: "X", PriceMinor: 100, Status: menu.ItemStatusActive, Tags: []string{}}
	require.NoError(t, itemRepo.Create(ctx, item))

	// Seed two images, then replace with a different set.
	require.NoError(t, imgRepo.Add(ctx, &menu.Image{ItemID: item.ID, BlobURI: "s3://b/old1.jpg", SortOrder: 0}))
	require.NoError(t, imgRepo.Add(ctx, &menu.Image{ItemID: item.ID, BlobURI: "s3://b/old2.jpg", SortOrder: 1}))

	require.NoError(t, imgRepo.ReplaceForItem(ctx, item.ID, []string{"s3://b/new1.jpg", "s3://b/new2.jpg", "s3://b/new3.jpg"}))

	imgs, err := imgRepo.ListByItem(ctx, item.ID)
	require.NoError(t, err)
	require.Len(t, imgs, 3)
	assert.Equal(t, "s3://b/new1.jpg", imgs[0].BlobURI)
	assert.Equal(t, 0, imgs[0].SortOrder)
	assert.Equal(t, "s3://b/new2.jpg", imgs[1].BlobURI)
	assert.Equal(t, 1, imgs[1].SortOrder)
	assert.Equal(t, "s3://b/new3.jpg", imgs[2].BlobURI)
	assert.Equal(t, 2, imgs[2].SortOrder)
}

func TestImageRepo_ReplaceForItem_EmptyClearsAll(t *testing.T) {
	pool, cleanup := setupPostgres(t)
	defer cleanup()
	vendorID := seedApprovedVendor(t, pool)
	itemRepo := postgres.NewItemRepo(pool)
	imgRepo := postgres.NewImageRepo(pool)
	ctx := context.Background()

	item := &menu.Item{VendorID: vendorID, Name: "X", PriceMinor: 100, Status: menu.ItemStatusActive, Tags: []string{}}
	require.NoError(t, itemRepo.Create(ctx, item))
	require.NoError(t, imgRepo.Add(ctx, &menu.Image{ItemID: item.ID, BlobURI: "s3://b/1.jpg", SortOrder: 0}))

	require.NoError(t, imgRepo.ReplaceForItem(ctx, item.ID, nil))

	imgs, err := imgRepo.ListByItem(ctx, item.ID)
	require.NoError(t, err)
	assert.Empty(t, imgs)
}

func TestImageRepo_Remove(t *testing.T) {
	pool, cleanup := setupPostgres(t)
	defer cleanup()
	vendorID := seedApprovedVendor(t, pool)
	itemRepo := postgres.NewItemRepo(pool)
	imgRepo := postgres.NewImageRepo(pool)
	ctx := context.Background()

	item := &menu.Item{VendorID: vendorID, Name: "X", PriceMinor: 100, Status: menu.ItemStatusActive, Tags: []string{}}
	require.NoError(t, itemRepo.Create(ctx, item))

	img := &menu.Image{ItemID: item.ID, BlobURI: "s3://b/1.jpg", Alt: "a", SortOrder: 0}
	require.NoError(t, imgRepo.Add(ctx, img))

	require.NoError(t, imgRepo.Remove(ctx, img.ID))
	imgs, err := imgRepo.ListByItem(ctx, item.ID)
	require.NoError(t, err)
	assert.Empty(t, imgs)
}
