package menu_test

import (
	"context"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/takalawang/corporate-catering-system/services/api/internal/menu"
)

// ----- Mocks -----

type fakeCategoryRepo struct {
	mu     sync.Mutex
	byID   map[string]*menu.Category
	nextID int
}

func newFakeCategoryRepo() *fakeCategoryRepo {
	return &fakeCategoryRepo{byID: map[string]*menu.Category{}}
}

func (r *fakeCategoryRepo) Create(_ context.Context, c *menu.Category) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.nextID++
	c.ID = "cat-" + strconv.Itoa(r.nextID)
	c.CreatedAt = time.Now().UTC()
	r.byID[c.ID] = c
	return nil
}

func (r *fakeCategoryRepo) Update(_ context.Context, c *menu.Category) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.byID[c.ID]; !ok {
		return menu.ErrCategoryNotFound
	}
	r.byID[c.ID] = c
	return nil
}

func (r *fakeCategoryRepo) Delete(_ context.Context, id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.byID[id]; !ok {
		return menu.ErrCategoryNotFound
	}
	delete(r.byID, id)
	return nil
}

func (r *fakeCategoryRepo) GetByID(_ context.Context, id string) (*menu.Category, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if c, ok := r.byID[id]; ok {
		return c, nil
	}
	return nil, menu.ErrCategoryNotFound
}

func (r *fakeCategoryRepo) ListByVendor(_ context.Context, vendorID string) ([]*menu.Category, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []*menu.Category
	for _, c := range r.byID {
		if c.VendorID == vendorID {
			out = append(out, c)
		}
	}
	return out, nil
}

type fakeItemRepo struct {
	mu            sync.Mutex
	byID          map[string]*menu.Item
	activeByPlant map[string][]*menu.ActiveItemRow
	nextID        int
	lastFilter    menu.EmployeeMenuFilter // captures the most recent ListActiveByPlant arg
}

func newFakeItemRepo() *fakeItemRepo {
	return &fakeItemRepo{byID: map[string]*menu.Item{}, activeByPlant: map[string][]*menu.ActiveItemRow{}}
}

func (r *fakeItemRepo) Create(_ context.Context, i *menu.Item) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.nextID++
	i.ID = "item-" + strconv.Itoa(r.nextID)
	i.CreatedAt = time.Now().UTC()
	i.UpdatedAt = i.CreatedAt
	r.byID[i.ID] = i
	return nil
}

func (r *fakeItemRepo) Update(_ context.Context, i *menu.Item) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.byID[i.ID]; !ok {
		return menu.ErrItemNotFound
	}
	i.UpdatedAt = time.Now().UTC()
	r.byID[i.ID] = i
	return nil
}

func (r *fakeItemRepo) SetStatus(_ context.Context, id string, s menu.ItemStatus) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	i, ok := r.byID[id]
	if !ok {
		return menu.ErrItemNotFound
	}
	i.Status = s
	if s == menu.ItemStatusArchived {
		now := time.Now().UTC()
		i.ArchivedAt = &now
	} else {
		i.ArchivedAt = nil
	}
	return nil
}

func (r *fakeItemRepo) GetByID(_ context.Context, id string) (*menu.Item, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if i, ok := r.byID[id]; ok {
		return i, nil
	}
	return nil, menu.ErrItemNotFound
}

func (r *fakeItemRepo) ListByVendor(_ context.Context, vendorID string, includeArchived bool) ([]*menu.MerchantItemRow, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []*menu.MerchantItemRow
	for _, i := range r.byID {
		if i.VendorID != vendorID {
			continue
		}
		if !includeArchived && i.Status == menu.ItemStatusArchived {
			continue
		}
		out = append(out, &menu.MerchantItemRow{Item: *i})
	}
	return out, nil
}

func (r *fakeItemRepo) ListActiveByPlant(_ context.Context, f menu.EmployeeMenuFilter) ([]*menu.ActiveItemRow, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.lastFilter = f
	return r.activeByPlant[f.Plant], nil
}

type fakeImageRepo struct {
	mu     sync.Mutex
	byItem map[string][]*menu.Image
	nextID int
}

func newFakeImageRepo() *fakeImageRepo {
	return &fakeImageRepo{byItem: map[string][]*menu.Image{}}
}

func (r *fakeImageRepo) Add(_ context.Context, img *menu.Image) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.nextID++
	img.ID = "img-" + strconv.Itoa(r.nextID)
	img.CreatedAt = time.Now().UTC()
	r.byItem[img.ItemID] = append(r.byItem[img.ItemID], img)
	return nil
}

func (r *fakeImageRepo) Remove(_ context.Context, id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for itemID, imgs := range r.byItem {
		for i, im := range imgs {
			if im.ID == id {
				r.byItem[itemID] = append(imgs[:i], imgs[i+1:]...)
				return nil
			}
		}
	}
	return menu.ErrImageNotFound
}

func (r *fakeImageRepo) ListByItem(_ context.Context, itemID string) ([]*menu.Image, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.byItem[itemID], nil
}

func (r *fakeImageRepo) ReplaceForItem(_ context.Context, itemID string, uris []string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	imgs := make([]*menu.Image, 0, len(uris))
	for i, uri := range uris {
		r.nextID++
		imgs = append(imgs, &menu.Image{
			ID:        "img-" + strconv.Itoa(r.nextID),
			ItemID:    itemID,
			BlobURI:   uri,
			SortOrder: i,
			CreatedAt: time.Now().UTC(),
		})
	}
	r.byItem[itemID] = imgs
	return nil
}

// ----- Helpers -----

func newSvc() (*menu.Service, *fakeCategoryRepo, *fakeItemRepo, *fakeImageRepo) {
	cr := newFakeCategoryRepo()
	ir := newFakeItemRepo()
	gr := newFakeImageRepo()
	return &menu.Service{Categories: cr, Items: ir, Images: gr}, cr, ir, gr
}

// ----- Tests -----

func TestService_CreateItem_DefaultsToDraftAndNormalizesSlices(t *testing.T) {
	svc, _, _, _ := newSvc()
	got, err := svc.CreateItem(context.Background(), menu.CreateItemInput{
		VendorID:   "v1",
		Name:       "雞腿便當",
		PriceMinor: 11000,
		// Tags/Badges left nil
	})
	require.NoError(t, err)
	assert.NotEmpty(t, got.ID)
	assert.Equal(t, menu.ItemStatusDraft, got.Status)
	assert.NotNil(t, got.Tags)
	assert.Equal(t, []string{}, got.Tags)
	assert.NotNil(t, got.Badges)
	assert.Equal(t, []string{}, got.Badges)
}

func TestService_CreateItem_PersistsImages(t *testing.T) {
	svc, _, _, gr := newSvc()
	ctx := context.Background()
	got, err := svc.CreateItem(ctx, menu.CreateItemInput{
		VendorID:   "v1",
		Name:       "雞腿便當",
		PriceMinor: 11000,
		Images:     []string{"s3://b/1.jpg", "s3://b/2.jpg"},
	})
	require.NoError(t, err)
	imgs, err := gr.ListByItem(ctx, got.ID)
	require.NoError(t, err)
	require.Len(t, imgs, 2)
	assert.Equal(t, "s3://b/1.jpg", imgs[0].BlobURI)
	assert.Equal(t, "s3://b/2.jpg", imgs[1].BlobURI)
}

func TestService_CreateItem_NoImagesLeavesNone(t *testing.T) {
	svc, _, _, gr := newSvc()
	ctx := context.Background()
	got, err := svc.CreateItem(ctx, menu.CreateItemInput{
		VendorID: "v1", Name: "X", PriceMinor: 9000,
	})
	require.NoError(t, err)
	imgs, err := gr.ListByItem(ctx, got.ID)
	require.NoError(t, err)
	assert.Empty(t, imgs)
}

func TestService_UpdateItem_ReplacesImages(t *testing.T) {
	svc, _, _, gr := newSvc()
	ctx := context.Background()
	created, err := svc.CreateItem(ctx, menu.CreateItemInput{
		VendorID: "v1", Name: "X", PriceMinor: 9000,
		Images: []string{"s3://b/old.jpg"},
	})
	require.NoError(t, err)

	_, err = svc.UpdateItem(ctx, created.ID, "v1", menu.UpdateItemInput{
		Name: "X", PriceMinor: 9000,
		Images: []string{"s3://b/new1.jpg", "s3://b/new2.jpg"},
	})
	require.NoError(t, err)

	imgs, err := gr.ListByItem(ctx, created.ID)
	require.NoError(t, err)
	require.Len(t, imgs, 2)
	assert.Equal(t, "s3://b/new1.jpg", imgs[0].BlobURI)
	assert.Equal(t, "s3://b/new2.jpg", imgs[1].BlobURI)
}

func TestService_UpdateItem_WrongVendorIsForbidden(t *testing.T) {
	svc, _, _, _ := newSvc()
	created, err := svc.CreateItem(context.Background(), menu.CreateItemInput{
		VendorID: "v-owner", Name: "X", PriceMinor: 9000,
	})
	require.NoError(t, err)
	_, err = svc.UpdateItem(context.Background(), created.ID, "v-other", menu.UpdateItemInput{
		Name: "X", PriceMinor: 9000,
	})
	assert.ErrorIs(t, err, menu.ErrForbidden)
}

func TestService_Publish_WrongVendorIsForbidden(t *testing.T) {
	svc, _, _, _ := newSvc()
	created, _ := svc.CreateItem(context.Background(), menu.CreateItemInput{
		VendorID: "v-owner", Name: "X", PriceMinor: 9000,
	})
	err := svc.Publish(context.Background(), created.ID, "v-other")
	assert.ErrorIs(t, err, menu.ErrForbidden)
}

func TestService_CopyItem_CreatesIndependentDraft(t *testing.T) {
	svc, _, _, _ := newSvc()
	src, err := svc.CreateItem(context.Background(), menu.CreateItemInput{
		VendorID: "v1", Name: "雞腿便當", Description: "經典", PriceMinor: 11000,
		Tags: []string{"halal"}, Badges: []string{"hot"},
	})
	require.NoError(t, err)
	require.NoError(t, svc.Publish(context.Background(), src.ID, "v1")) // source is active

	copied, err := svc.CopyItem(context.Background(), src.ID, "v1")
	require.NoError(t, err)
	assert.NotEqual(t, src.ID, copied.ID)
	assert.Equal(t, "雞腿便當（複製）", copied.Name)
	assert.Equal(t, menu.ItemStatusDraft, copied.Status, "copy is a draft even when source is active")
	assert.Equal(t, src.PriceMinor, copied.PriceMinor)
	assert.Equal(t, src.Description, copied.Description)
	assert.Equal(t, []string{"halal"}, copied.Tags)
	assert.Equal(t, []string{"hot"}, copied.Badges)
}

func TestService_CopyItem_WrongVendorIsForbidden(t *testing.T) {
	svc, _, _, _ := newSvc()
	src, err := svc.CreateItem(context.Background(), menu.CreateItemInput{
		VendorID: "v-owner", Name: "X", PriceMinor: 9000,
	})
	require.NoError(t, err)
	_, err = svc.CopyItem(context.Background(), src.ID, "v-other")
	assert.ErrorIs(t, err, menu.ErrForbidden)
}

func TestService_Archive_OwnVendor_Succeeds(t *testing.T) {
	svc, _, ir, _ := newSvc()
	created, _ := svc.CreateItem(context.Background(), menu.CreateItemInput{
		VendorID: "v-owner", Name: "X", PriceMinor: 9000,
	})
	require.NoError(t, svc.Archive(context.Background(), created.ID, "v-owner"))
	got, _ := ir.GetByID(context.Background(), created.ID)
	assert.Equal(t, menu.ItemStatusArchived, got.Status)
	assert.NotNil(t, got.ArchivedAt)
}

func TestService_Publish_OwnVendor_Succeeds(t *testing.T) {
	svc, _, ir, _ := newSvc()
	created, _ := svc.CreateItem(context.Background(), menu.CreateItemInput{
		VendorID: "v-owner", Name: "X", PriceMinor: 9000,
	})
	require.NoError(t, svc.Publish(context.Background(), created.ID, "v-owner"))
	got, _ := ir.GetByID(context.Background(), created.ID)
	assert.Equal(t, menu.ItemStatusActive, got.Status)
}

func TestService_ListForEmployee_JoinsImagesAndMarksSoldOut(t *testing.T) {
	svc, _, ir, gr := newSvc()
	ctx := context.Background()

	// Seed an item and two images for it via the fake repos.
	item := &menu.Item{
		ID: "item-99", VendorID: "v1", Name: "排骨便當", PriceMinor: 12000,
		Tags: []string{"招牌"}, Badges: []string{"熱賣"}, Status: menu.ItemStatusActive,
	}
	ir.byID[item.ID] = item
	require.NoError(t, gr.Add(ctx, &menu.Image{ItemID: item.ID, BlobURI: "blob://1"}))
	require.NoError(t, gr.Add(ctx, &menu.Image{ItemID: item.ID, BlobURI: "blob://2"}))

	day := time.Date(2026, 5, 14, 0, 0, 0, 0, time.UTC)
	ir.activeByPlant["F12B-3F"] = []*menu.ActiveItemRow{
		{
			Item:         *item,
			VendorName:   "稻禾家便當",
			SupplyDate:   day,
			Capacity:     50,
			Remain:       0, // sold out
			PickupWindow: "12:00-12:30",
			ETALabel:     "10 分鐘後可取",
		},
	}

	out, err := svc.ListForEmployee(ctx, menu.EmployeeMenuFilter{Plant: "F12B-3F", Day: day})
	require.NoError(t, err)
	require.Len(t, out, 1)
	got := out[0]
	assert.Equal(t, "item-99", got.ID)
	assert.Equal(t, "稻禾家便當", got.VendorName)
	assert.True(t, got.SoldOut)
	assert.Equal(t, 0, got.Remain)
	assert.Equal(t, 50, got.Capacity)
	assert.Equal(t, []string{"blob://1", "blob://2"}, got.Images)
	assert.Equal(t, []string{"招牌"}, got.Tags)
	assert.Equal(t, "12:00-12:30", got.PickupWindow)
}

func TestService_ListForEmployee_RespectsSoldOutFlag(t *testing.T) {
	svc, _, ir, _ := newSvc()
	ctx := context.Background()
	item := &menu.Item{
		ID: "item-7", VendorID: "v1", Name: "雞排飯", PriceMinor: 9000, Status: menu.ItemStatusActive,
	}
	ir.byID[item.ID] = item
	day := time.Date(2026, 5, 14, 0, 0, 0, 0, time.UTC)
	ir.activeByPlant["F12B-3F"] = []*menu.ActiveItemRow{
		{Item: *item, VendorName: "X", SupplyDate: day, Capacity: 50, Remain: 20, SoldOut: true},
	}
	out, err := svc.ListForEmployee(ctx, menu.EmployeeMenuFilter{Plant: "F12B-3F", Day: day})
	require.NoError(t, err)
	require.Len(t, out, 1)
	assert.True(t, out[0].SoldOut, "sold_out flag marks the item unavailable even with remain>0")
	assert.Equal(t, 20, out[0].Remain)
}

func TestService_ListForEmployee_PassesFilterToRepo(t *testing.T) {
	svc, _, ir, _ := newSvc()
	ctx := context.Background()
	day := time.Date(2026, 5, 14, 0, 0, 0, 0, time.UTC)
	priceMin := int64(8000)
	inStock := true

	f := menu.EmployeeMenuFilter{
		Plant:    "F12B-3F",
		Day:      day,
		Q:        "雞腿",
		Tags:     []string{"低卡", "高蛋白"},
		PriceMin: &priceMin,
		InStock:  &inStock,
		Sort:     menu.EmployeeMenuSortPriceAsc,
	}
	_, err := svc.ListForEmployee(ctx, f)
	require.NoError(t, err)

	// The service must push the filter through to the repository untouched;
	// all search/filter/sort work happens in repo SQL.
	assert.Equal(t, f, ir.lastFilter)
}

func TestService_CreateCategory_And_List(t *testing.T) {
	svc, _, _, _ := newSvc()
	ctx := context.Background()
	c, err := svc.CreateCategory(ctx, menu.CreateCategoryInput{VendorID: "v1", Name: "主食", SortOrder: 1})
	require.NoError(t, err)
	assert.NotEmpty(t, c.ID)
	list, err := svc.ListCategories(ctx, "v1")
	require.NoError(t, err)
	assert.Len(t, list, 1)
}
