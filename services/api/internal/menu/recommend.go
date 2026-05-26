package menu

import "sort"

// MenuItemMeta is the minimal candidate-item projection used by the recommender.
// It is intentionally narrower than the full menu.Item to keep score() pure and
// free of unrelated fields (tags, archival, etc.).
type MenuItemMeta struct {
	ID        string
	Name      string
	UnitPrice int64
	VendorID  string
}

// RecommendInputs bundles everything Score() needs. The caller (HomeService) is
// responsible for normalising VendorAffinity to a 0..1 distribution; an empty
// map represents the cold-start path (no user history).
type RecommendInputs struct {
	Popularity     map[string]float64  // menu_item_id → base count (plant popularity on target day)
	VendorAffinity map[string]float64  // vendor_id → normalised weight (sums to 1) or empty
	Items          []MenuItemMeta      // candidate items (typically: all items with popularity > 0)
	FavoriteIDs    map[string]struct{} // items to exclude (already in favorites chips)
	Alpha          float64             // affinity weight; 0 disables affinity entirely
	Limit          int                 // max chips to return
}

// RecommendChip is one ranked recommendation with its reason for the UI.
type RecommendChip struct {
	MenuItemID string
	Name       string
	UnitPrice  int64
	VendorID   string
	Score      float64
	Reason     string // "因為你常點此家" if vendor in user history, else "同事熱門"
}

// Score ranks candidate items by popularity × (1 + α·affinity) and returns the
// top Limit chips. Items not present in Popularity are dropped — only items
// with a real signal are recommended.
//
// Tie-break: by MenuItemID ascending, for deterministic output.
//
// Pure function: no DB, no clock, no side effects.
func Score(in RecommendInputs) []RecommendChip {
	out := make([]RecommendChip, 0, len(in.Items))
	for _, it := range in.Items {
		if _, ok := in.FavoriteIDs[it.ID]; ok {
			continue
		}
		pop, ok := in.Popularity[it.ID]
		if !ok {
			continue
		}
		aff := in.VendorAffinity[it.VendorID]
		s := pop * (1.0 + in.Alpha*aff)
		reason := "同事熱門"
		if aff > 0 {
			reason = "因為你常點此家"
		}
		out = append(out, RecommendChip{
			MenuItemID: it.ID,
			Name:       it.Name,
			UnitPrice:  it.UnitPrice,
			VendorID:   it.VendorID,
			Score:      s,
			Reason:     reason,
		})
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Score != out[j].Score {
			return out[i].Score > out[j].Score
		}
		return out[i].MenuItemID < out[j].MenuItemID
	})
	if in.Limit > 0 && len(out) > in.Limit {
		out = out[:in.Limit]
	}
	return out
}
