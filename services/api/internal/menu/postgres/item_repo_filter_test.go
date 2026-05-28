package postgres_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Agentic-Build/corporate-catering-system/services/api/internal/menu"
	"github.com/Agentic-Build/corporate-catering-system/services/api/internal/menu/postgres"
)

// employeeMenuFixture seeds one approved vendor mapped to a plant with four
// active items + supply on the given day, and returns the repo, plant, day and
// the item IDs keyed by name. It is the shared fixture for the F3
// search/filter/sort repo tests.
func employeeMenuFixture(t *testing.T) (*postgres.ItemRepo, string, time.Time, map[string]string) {
	t.Helper()
	pool, cleanup := setupPostgres(t)
	t.Cleanup(cleanup)
	ctx := context.Background()

	vendorID := seedApprovedVendor(t, pool, "f3-fixture")
	plant := "F12B-3F"
	seedPlantMapping(t, pool, vendorID, plant)
	day := time.Date(2026, 5, 14, 0, 0, 0, 0, time.UTC)
	repo := postgres.NewItemRepo(pool)

	type spec struct {
		name   string
		desc   string
		price  int64
		tags   []string
		remain int
		cap    int
	}
	specs := []spec{
		{"雞腿便當", "酥脆多汁的炸雞腿", 12000, []string{"高蛋白"}, 30, 50},
		{"低卡沙拉", "新鮮蔬菜雞胸肉", 8000, []string{"低卡", "高蛋白"}, 0, 40},
		{"豬排便當", "厚切豬排配白飯", 11000, []string{"高蛋白"}, 10, 30},
		{"素食便當", "當季時蔬便當", 9000, []string{"素食", "低卡"}, 25, 25},
	}
	ids := make(map[string]string, len(specs))
	for _, s := range specs {
		it := &menu.Item{
			VendorID:    vendorID,
			Name:        s.name,
			Description: s.desc,
			PriceMinor:  s.price,
			Tags:        s.tags,
			Status:      menu.ItemStatusActive,
		}
		require.NoError(t, repo.Create(ctx, it))
		require.NoError(t, repo.SetStatus(ctx, it.ID, menu.ItemStatusActive))
		seedMealSupply(t, pool, it.ID, day, s.cap, s.remain)
		ids[s.name] = it.ID
	}
	return repo, plant, day, ids
}

// namesOf collects the item names from a result set for order-sensitive asserts.
func namesOf(rows []*menu.ActiveItemRow) []string {
	out := make([]string, 0, len(rows))
	for _, r := range rows {
		out = append(out, r.Item.Name)
	}
	return out
}

func TestItemRepo_ListActiveByPlant_NoFilterUnchanged(t *testing.T) {
	repo, plant, day, _ := employeeMenuFixture(t)

	// A filter carrying only Plant/Day must return every active+supplied item
	// in the historical default order (vendor display_name, then item name).
	// All four items share one vendor, so this reduces to name ordering.
	rows, err := repo.ListActiveByPlant(context.Background(), menu.EmployeeMenuFilter{Plant: plant, Day: day})
	require.NoError(t, err)
	assert.Equal(t, []string{"低卡沙拉", "素食便當", "豬排便當", "雞腿便當"}, namesOf(rows))
}

func TestItemRepo_ListActiveByPlant_KeywordFilter(t *testing.T) {
	repo, plant, day, _ := employeeMenuFixture(t)
	ctx := context.Background()

	// Keyword matched against name.
	rows, err := repo.ListActiveByPlant(ctx, menu.EmployeeMenuFilter{Plant: plant, Day: day, Q: "便當"})
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{"雞腿便當", "豬排便當", "素食便當"}, namesOf(rows))

	// Keyword matched against description only ("雞胸" appears in 低卡沙拉's desc).
	rows, err = repo.ListActiveByPlant(ctx, menu.EmployeeMenuFilter{Plant: plant, Day: day, Q: "雞胸"})
	require.NoError(t, err)
	assert.Equal(t, []string{"低卡沙拉"}, namesOf(rows))

	// No match.
	rows, err = repo.ListActiveByPlant(ctx, menu.EmployeeMenuFilter{Plant: plant, Day: day, Q: "不存在的關鍵字"})
	require.NoError(t, err)
	assert.Empty(t, rows)
}

func TestItemRepo_ListActiveByPlant_TagFilter(t *testing.T) {
	repo, plant, day, _ := employeeMenuFixture(t)
	ctx := context.Background()

	// Single tag.
	rows, err := repo.ListActiveByPlant(ctx, menu.EmployeeMenuFilter{Plant: plant, Day: day, Tags: []string{"素食"}})
	require.NoError(t, err)
	assert.Equal(t, []string{"素食便當"}, namesOf(rows))

	// Multiple tags: item matches if it has ANY of them (array overlap).
	rows, err = repo.ListActiveByPlant(ctx, menu.EmployeeMenuFilter{Plant: plant, Day: day, Tags: []string{"低卡", "素食"}})
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{"低卡沙拉", "素食便當"}, namesOf(rows))

	// Tag carried by no item.
	rows, err = repo.ListActiveByPlant(ctx, menu.EmployeeMenuFilter{Plant: plant, Day: day, Tags: []string{"無此標籤"}})
	require.NoError(t, err)
	assert.Empty(t, rows)
}

func TestItemRepo_ListActiveByPlant_PriceRange(t *testing.T) {
	repo, plant, day, _ := employeeMenuFixture(t)
	ctx := context.Background()
	min9000 := int64(9000)
	max11000 := int64(11000)

	// price_min only — bound is inclusive (素食便當 is exactly 9000).
	rows, err := repo.ListActiveByPlant(ctx, menu.EmployeeMenuFilter{Plant: plant, Day: day, PriceMin: &min9000})
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{"雞腿便當", "豬排便當", "素食便當"}, namesOf(rows))

	// price_max only — bound is inclusive (豬排便當 is exactly 11000).
	rows, err = repo.ListActiveByPlant(ctx, menu.EmployeeMenuFilter{Plant: plant, Day: day, PriceMax: &max11000})
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{"低卡沙拉", "豬排便當", "素食便當"}, namesOf(rows))

	// Inclusive range [9000, 11000].
	rows, err = repo.ListActiveByPlant(ctx, menu.EmployeeMenuFilter{Plant: plant, Day: day, PriceMin: &min9000, PriceMax: &max11000})
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{"豬排便當", "素食便當"}, namesOf(rows))
}

func TestItemRepo_ListActiveByPlant_InStock(t *testing.T) {
	repo, plant, day, _ := employeeMenuFixture(t)
	ctx := context.Background()
	inStock := true
	notInStock := false

	// in_stock=true excludes the sold-out 低卡沙拉 (remain=0).
	rows, err := repo.ListActiveByPlant(ctx, menu.EmployeeMenuFilter{Plant: plant, Day: day, InStock: &inStock})
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{"雞腿便當", "豬排便當", "素食便當"}, namesOf(rows))

	// in_stock=false is a no-op: the sold-out item stays in the result.
	rows, err = repo.ListActiveByPlant(ctx, menu.EmployeeMenuFilter{Plant: plant, Day: day, InStock: &notInStock})
	require.NoError(t, err)
	assert.Len(t, rows, 4)
}

func TestItemRepo_ListActiveByPlant_Sort(t *testing.T) {
	repo, plant, day, _ := employeeMenuFixture(t)
	ctx := context.Background()

	rows, err := repo.ListActiveByPlant(ctx, menu.EmployeeMenuFilter{Plant: plant, Day: day, Sort: menu.EmployeeMenuSortName})
	require.NoError(t, err)
	assert.Equal(t, []string{"低卡沙拉", "素食便當", "豬排便當", "雞腿便當"}, namesOf(rows))

	rows, err = repo.ListActiveByPlant(ctx, menu.EmployeeMenuFilter{Plant: plant, Day: day, Sort: menu.EmployeeMenuSortPriceAsc})
	require.NoError(t, err)
	assert.Equal(t, []string{"低卡沙拉", "素食便當", "豬排便當", "雞腿便當"}, namesOf(rows))

	rows, err = repo.ListActiveByPlant(ctx, menu.EmployeeMenuFilter{Plant: plant, Day: day, Sort: menu.EmployeeMenuSortPriceDesc})
	require.NoError(t, err)
	assert.Equal(t, []string{"雞腿便當", "豬排便當", "素食便當", "低卡沙拉"}, namesOf(rows))

	// remain: 雞腿=30, 素食=25, 豬排=10, 低卡=0 → DESC.
	rows, err = repo.ListActiveByPlant(ctx, menu.EmployeeMenuFilter{Plant: plant, Day: day, Sort: menu.EmployeeMenuSortRemain})
	require.NoError(t, err)
	assert.Equal(t, []string{"雞腿便當", "素食便當", "豬排便當", "低卡沙拉"}, namesOf(rows))
}

func TestItemRepo_ListActiveByPlant_CombinedFilters(t *testing.T) {
	repo, plant, day, _ := employeeMenuFixture(t)
	ctx := context.Background()
	max11000 := int64(11000)
	inStock := true

	// "便當" keyword + 高蛋白 tag + price <= 11000 + in stock, sorted by price asc.
	// 雞腿便當 (12000) excluded by price; 低卡沙拉 excluded by keyword;
	// 素食便當 excluded by tag → only 豬排便當 remains.
	rows, err := repo.ListActiveByPlant(ctx, menu.EmployeeMenuFilter{
		Plant:    plant,
		Day:      day,
		Q:        "便當",
		Tags:     []string{"高蛋白"},
		PriceMax: &max11000,
		InStock:  &inStock,
		Sort:     menu.EmployeeMenuSortPriceAsc,
	})
	require.NoError(t, err)
	assert.Equal(t, []string{"豬排便當"}, namesOf(rows))
}
