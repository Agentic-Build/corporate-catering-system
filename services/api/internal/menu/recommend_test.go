package menu_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/takalawang/corporate-catering-system/services/api/internal/menu"
)

func TestScore_ColdStartUsesPopularityOnly(t *testing.T) {
	// No vendor affinity → recommendations are pure popularity, all reasons = "同事熱門".
	items := []menu.MenuItemMeta{
		{ID: "i1", Name: "A", UnitPrice: 100, VendorID: "vA"},
		{ID: "i2", Name: "B", UnitPrice: 200, VendorID: "vB"},
		{ID: "i3", Name: "C", UnitPrice: 300, VendorID: "vC"},
	}
	out := menu.Score(menu.RecommendInputs{
		Popularity:     map[string]float64{"i1": 3.0, "i2": 5.0, "i3": 1.0},
		VendorAffinity: map[string]float64{}, // cold start
		Items:          items,
		FavoriteIDs:    map[string]struct{}{},
		Alpha:          1.0,
		Limit:          5,
	})
	require.Len(t, out, 3)
	// Order by popularity desc: i2(5), i1(3), i3(1)
	assert.Equal(t, "i2", out[0].MenuItemID)
	assert.Equal(t, "i1", out[1].MenuItemID)
	assert.Equal(t, "i3", out[2].MenuItemID)
	for _, c := range out {
		assert.Equal(t, "同事熱門", c.Reason, "cold-start should always use 同事熱門")
	}
	// Score equals popularity when affinity is 0.
	assert.InDelta(t, 5.0, out[0].Score, 1e-9)
	assert.InDelta(t, 3.0, out[1].Score, 1e-9)
	assert.InDelta(t, 1.0, out[2].Score, 1e-9)
}

func TestScore_VendorAffinityBoostsAndChangesReason(t *testing.T) {
	// Items: i1 vendor vA (user has affinity), i2 vendor vB (user has no affinity).
	items := []menu.MenuItemMeta{
		{ID: "i1", Name: "A", UnitPrice: 100, VendorID: "vA"},
		{ID: "i2", Name: "B", UnitPrice: 200, VendorID: "vB"},
	}
	out := menu.Score(menu.RecommendInputs{
		Popularity: map[string]float64{"i1": 2.0, "i2": 5.0},
		// Normalized affinity (sums to 1) — caller normalises before passing in.
		VendorAffinity: map[string]float64{"vA": 1.0},
		Items:          items,
		FavoriteIDs:    map[string]struct{}{},
		Alpha:          2.0, // large boost so i1 beats i2
		Limit:          5,
	})
	require.Len(t, out, 2)
	// i1: 2.0 * (1 + 2.0*1.0) = 6.0
	// i2: 5.0 * (1 + 2.0*0)   = 5.0
	assert.Equal(t, "i1", out[0].MenuItemID)
	assert.Equal(t, "因為你常點此家", out[0].Reason)
	assert.InDelta(t, 6.0, out[0].Score, 1e-9)
	assert.Equal(t, "i2", out[1].MenuItemID)
	assert.Equal(t, "同事熱門", out[1].Reason)
	assert.InDelta(t, 5.0, out[1].Score, 1e-9)
}

func TestScore_AlphaZeroFallsBackToPopularity(t *testing.T) {
	// Alpha=0 → vendor affinity has no effect, even when set.
	items := []menu.MenuItemMeta{
		{ID: "i1", Name: "A", UnitPrice: 100, VendorID: "vA"},
		{ID: "i2", Name: "B", UnitPrice: 200, VendorID: "vB"},
	}
	out := menu.Score(menu.RecommendInputs{
		Popularity:     map[string]float64{"i1": 2.0, "i2": 5.0},
		VendorAffinity: map[string]float64{"vA": 1.0},
		Items:          items,
		FavoriteIDs:    map[string]struct{}{},
		Alpha:          0,
		Limit:          5,
	})
	require.Len(t, out, 2)
	// Order is by raw popularity.
	assert.Equal(t, "i2", out[0].MenuItemID)
	assert.Equal(t, "i1", out[1].MenuItemID)
	// Reason still mirrors affinity presence (i1 vendor has user history).
	assert.Equal(t, "同事熱門", out[0].Reason)
	assert.Equal(t, "因為你常點此家", out[1].Reason)
}

func TestScore_FavoritesAreExcluded(t *testing.T) {
	items := []menu.MenuItemMeta{
		{ID: "i1", Name: "A", UnitPrice: 100, VendorID: "vA"},
		{ID: "i2", Name: "B", UnitPrice: 200, VendorID: "vB"},
		{ID: "i3", Name: "C", UnitPrice: 300, VendorID: "vC"},
	}
	out := menu.Score(menu.RecommendInputs{
		Popularity:     map[string]float64{"i1": 3.0, "i2": 5.0, "i3": 1.0},
		VendorAffinity: map[string]float64{},
		Items:          items,
		FavoriteIDs:    map[string]struct{}{"i2": {}},
		Alpha:          1.0,
		Limit:          5,
	})
	require.Len(t, out, 2)
	for _, c := range out {
		assert.NotEqual(t, "i2", c.MenuItemID, "favorite must be excluded")
	}
}

func TestScore_LimitTruncates(t *testing.T) {
	items := []menu.MenuItemMeta{
		{ID: "i1", Name: "A", UnitPrice: 100, VendorID: "vA"},
		{ID: "i2", Name: "B", UnitPrice: 200, VendorID: "vB"},
		{ID: "i3", Name: "C", UnitPrice: 300, VendorID: "vC"},
		{ID: "i4", Name: "D", UnitPrice: 400, VendorID: "vD"},
	}
	out := menu.Score(menu.RecommendInputs{
		Popularity:     map[string]float64{"i1": 4.0, "i2": 3.0, "i3": 2.0, "i4": 1.0},
		VendorAffinity: map[string]float64{},
		Items:          items,
		FavoriteIDs:    map[string]struct{}{},
		Alpha:          1.0,
		Limit:          2,
	})
	require.Len(t, out, 2)
	assert.Equal(t, "i1", out[0].MenuItemID)
	assert.Equal(t, "i2", out[1].MenuItemID)
}

func TestScore_TieBreakByMenuItemIDDeterministic(t *testing.T) {
	// Two items with identical scores → sort breaks tie by MenuItemID asc.
	items := []menu.MenuItemMeta{
		{ID: "ib", Name: "B", UnitPrice: 100, VendorID: "v1"},
		{ID: "ia", Name: "A", UnitPrice: 100, VendorID: "v1"},
		{ID: "ic", Name: "C", UnitPrice: 100, VendorID: "v1"},
	}
	out := menu.Score(menu.RecommendInputs{
		Popularity:     map[string]float64{"ia": 1.0, "ib": 1.0, "ic": 1.0},
		VendorAffinity: map[string]float64{},
		Items:          items,
		FavoriteIDs:    map[string]struct{}{},
		Alpha:          1.0,
		Limit:          5,
	})
	require.Len(t, out, 3)
	// ia, ib, ic
	assert.Equal(t, "ia", out[0].MenuItemID)
	assert.Equal(t, "ib", out[1].MenuItemID)
	assert.Equal(t, "ic", out[2].MenuItemID)
}

func TestScore_ZeroPopularityItemsAreOmitted(t *testing.T) {
	// Items without a popularity entry should not appear (they have no signal).
	// This keeps the candidate set scoped to the plant-popularity result.
	items := []menu.MenuItemMeta{
		{ID: "i1", Name: "A", UnitPrice: 100, VendorID: "vA"},
		{ID: "i2", Name: "B", UnitPrice: 200, VendorID: "vB"},
	}
	out := menu.Score(menu.RecommendInputs{
		Popularity:     map[string]float64{"i1": 3.0}, // i2 missing
		VendorAffinity: map[string]float64{},
		Items:          items,
		FavoriteIDs:    map[string]struct{}{},
		Alpha:          1.0,
		Limit:          5,
	})
	require.Len(t, out, 1)
	assert.Equal(t, "i1", out[0].MenuItemID)
}

func TestScore_EmptyInputsReturnsEmpty(t *testing.T) {
	out := menu.Score(menu.RecommendInputs{
		Popularity:     map[string]float64{},
		VendorAffinity: map[string]float64{},
		Items:          nil,
		FavoriteIDs:    map[string]struct{}{},
		Alpha:          1.0,
		Limit:          5,
	})
	assert.Empty(t, out)
}
