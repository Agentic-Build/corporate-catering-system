package menu_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Agentic-Build/corporate-catering-system/services/api/internal/menu"
	"github.com/Agentic-Build/corporate-catering-system/services/api/internal/platform/clock"
)

// ============================================================================
// service.go — error branches and the imageURIsForRows batched path
// ============================================================================

// errItemRepo2 lets the gap tests inject errors on the read/update methods the
// existing errItemRepo doesn't cover (GetByID, Update, SetStatus,
// ListActiveByPlant).
type errItemRepo2 struct {
	*fakeItemRepo
	getErr        error
	updateErr     error
	listActiveErr error
}

func (r *errItemRepo2) GetByID(ctx context.Context, id string) (*menu.Item, error) {
	if r.getErr != nil {
		return nil, r.getErr
	}
	return r.fakeItemRepo.GetByID(ctx, id)
}

func (r *errItemRepo2) Update(ctx context.Context, i *menu.Item) error {
	if r.updateErr != nil {
		return r.updateErr
	}
	return r.fakeItemRepo.Update(ctx, i)
}

func (r *errItemRepo2) ListActiveByPlant(ctx context.Context, f menu.EmployeeMenuFilter) ([]*menu.ActiveItemRow, error) {
	if r.listActiveErr != nil {
		return nil, r.listActiveErr
	}
	return r.fakeItemRepo.ListActiveByPlant(ctx, f)
}

// --- UpdateItem error branches ---

func TestService_UpdateItem_GetByIDError(t *testing.T) {
	ir := &errItemRepo2{fakeItemRepo: newFakeItemRepo()}
	svc := &menu.Service{Categories: newFakeCategoryRepo(), Items: ir, Images: newFakeImageRepo()}
	_, err := svc.UpdateItem(context.Background(), "missing", "v1", menu.UpdateItemInput{Name: "X", PriceMinor: 9000})
	assert.ErrorIs(t, err, menu.ErrItemNotFound)
}

func TestService_UpdateItem_UpdateError(t *testing.T) {
	ir := &errItemRepo2{fakeItemRepo: newFakeItemRepo()}
	svc := &menu.Service{Categories: newFakeCategoryRepo(), Items: ir, Images: newFakeImageRepo()}
	created, err := svc.CreateItem(context.Background(), menu.CreateItemInput{VendorID: "v1", Name: "X", PriceMinor: 9000})
	require.NoError(t, err)
	ir.updateErr = errors.New("update boom")
	_, err = svc.UpdateItem(context.Background(), created.ID, "v1", menu.UpdateItemInput{Name: "Y", PriceMinor: 9000})
	assert.Error(t, err)
}

func TestService_UpdateItem_ImageReplaceError(t *testing.T) {
	gr := &errImageRepo{fakeImageRepo: newFakeImageRepo()}
	svc := &menu.Service{Categories: newFakeCategoryRepo(), Items: newFakeItemRepo(), Images: gr}
	created, err := svc.CreateItem(context.Background(), menu.CreateItemInput{VendorID: "v1", Name: "X", PriceMinor: 9000})
	require.NoError(t, err)
	gr.replaceErr = errors.New("replace boom")
	_, err = svc.UpdateItem(context.Background(), created.ID, "v1", menu.UpdateItemInput{
		Name: "X", PriceMinor: 9000, Images: []string{"s3://b/1.jpg"},
	})
	assert.Error(t, err)
}

func TestService_UpdateItem_NilTagsNormalised(t *testing.T) {
	svc, _, ir, _ := newSvc()
	created, err := svc.CreateItem(context.Background(), menu.CreateItemInput{VendorID: "v1", Name: "X", PriceMinor: 9000, Tags: []string{"old"}})
	require.NoError(t, err)
	// Tags left nil in the update input → must normalise to [].
	_, err = svc.UpdateItem(context.Background(), created.ID, "v1", menu.UpdateItemInput{Name: "X", PriceMinor: 9000})
	require.NoError(t, err)
	got, err := ir.GetByID(context.Background(), created.ID)
	require.NoError(t, err)
	assert.Equal(t, []string{}, got.Tags)
}

// --- CopyItem error branches ---

func TestService_CopyItem_GetByIDError(t *testing.T) {
	svc, _, _, _ := newSvc()
	_, err := svc.CopyItem(context.Background(), "missing", "v1")
	assert.ErrorIs(t, err, menu.ErrItemNotFound)
}

func TestService_CopyItem_ListImagesError(t *testing.T) {
	gr := &errImageRepo{fakeImageRepo: newFakeImageRepo()}
	svc := &menu.Service{Categories: newFakeCategoryRepo(), Items: newFakeItemRepo(), Images: gr}
	src, err := svc.CreateItem(context.Background(), menu.CreateItemInput{VendorID: "v1", Name: "X", PriceMinor: 9000})
	require.NoError(t, err)
	gr.listErr = errors.New("list boom")
	_, err = svc.CopyItem(context.Background(), src.ID, "v1")
	assert.Error(t, err)
}

// --- Publish GetByID error ---

func TestService_Publish_NotFound(t *testing.T) {
	svc, _, _, _ := newSvc()
	err := svc.Publish(context.Background(), "missing", "v1")
	assert.ErrorIs(t, err, menu.ErrItemNotFound)
}

// --- ListForEmployee error branches + nil-images path ---

func TestService_ListForEmployee_ListActiveError(t *testing.T) {
	ir := &errItemRepo2{fakeItemRepo: newFakeItemRepo(), listActiveErr: errors.New("boom")}
	svc := &menu.Service{Categories: newFakeCategoryRepo(), Items: ir, Images: newFakeImageRepo()}
	_, err := svc.ListForEmployee(context.Background(), menu.EmployeeMenuFilter{Plant: "P"})
	assert.Error(t, err)
}

func TestService_ListForEmployee_ImageListError(t *testing.T) {
	ir := newFakeItemRepo()
	gr := &errImageRepo{fakeImageRepo: newFakeImageRepo(), listErr: errors.New("boom")}
	svc := &menu.Service{Categories: newFakeCategoryRepo(), Items: ir, Images: gr}
	item := &menu.Item{ID: "item-1", VendorID: "v1", Name: "X", PriceMinor: 9000, Status: menu.ItemStatusActive}
	ir.byID[item.ID] = item
	ir.activeByPlant["P"] = []*menu.ActiveItemRow{{Item: *item, VendorName: "V"}}
	_, err := svc.ListForEmployee(context.Background(), menu.EmployeeMenuFilter{Plant: "P"})
	assert.Error(t, err)
}

func TestService_ListForEmployee_NilImagesAndNilTags(t *testing.T) {
	svc, _, ir, _ := newSvc()
	// Item with no images attached and nil Tags → uris/tags normalise to [].
	item := &menu.Item{ID: "item-1", VendorID: "v1", Name: "X", PriceMinor: 9000, Status: menu.ItemStatusActive}
	ir.byID[item.ID] = item
	ir.activeByPlant["P"] = []*menu.ActiveItemRow{{Item: *item, VendorName: "V", Remain: 5}}
	out, err := svc.ListForEmployee(context.Background(), menu.EmployeeMenuFilter{Plant: "P"})
	require.NoError(t, err)
	require.Len(t, out, 1)
	assert.Equal(t, []string{}, out[0].Images)
	assert.Equal(t, []string{}, out[0].Tags)
	assert.False(t, out[0].SoldOut)
}

// --- imageURIsForRows batched path (BatchImageLister) ---

// batchImageRepo implements both ImageRepository and BatchImageLister so the
// service takes the single-query branch.
type batchImageRepo struct {
	*fakeImageRepo
	byItems    map[string][]*menu.Image
	byItemsErr error
}

func (r *batchImageRepo) ListByItems(_ context.Context, _ []string) (map[string][]*menu.Image, error) {
	if r.byItemsErr != nil {
		return nil, r.byItemsErr
	}
	return r.byItems, nil
}

func TestService_ListForEmployee_BatchImageLister(t *testing.T) {
	ir := newFakeItemRepo()
	br := &batchImageRepo{
		fakeImageRepo: newFakeImageRepo(),
		byItems: map[string][]*menu.Image{
			"item-1": {{BlobURI: "blob://a"}, {BlobURI: "blob://b"}},
		},
	}
	svc := &menu.Service{Categories: newFakeCategoryRepo(), Items: ir, Images: br}
	i1 := &menu.Item{ID: "item-1", VendorID: "v1", Name: "A", PriceMinor: 9000, Status: menu.ItemStatusActive, Tags: []string{"t"}}
	i2 := &menu.Item{ID: "item-2", VendorID: "v1", Name: "B", PriceMinor: 9000, Status: menu.ItemStatusActive}
	ir.byID[i1.ID] = i1
	ir.byID[i2.ID] = i2
	ir.activeByPlant["P"] = []*menu.ActiveItemRow{
		{Item: *i1, VendorName: "V", Remain: 3},
		{Item: *i2, VendorName: "V", Remain: 2},
	}
	out, err := svc.ListForEmployee(context.Background(), menu.EmployeeMenuFilter{Plant: "P"})
	require.NoError(t, err)
	require.Len(t, out, 2)
	byID := map[string][]string{}
	for _, o := range out {
		byID[o.ID] = o.Images
	}
	assert.Equal(t, []string{"blob://a", "blob://b"}, byID["item-1"])
	// item-2 had no batched images → normalised to [].
	assert.Equal(t, []string{}, byID["item-2"])
}

func TestService_ListForEmployee_BatchImageListerError(t *testing.T) {
	ir := newFakeItemRepo()
	br := &batchImageRepo{fakeImageRepo: newFakeImageRepo(), byItemsErr: errors.New("batch boom")}
	svc := &menu.Service{Categories: newFakeCategoryRepo(), Items: ir, Images: br}
	item := &menu.Item{ID: "item-1", VendorID: "v1", Name: "A", PriceMinor: 9000, Status: menu.ItemStatusActive}
	ir.byID[item.ID] = item
	ir.activeByPlant["P"] = []*menu.ActiveItemRow{{Item: *item, VendorName: "V", Remain: 1}}
	_, err := svc.ListForEmployee(context.Background(), menu.EmployeeMenuFilter{Plant: "P"})
	assert.Error(t, err)
}

// ============================================================================
// home_service.go — Compute / nextOrderableDay / serverTZ error & edge paths
// ============================================================================

// configurableRecentOrders lets each test control GetOrderByUserDate, which the
// shared fakeRecentOrders hardcodes to (nil, nil).
type configurableRecentOrders struct {
	order    *menu.UserOrderToday
	orderErr error
}

func (f *configurableRecentOrders) RecentOrdersByUser(_ context.Context, _ string, _, _ int) ([]menu.RecentOrderRow, error) {
	return nil, nil
}
func (f *configurableRecentOrders) GetOrderByUserDate(_ context.Context, _ string, _ time.Time, _ string) (*menu.UserOrderToday, error) {
	return f.order, f.orderErr
}
func (f *configurableRecentOrders) ItemNamesByOrderIDs(_ context.Context, _ []string, _ int) (map[string][]string, error) {
	return nil, nil
}
func (f *configurableRecentOrders) OrderAvailability(_ context.Context, _ []string, _ time.Time) (map[string]bool, error) {
	return nil, nil
}

// configurablePopularity lets tests control AllCutoffsPassed, which the shared
// fakePopularity hardcodes to (false, nil).
type configurablePopularity struct {
	allPassed    bool
	allPassedErr error
}

func (f *configurablePopularity) PlantPopularity(_ context.Context, _ string, _ time.Time) (map[string]float64, error) {
	return nil, nil
}
func (f *configurablePopularity) MetaByIDs(_ context.Context, _ []string) ([]menu.MenuItemMeta, error) {
	return nil, nil
}
func (f *configurablePopularity) AllCutoffsPassed(_ context.Context, _ string, _ time.Time, _ time.Time) (bool, error) {
	return f.allPassed, f.allPassedErr
}

func newComputeSvc(ro menu.RecentOrdersForHome, pop menu.PopularityForHome, tz *time.Location) *menu.HomeService {
	return &menu.HomeService{
		Clock:        clock.FixedClock{T: time.Date(2026, 5, 15, 9, 0, 0, 0, time.UTC)},
		ServerTZ:     tz,
		RecentOrders: ro,
		Popularity:   pop,
	}
}

func TestHomeCompute_GetOrderError(t *testing.T) {
	ro := &configurableRecentOrders{orderErr: errors.New("db boom")}
	pop := &configurablePopularity{}
	svc := newComputeSvc(ro, pop, time.UTC)
	_, err := svc.Compute(context.Background(), "u1", "P", "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "get order by user/date")
}

func TestHomeCompute_NextOrderableDayError(t *testing.T) {
	// No order today (nil), so Compute proceeds to nextOrderableDay, which
	// surfaces AllCutoffsPassed's error.
	ro := &configurableRecentOrders{}
	pop := &configurablePopularity{allPassedErr: errors.New("cutoff boom")}
	svc := newComputeSvc(ro, pop, time.UTC)
	_, err := svc.Compute(context.Background(), "u1", "P", "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "next-orderable-day")
}

func TestHomeCompute_OpenOrderToday_HasOrdered(t *testing.T) {
	// An open (non-closed) order today → returns today with the summary.
	ro := &configurableRecentOrders{order: &menu.UserOrderToday{
		OrderID:         "o1",
		VendorID:        "vA",
		Status:          "placed",
		TotalPriceMinor: 1200,
		CutoffAt:        time.Date(2026, 5, 15, 11, 0, 0, 0, time.UTC),
	}}
	pop := &configurablePopularity{}
	svc := newComputeSvc(ro, pop, time.UTC)
	state, err := svc.Compute(context.Background(), "u1", "P", "")
	require.NoError(t, err)
	assert.Equal(t, "2026-05-15", state.TargetDay)
	assert.True(t, state.HasOrdered)
	require.NotNil(t, state.OrderSummary)
	assert.Equal(t, "o1", state.OrderSummary.OrderID)
	assert.Equal(t, int64(1200), state.OrderSummary.TotalPriceMinor)
}

func TestHomeCompute_ClosedOrderToday_AdvancesStartDay(t *testing.T) {
	// A closed order today → startDay = tomorrow; nextOrderableDay finds it
	// immediately (not passed). The summary for tomorrow is nil.
	ro := &configurableRecentOrders{order: &menu.UserOrderToday{
		OrderID: "o1", VendorID: "vA", Status: "picked_up",
	}}
	pop := &configurablePopularity{allPassed: false}
	svc := newComputeSvc(ro, pop, time.UTC)
	// orderSummaryFor for the next day will call GetOrderByUserDate again and
	// return the same row; since the day differs in real DB this is fine for a
	// fake, but we just assert it advanced past today.
	state, err := svc.Compute(context.Background(), "u1", "P", "")
	require.NoError(t, err)
	assert.Equal(t, "2026-05-16", state.TargetDay)
}

func TestHomeCompute_ServerTZNilUsesLocal(t *testing.T) {
	// ServerTZ nil → serverTZ() falls back to time.Local. No order, not passed
	// → returns today (in local tz). We only assert it doesn't error and that
	// HasOrdered is false.
	ro := &configurableRecentOrders{}
	pop := &configurablePopularity{}
	svc := newComputeSvc(ro, pop, nil) // nil ServerTZ
	state, err := svc.Compute(context.Background(), "u1", "P", "")
	require.NoError(t, err)
	assert.False(t, state.HasOrdered)
	assert.NotEmpty(t, state.TargetDay)
}

func TestHomeCompute_BuildHomeStateForDay_OrderSummaryError(t *testing.T) {
	// dayOverride path → buildHomeStateForDay → orderSummaryFor → GetOrderByUserDate error.
	ro := &configurableRecentOrders{orderErr: errors.New("summary boom")}
	pop := &configurablePopularity{}
	svc := newComputeSvc(ro, pop, time.UTC)
	_, err := svc.Compute(context.Background(), "u1", "P", "2026-05-20")
	require.Error(t, err)
}

func TestHomeCompute_NextOrderableDay_ExhaustsFortnight(t *testing.T) {
	// Every day's cutoffs are passed → loop runs the full 14 iterations and
	// returns the 14th-day fallback (exercises the post-loop return).
	ro := &configurableRecentOrders{}
	pop := &configurablePopularity{allPassed: true}
	svc := newComputeSvc(ro, pop, time.UTC)
	state, err := svc.Compute(context.Background(), "u1", "P", "")
	require.NoError(t, err)
	// today=2026-05-15; loop advances 14 days from start → 2026-05-29.
	assert.Equal(t, "2026-05-29", state.TargetDay)
}

// ============================================================================
// home_service.go — RecommendChips negative-offset clamp
// ============================================================================

func TestHomeRecommendChips_NegativeOffsetClampsToZero(t *testing.T) {
	svc, _, pop, aff, _ := newFakeHomeService()
	pop.pop = map[string]float64{"i1": 5.0, "i2": 4.0}
	pop.meta = []menu.MenuItemMeta{
		{ID: "i1", VendorID: "vA"},
		{ID: "i2", VendorID: "vB"},
	}
	aff.aff = map[string]float64{}
	// offset < 0 must clamp to 0 and return from the start.
	chips, _, err := svc.RecommendChips(context.Background(), "u1", "P", time.Now(), -10, 5)
	require.NoError(t, err)
	require.Len(t, chips, 2)
	assert.Equal(t, "i1", chips[0].MenuItemID)
}
