package menu_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Agentic-Build/corporate-catering-system/services/api/internal/menu"
)

// === Error-injecting fakes (only the methods the gap tests exercise) ===

type errCategoryRepo struct {
	*fakeCategoryRepo
	createErr error
}

func (r *errCategoryRepo) Create(ctx context.Context, c *menu.Category) error {
	if r.createErr != nil {
		return r.createErr
	}
	return r.fakeCategoryRepo.Create(ctx, c)
}

type errItemRepo struct {
	*fakeItemRepo
	createErr   error
	listVendErr error
}

func (r *errItemRepo) Create(ctx context.Context, i *menu.Item) error {
	if r.createErr != nil {
		return r.createErr
	}
	return r.fakeItemRepo.Create(ctx, i)
}

func (r *errItemRepo) ListByVendor(ctx context.Context, vendorID string, includeArchived bool) ([]*menu.MerchantItemRow, error) {
	if r.listVendErr != nil {
		return nil, r.listVendErr
	}
	return r.fakeItemRepo.ListByVendor(ctx, vendorID, includeArchived)
}

type errImageRepo struct {
	*fakeImageRepo
	replaceErr error
	listErr    error
}

func (r *errImageRepo) ReplaceForItem(ctx context.Context, itemID string, uris []string) error {
	if r.replaceErr != nil {
		return r.replaceErr
	}
	return r.fakeImageRepo.ReplaceForItem(ctx, itemID, uris)
}

func (r *errImageRepo) ListByItem(ctx context.Context, itemID string) ([]*menu.Image, error) {
	if r.listErr != nil {
		return nil, r.listErr
	}
	return r.fakeImageRepo.ListByItem(ctx, itemID)
}

// === CreateCategory ===

func TestService_CreateCategory_PropagatesRepoError(t *testing.T) {
	cr := &errCategoryRepo{fakeCategoryRepo: newFakeCategoryRepo(), createErr: errors.New("boom")}
	svc := &menu.Service{Categories: cr, Items: newFakeItemRepo(), Images: newFakeImageRepo()}
	_, err := svc.CreateCategory(context.Background(), menu.CreateCategoryInput{VendorID: "v1", Name: "主食"})
	assert.Error(t, err)
}

// === CreateItem error branches ===

func TestService_CreateItem_PropagatesCreateError(t *testing.T) {
	ir := &errItemRepo{fakeItemRepo: newFakeItemRepo(), createErr: errors.New("boom")}
	svc := &menu.Service{Categories: newFakeCategoryRepo(), Items: ir, Images: newFakeImageRepo()}
	_, err := svc.CreateItem(context.Background(), menu.CreateItemInput{VendorID: "v1", Name: "X", PriceMinor: 9000})
	assert.Error(t, err)
}

func TestService_CreateItem_PropagatesImageReplaceError(t *testing.T) {
	gr := &errImageRepo{fakeImageRepo: newFakeImageRepo(), replaceErr: errors.New("boom")}
	svc := &menu.Service{Categories: newFakeCategoryRepo(), Items: newFakeItemRepo(), Images: gr}
	_, err := svc.CreateItem(context.Background(), menu.CreateItemInput{
		VendorID: "v1", Name: "X", PriceMinor: 9000, Images: []string{"s3://b/1.jpg"},
	})
	assert.Error(t, err)
}

// === Archive not-found ===

func TestService_Archive_NotFound(t *testing.T) {
	svc, _, _, _ := newSvc()
	err := svc.Archive(context.Background(), "missing", "v1")
	assert.ErrorIs(t, err, menu.ErrItemNotFound)
}

func TestService_Archive_WrongVendorIsForbidden(t *testing.T) {
	svc, _, _, _ := newSvc()
	created, err := svc.CreateItem(context.Background(), menu.CreateItemInput{VendorID: "v-owner", Name: "X", PriceMinor: 9000})
	require.NoError(t, err)
	err = svc.Archive(context.Background(), created.ID, "v-other")
	assert.ErrorIs(t, err, menu.ErrForbidden)
}

// === ListByVendor ===

func TestService_ListByVendor_ReturnsRows(t *testing.T) {
	svc, _, _, _ := newSvc()
	ctx := context.Background()
	_, err := svc.CreateItem(ctx, menu.CreateItemInput{VendorID: "v1", Name: "A", PriceMinor: 9000})
	require.NoError(t, err)
	archived, err := svc.CreateItem(ctx, menu.CreateItemInput{VendorID: "v1", Name: "B", PriceMinor: 9000})
	require.NoError(t, err)
	require.NoError(t, svc.Archive(ctx, archived.ID, "v1"))

	active, err := svc.ListByVendor(ctx, "v1", false)
	require.NoError(t, err)
	assert.Len(t, active, 1)

	all, err := svc.ListByVendor(ctx, "v1", true)
	require.NoError(t, err)
	assert.Len(t, all, 2)
}

func TestService_ListByVendor_PropagatesError(t *testing.T) {
	ir := &errItemRepo{fakeItemRepo: newFakeItemRepo(), listVendErr: errors.New("boom")}
	svc := &menu.Service{Categories: newFakeCategoryRepo(), Items: ir, Images: newFakeImageRepo()}
	_, err := svc.ListByVendor(context.Background(), "v1", false)
	assert.Error(t, err)
}

// === ListImagesByItem ===

func TestService_ListImagesByItem_ReturnsImages(t *testing.T) {
	svc, _, _, gr := newSvc()
	ctx := context.Background()
	require.NoError(t, gr.Add(ctx, &menu.Image{ItemID: "item-1", BlobURI: "blob://1", CreatedAt: time.Now().UTC()}))
	imgs, err := svc.ListImagesByItem(ctx, "item-1")
	require.NoError(t, err)
	require.Len(t, imgs, 1)
	assert.Equal(t, "blob://1", imgs[0].BlobURI)
}
