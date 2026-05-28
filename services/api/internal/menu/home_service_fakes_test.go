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

// === Fakes for the four HomeService ports (no DB needed) ===

type fakeRecentOrders struct {
	recent       []menu.RecentOrderRow
	recentErr    error
	previews     map[string][]string
	previewsErr  error
	availability map[string]bool
	availErr     error
}

func (f *fakeRecentOrders) RecentOrdersByUser(_ context.Context, _ string, _, _ int) ([]menu.RecentOrderRow, error) {
	return f.recent, f.recentErr
}
func (f *fakeRecentOrders) GetOrderByUserDate(_ context.Context, _ string, _ time.Time, _ string) (*menu.UserOrderToday, error) {
	return nil, nil
}
func (f *fakeRecentOrders) ItemNamesByOrderIDs(_ context.Context, _ []string, _ int) (map[string][]string, error) {
	return f.previews, f.previewsErr
}
func (f *fakeRecentOrders) OrderAvailability(_ context.Context, _ []string, _ time.Time) (map[string]bool, error) {
	return f.availability, f.availErr
}

type fakePopularity struct {
	pop     map[string]float64
	popErr  error
	meta    []menu.MenuItemMeta
	metaErr error
}

func (f *fakePopularity) PlantPopularity(_ context.Context, _ string, _ time.Time) (map[string]float64, error) {
	return f.pop, f.popErr
}
func (f *fakePopularity) MetaByIDs(_ context.Context, _ []string) ([]menu.MenuItemMeta, error) {
	return f.meta, f.metaErr
}
func (f *fakePopularity) AllCutoffsPassed(_ context.Context, _ string, _ time.Time, _ time.Time) (bool, error) {
	return false, nil
}

type fakeAffinity struct {
	aff    map[string]float64
	affErr error
}

func (f *fakeAffinity) UserVendorAffinity(_ context.Context, _ string) (map[string]float64, error) {
	return f.aff, f.affErr
}

type fakeFavForHome struct {
	chips    []menu.FavoriteChip
	cursor   *time.Time
	err      error
	lastArgs favListArgs
}

func (f *fakeFavForHome) ListByUser(_ context.Context, userID, targetDay, plant string, limit int, cursor *time.Time) ([]menu.FavoriteChip, *time.Time, error) {
	f.lastArgs = favListArgs{userID, targetDay, plant, limit, cursor}
	return f.chips, f.cursor, f.err
}

func newFakeHomeService() (*menu.HomeService, *fakeRecentOrders, *fakePopularity, *fakeAffinity, *fakeFavForHome) {
	ro := &fakeRecentOrders{}
	pop := &fakePopularity{}
	aff := &fakeAffinity{}
	fav := &fakeFavForHome{}
	svc := &menu.HomeService{
		Clock:         clock.FixedClock{T: time.Date(2026, 5, 15, 9, 0, 0, 0, time.UTC)},
		ServerTZ:      time.UTC,
		RecentOrders:  ro,
		Popularity:    pop,
		Affinity:      aff,
		FavoritesRepo: fav,
		Alpha:         1.0,
	}
	return svc, ro, pop, aff, fav
}

// === FavoriteChipsList ===

func TestHomeFavoriteChipsList_DefaultLimitAndDelegates(t *testing.T) {
	svc, _, _, _, fav := newFakeHomeService()
	svc.FavoriteLimit = 7
	fav.chips = []menu.FavoriteChip{{MenuItemID: "i1", UnitPrice: 110}}
	chips, _, err := svc.FavoriteChipsList(context.Background(), "u1", "2026-05-15", "F12B-3F", 0, nil)
	require.NoError(t, err)
	require.Len(t, chips, 1)
	assert.Equal(t, int64(110), chips[0].UnitPrice)
	assert.Equal(t, 7, fav.lastArgs.limit, "limit<=0 falls back to FavoriteLimit")
}

func TestHomeFavoriteChipsList_ZeroLimitNoConfigUsesFive(t *testing.T) {
	svc, _, _, _, fav := newFakeHomeService()
	_, _, err := svc.FavoriteChipsList(context.Background(), "u1", "2026-05-15", "F12B-3F", 0, nil)
	require.NoError(t, err)
	assert.Equal(t, 5, fav.lastArgs.limit)
}

func TestHomeFavoriteChipsList_ExplicitLimitPassthrough(t *testing.T) {
	svc, _, _, _, fav := newFakeHomeService()
	_, _, err := svc.FavoriteChipsList(context.Background(), "u1", "2026-05-15", "F12B-3F", 3, nil)
	require.NoError(t, err)
	assert.Equal(t, 3, fav.lastArgs.limit)
}

// === ReorderChips ===

func TestHomeReorderChips_EmptyReturnsNoMore(t *testing.T) {
	svc, _, _, _, _ := newFakeHomeService()
	chips, next, err := svc.ReorderChips(context.Background(), "u1", time.Now(), 0, 5)
	require.NoError(t, err)
	assert.Nil(t, chips)
	assert.Equal(t, -1, next)
}

func TestHomeReorderChips_DefaultLimitFromConfig(t *testing.T) {
	svc, ro, _, _, _ := newFakeHomeService()
	svc.ReorderLimit = 2
	ro.recent = []menu.RecentOrderRow{
		{OrderID: "o1", VendorID: "vA", TotalPriceMinor: 1200, Freq: 3},
		{OrderID: "o2", VendorID: "vB", TotalPriceMinor: 900, Freq: 1},
	}
	ro.previews = map[string][]string{"o1": {"雞腿便當"}, "o2": {"排骨飯"}}
	ro.availability = map[string]bool{"o1": true}
	chips, next, err := svc.ReorderChips(context.Background(), "u1", time.Now(), 0, 0)
	require.NoError(t, err)
	require.Len(t, chips, 2)
	assert.Equal(t, "o1", chips[0].SourceOrderID)
	assert.Equal(t, int64(1200), chips[0].TotalPriceMinor)
	assert.True(t, chips[0].AvailableToday)
	assert.Equal(t, []string{"雞腿便當"}, chips[0].ItemsPreview)
	// len(rows)==limit(2) → next page offset.
	assert.Equal(t, 2, next)
}

func TestHomeReorderChips_FewerThanLimitNoMore(t *testing.T) {
	svc, ro, _, _, _ := newFakeHomeService()
	ro.recent = []menu.RecentOrderRow{{OrderID: "o1", VendorID: "vA"}}
	ro.previews = map[string][]string{}
	ro.availability = map[string]bool{}
	_, next, err := svc.ReorderChips(context.Background(), "u1", time.Now(), 0, 5)
	require.NoError(t, err)
	assert.Equal(t, -1, next)
}

func TestHomeReorderChips_UsesVendorNames(t *testing.T) {
	svc, ro, _, _, _ := newFakeHomeService()
	ro.recent = []menu.RecentOrderRow{{OrderID: "o1", VendorID: "vA"}}
	ro.previews = map[string][]string{}
	ro.availability = map[string]bool{}
	svc.VendorNames = func(_ context.Context, ids []string) (map[string]string, error) {
		return map[string]string{"vA": "便當王"}, nil
	}
	chips, _, err := svc.ReorderChips(context.Background(), "u1", time.Now(), 0, 5)
	require.NoError(t, err)
	require.Len(t, chips, 1)
	assert.Equal(t, "便當王", chips[0].VendorName)
}

func TestHomeReorderChips_RecentOrdersError(t *testing.T) {
	svc, ro, _, _, _ := newFakeHomeService()
	ro.recentErr = errors.New("boom")
	_, next, err := svc.ReorderChips(context.Background(), "u1", time.Now(), 0, 5)
	assert.Error(t, err)
	assert.Equal(t, -1, next)
}

func TestHomeReorderChips_PreviewsError(t *testing.T) {
	svc, ro, _, _, _ := newFakeHomeService()
	ro.recent = []menu.RecentOrderRow{{OrderID: "o1", VendorID: "vA"}}
	ro.previewsErr = errors.New("boom")
	_, _, err := svc.ReorderChips(context.Background(), "u1", time.Now(), 0, 5)
	assert.Error(t, err)
}

func TestHomeReorderChips_AvailabilityError(t *testing.T) {
	svc, ro, _, _, _ := newFakeHomeService()
	ro.recent = []menu.RecentOrderRow{{OrderID: "o1", VendorID: "vA"}}
	ro.previews = map[string][]string{}
	ro.availErr = errors.New("boom")
	_, _, err := svc.ReorderChips(context.Background(), "u1", time.Now(), 0, 5)
	assert.Error(t, err)
}

func TestHomeReorderChips_VendorNamesError(t *testing.T) {
	svc, ro, _, _, _ := newFakeHomeService()
	ro.recent = []menu.RecentOrderRow{{OrderID: "o1", VendorID: "vA"}}
	ro.previews = map[string][]string{}
	ro.availability = map[string]bool{}
	svc.VendorNames = func(_ context.Context, _ []string) (map[string]string, error) {
		return nil, errors.New("boom")
	}
	_, _, err := svc.ReorderChips(context.Background(), "u1", time.Now(), 0, 5)
	assert.Error(t, err)
}

// === RecommendChips ===

func TestHomeRecommendChips_EmptyPopularityNoMore(t *testing.T) {
	svc, _, pop, _, _ := newFakeHomeService()
	pop.pop = map[string]float64{}
	chips, next, err := svc.RecommendChips(context.Background(), "u1", "F12B-3F", time.Now(), 0, 5)
	require.NoError(t, err)
	assert.Nil(t, chips)
	assert.Equal(t, -1, next)
}

func TestHomeRecommendChips_DefaultLimitAndPagination(t *testing.T) {
	svc, _, pop, aff, _ := newFakeHomeService()
	svc.RecommendLimit = 2
	pop.pop = map[string]float64{"i1": 5.0, "i2": 4.0, "i3": 3.0}
	pop.meta = []menu.MenuItemMeta{
		{ID: "i1", Name: "A", UnitPrice: 110, VendorID: "vA"},
		{ID: "i2", Name: "B", UnitPrice: 120, VendorID: "vB"},
		{ID: "i3", Name: "C", UnitPrice: 130, VendorID: "vC"},
	}
	aff.aff = map[string]float64{} // cold start

	// First page (limit 0 → default 2).
	page1, next1, err := svc.RecommendChips(context.Background(), "u1", "F12B-3F", time.Now(), 0, 0)
	require.NoError(t, err)
	require.Len(t, page1, 2)
	assert.Equal(t, "i1", page1[0].MenuItemID)
	assert.Equal(t, int64(110), page1[0].UnitPrice)
	assert.Equal(t, 2, next1)

	// Second page → remaining item, then no more.
	page2, next2, err := svc.RecommendChips(context.Background(), "u1", "F12B-3F", time.Now(), 2, 2)
	require.NoError(t, err)
	require.Len(t, page2, 1)
	assert.Equal(t, "i3", page2[0].MenuItemID)
	assert.Equal(t, -1, next2)
}

func TestHomeRecommendChips_OffsetBeyondEndNoMore(t *testing.T) {
	svc, _, pop, aff, _ := newFakeHomeService()
	pop.pop = map[string]float64{"i1": 5.0}
	pop.meta = []menu.MenuItemMeta{{ID: "i1", VendorID: "vA"}}
	aff.aff = map[string]float64{}
	chips, next, err := svc.RecommendChips(context.Background(), "u1", "F12B-3F", time.Now(), 99, 5)
	require.NoError(t, err)
	assert.Nil(t, chips)
	assert.Equal(t, -1, next)
}

func TestHomeRecommendChips_ZeroSumAffinityColdStart(t *testing.T) {
	svc, _, pop, aff, _ := newFakeHomeService()
	pop.pop = map[string]float64{"i1": 5.0}
	pop.meta = []menu.MenuItemMeta{{ID: "i1", VendorID: "vA"}}
	aff.aff = map[string]float64{"vA": 0, "vB": 0} // non-empty but zero-sum
	chips, _, err := svc.RecommendChips(context.Background(), "u1", "F12B-3F", time.Now(), 0, 5)
	require.NoError(t, err)
	require.Len(t, chips, 1)
	assert.Equal(t, "同事熱門", chips[0].Reason, "zero-sum affinity is cold start")
}

func TestHomeRecommendChips_ExcludesFavorites(t *testing.T) {
	svc, _, pop, aff, fav := newFakeHomeService()
	pop.pop = map[string]float64{"i1": 5.0, "i2": 4.0}
	pop.meta = []menu.MenuItemMeta{{ID: "i1", VendorID: "vA"}, {ID: "i2", VendorID: "vB"}}
	aff.aff = map[string]float64{}
	fav.chips = []menu.FavoriteChip{{MenuItemID: "i1"}}
	chips, _, err := svc.RecommendChips(context.Background(), "u1", "F12B-3F", time.Now(), 0, 5)
	require.NoError(t, err)
	for _, c := range chips {
		assert.NotEqual(t, "i1", c.MenuItemID)
	}
}

func TestHomeRecommendChips_PopularityError(t *testing.T) {
	svc, _, pop, _, _ := newFakeHomeService()
	pop.popErr = errors.New("boom")
	_, next, err := svc.RecommendChips(context.Background(), "u1", "F12B-3F", time.Now(), 0, 5)
	assert.Error(t, err)
	assert.Equal(t, -1, next)
}

func TestHomeRecommendChips_AffinityError(t *testing.T) {
	svc, _, pop, aff, _ := newFakeHomeService()
	pop.pop = map[string]float64{"i1": 5.0}
	aff.affErr = errors.New("boom")
	_, _, err := svc.RecommendChips(context.Background(), "u1", "F12B-3F", time.Now(), 0, 5)
	assert.Error(t, err)
}

func TestHomeRecommendChips_MetaError(t *testing.T) {
	svc, _, pop, aff, _ := newFakeHomeService()
	pop.pop = map[string]float64{"i1": 5.0}
	aff.aff = map[string]float64{}
	pop.metaErr = errors.New("boom")
	_, _, err := svc.RecommendChips(context.Background(), "u1", "F12B-3F", time.Now(), 0, 5)
	assert.Error(t, err)
}

func TestHomeRecommendChips_FavoriteIDSetError(t *testing.T) {
	svc, _, pop, aff, fav := newFakeHomeService()
	pop.pop = map[string]float64{"i1": 5.0}
	pop.meta = []menu.MenuItemMeta{{ID: "i1", VendorID: "vA"}}
	aff.aff = map[string]float64{}
	fav.err = errors.New("boom")
	_, _, err := svc.RecommendChips(context.Background(), "u1", "F12B-3F", time.Now(), 0, 5)
	assert.Error(t, err)
}
