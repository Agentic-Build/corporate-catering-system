package menu

import (
	"context"
	"fmt"
	"time"

	"github.com/takalawang/corporate-catering-system/services/api/internal/platform/clock"
)

// ---------- Cross-package projection types ----------
// These are defined in the menu package (rather than order/postgres) to avoid
// the import cycle:
//   menu → order/postgres → order → menu
// The concrete order/postgres repo returns these types directly, so there is
// no marshalling cost.

// RecentOrderRow is the projection returned by RecentOrdersByUser. One row
// per (user, vendor) — the user's most-recent order with that vendor in the
// last 30 days, plus the per-vendor frequency.
type RecentOrderRow struct {
	OrderID         string
	VendorID        string
	SupplyDate      time.Time
	TotalPriceMinor int64
	Freq            int
}

// UserOrderToday is the projection returned by GetOrderByUserDate. Status is
// a raw string to keep menu free of an order-package import.
type UserOrderToday struct {
	OrderID         string
	VendorID        string
	Status          string
	TotalPriceMinor int64
	CutoffAt        time.Time
	PickedUpAt      *time.Time
}

// HomeService computes the employee landing page: target_day derivation, the
// order summary (if any), and the three chip carousels (reorder / favorite /
// recommend). All cross-cutting state — Clock, server timezone, recommender
// α — is injected so the orchestration is easy to test.
//
// Timezone assumption: ServerTZ is the timezone used to derive `today` from
// the wall clock. For a single-region deployment whose server is collocated
// with the employees' lunch window (the P9 target deployment), ServerTZ ==
// time.Local. Multi-region deployments are out of scope for P9.
type HomeService struct {
	Clock    clock.Clock
	ServerTZ *time.Location

	RecentOrders  RecentOrdersForHome
	Popularity    PopularityForHome
	Affinity      AffinityForHome
	FavoritesRepo FavoritesForHome

	// VendorNames is an optional closure that batch-resolves vendor display
	// names. The controller wires this with the existing plant/vendor repo.
	// Unset is fine — chips render with empty VendorName.
	VendorNames func(ctx context.Context, vendorIDs []string) (map[string]string, error)

	Alpha          float64 // recommender weight; controller injects from env (default 1.0).
	ReorderLimit   int     // default 5
	FavoriteLimit  int     // default 5
	RecommendLimit int     // default 5
}

// ---------- Repository ports (local interfaces — keep wiring decoupled) ----------

// RecentOrdersForHome captures the slice of order/postgres.RecentOrdersRepo
// the home service consumes.
type RecentOrdersForHome interface {
	RecentOrdersByUser(ctx context.Context, userID string, limit, offset int) ([]RecentOrderRow, error)
	GetOrderByUserDate(ctx context.Context, userID string, day time.Time, plant string) (*UserOrderToday, error)
	ItemNamesByOrderIDs(ctx context.Context, orderIDs []string, cap int) (map[string][]string, error)
	OrderAvailability(ctx context.Context, orderIDs []string, day time.Time) (map[string]bool, error)
}

// PopularityForHome captures the slice of menu/postgres.PopularityRepo
// the home service consumes.
type PopularityForHome interface {
	PlantPopularity(ctx context.Context, plant string, day time.Time) (map[string]float64, error)
	MetaByIDs(ctx context.Context, ids []string) ([]MenuItemMeta, error)
	AllCutoffsPassed(ctx context.Context, plant string, day time.Time, now time.Time) (bool, error)
}

// AffinityForHome captures the slice of menu/postgres.AffinityRepo we use.
type AffinityForHome interface {
	UserVendorAffinity(ctx context.Context, userID string) (map[string]float64, error)
}

// FavoritesForHome captures the read slice of menu/postgres.FavoriteRepo
// we use. Returns []menu.FavoriteChip — defined in this package by Task 2.
type FavoritesForHome interface {
	ListByUser(ctx context.Context, userID, targetDay, plant string, limit int, cursor *time.Time) ([]FavoriteChip, *time.Time, error)
}

// ---------- Output types ----------

// HomeState is the result of target-day derivation: which day the page
// should show, whether the user already ordered for that day, and a tiny
// summary when they did.
type HomeState struct {
	TargetDay    string
	HasOrdered   bool
	OrderSummary *OrderSummary
}

// OrderSummary is a tiny denormalised view of a single order, used to render
// the "已下單" banner so the client doesn't fetch the full order DTO.
type OrderSummary struct {
	OrderID         string
	VendorID        string
	Status          string
	TotalPriceMinor int64
	CutoffAt        time.Time
}

// ReorderChip is the rendered form of a reorder chip (一鍵再點).
type ReorderChip struct {
	SourceOrderID   string
	VendorID        string
	VendorName      string
	ItemsPreview    []string // up to 2 item names
	TotalPriceMinor int64
	Freq            int
	AvailableToday  bool // can the items be served on the target day?
}

// ---------- Public methods ----------

const (
	dateLayout = "2006-01-02"
)

// Compute derives the home page's target_day + order summary. State machine:
//
//  1. If dayOverride is non-empty, parse it as YYYY-MM-DD and use as-is.
//     Still surface the day's order summary if one exists.
//  2. Otherwise, today = clock.Now() in ServerTZ.
//  3. If the user has an order today and it is picked_up/no_show, today is
//     "done" for this user → advance to the next orderable day (the first
//     day from tomorrow whose supplies aren't all past cutoff).
//  4. Otherwise, target the next orderable day starting from today: stay on
//     today unless every meal_supply row for the day has cutoff_at ≤ now, in
//     which case skip forward to the first day that is still orderable.
func (s *HomeService) Compute(ctx context.Context, userID, plant, dayOverride string) (HomeState, error) {
	tz := s.ServerTZ
	if tz == nil {
		tz = time.Local
	}
	now := s.Clock.Now().In(tz)

	if dayOverride != "" {
		d, err := time.Parse(dateLayout, dayOverride)
		if err != nil {
			return HomeState{}, fmt.Errorf("day must be YYYY-MM-DD: %w", err)
		}
		day := time.Date(d.Year(), d.Month(), d.Day(), 0, 0, 0, 0, time.UTC)
		summary, err := s.orderSummaryFor(ctx, userID, plant, day)
		if err != nil {
			return HomeState{}, err
		}
		return HomeState{
			TargetDay:    day.Format(dateLayout),
			HasOrdered:   summary != nil,
			OrderSummary: summary,
		}, nil
	}

	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	tomorrow := today.AddDate(0, 0, 1)

	row, err := s.RecentOrders.GetOrderByUserDate(ctx, userID, today, plant)
	if err != nil {
		return HomeState{}, fmt.Errorf("get order by user/date: %w", err)
	}
	if row != nil {
		if row.Status == "picked_up" || row.Status == "no_show" {
			next, err := s.nextOrderableDay(ctx, plant, tomorrow)
			if err != nil {
				return HomeState{}, fmt.Errorf("next-orderable-day: %w", err)
			}
			summary, err := s.orderSummaryFor(ctx, userID, plant, next)
			if err != nil {
				return HomeState{}, err
			}
			return HomeState{
				TargetDay:    next.Format(dateLayout),
				HasOrdered:   summary != nil,
				OrderSummary: summary,
			}, nil
		}
		return HomeState{
			TargetDay:    today.Format(dateLayout),
			HasOrdered:   true,
			OrderSummary: orderSummaryFromRow(row),
		}, nil
	}

	next, err := s.nextOrderableDay(ctx, plant, today)
	if err != nil {
		return HomeState{}, fmt.Errorf("next-orderable-day: %w", err)
	}
	summary, err := s.orderSummaryFor(ctx, userID, plant, next)
	if err != nil {
		return HomeState{}, err
	}
	return HomeState{
		TargetDay:    next.Format(dateLayout),
		HasOrdered:   summary != nil,
		OrderSummary: summary,
	}, nil
}

func (s *HomeService) nextOrderableDay(ctx context.Context, plant string, start time.Time) (time.Time, error) {
	day := start
	for i := 0; i < 14; i++ {
		passed, err := s.Popularity.AllCutoffsPassed(ctx, plant, day, s.Clock.Now())
		if err != nil {
			return day, err
		}
		if !passed {
			return day, nil
		}
		day = day.AddDate(0, 0, 1)
	}
	return day, nil
}

func (s *HomeService) orderSummaryFor(ctx context.Context, userID, plant string, day time.Time) (*OrderSummary, error) {
	row, err := s.RecentOrders.GetOrderByUserDate(ctx, userID, day, plant)
	if err != nil || row == nil {
		return nil, err
	}
	return orderSummaryFromRow(row), nil
}

func orderSummaryFromRow(row *UserOrderToday) *OrderSummary {
	return &OrderSummary{
		OrderID:         row.OrderID,
		VendorID:        row.VendorID,
		Status:          row.Status,
		TotalPriceMinor: row.TotalPriceMinor,
		CutoffAt:        row.CutoffAt,
	}
}

// ReorderChips returns the user's top-N reorder chips for the target_day,
// each enriched with vendor display_name, items_preview, and available_today.
// nextOffset == -1 indicates no more pages.
func (s *HomeService) ReorderChips(
	ctx context.Context, userID string, targetDay time.Time, offset, limit int,
) ([]ReorderChip, int, error) {
	if limit <= 0 {
		limit = 5
		if s.ReorderLimit > 0 {
			limit = s.ReorderLimit
		}
	}
	rows, err := s.RecentOrders.RecentOrdersByUser(ctx, userID, limit, offset)
	if err != nil {
		return nil, -1, err
	}
	if len(rows) == 0 {
		return nil, -1, nil
	}

	orderIDs := make([]string, 0, len(rows))
	vendorIDs := make([]string, 0, len(rows))
	for _, r := range rows {
		orderIDs = append(orderIDs, r.OrderID)
		vendorIDs = append(vendorIDs, r.VendorID)
	}
	previews, err := s.RecentOrders.ItemNamesByOrderIDs(ctx, orderIDs, 2)
	if err != nil {
		return nil, -1, fmt.Errorf("item names: %w", err)
	}
	availability, err := s.RecentOrders.OrderAvailability(ctx, orderIDs, targetDay)
	if err != nil {
		return nil, -1, fmt.Errorf("order availability: %w", err)
	}
	vendorNames := map[string]string{}
	if s.VendorNames != nil {
		vendorNames, err = s.VendorNames(ctx, vendorIDs)
		if err != nil {
			return nil, -1, fmt.Errorf("vendor names: %w", err)
		}
	}

	out := make([]ReorderChip, 0, len(rows))
	for _, r := range rows {
		out = append(out, ReorderChip{
			SourceOrderID:   r.OrderID,
			VendorID:        r.VendorID,
			VendorName:      vendorNames[r.VendorID],
			ItemsPreview:    previews[r.OrderID],
			TotalPriceMinor: r.TotalPriceMinor,
			Freq:            r.Freq,
			AvailableToday:  availability[r.OrderID],
		})
	}
	next := -1
	if len(rows) == limit {
		next = offset + limit
	}
	return out, next, nil
}

// FavoriteChipsList delegates to the favorites repo (used by both the home
// endpoint and the "see more" favorites endpoint owned by another agent).
func (s *HomeService) FavoriteChipsList(
	ctx context.Context, userID, targetDay, plant string, limit int, cursor *time.Time,
) ([]FavoriteChip, *time.Time, error) {
	if limit <= 0 {
		limit = 5
		if s.FavoriteLimit > 0 {
			limit = s.FavoriteLimit
		}
	}
	return s.FavoritesRepo.ListByUser(ctx, userID, targetDay, plant, limit, cursor)
}

// RecommendChips assembles recommendation chips by combining plant popularity,
// normalised user vendor affinity, and the recommender Score function.
// nextOffset == -1 means "no more pages".
func (s *HomeService) RecommendChips(
	ctx context.Context, userID, plant string, targetDay time.Time, offset, limit int,
) ([]RecommendChip, int, error) {
	if limit <= 0 {
		limit = 5
		if s.RecommendLimit > 0 {
			limit = s.RecommendLimit
		}
	}

	popularity, err := s.Popularity.PlantPopularity(ctx, plant, targetDay)
	if err != nil {
		return nil, -1, fmt.Errorf("plant popularity: %w", err)
	}
	if len(popularity) == 0 {
		return nil, -1, nil
	}
	rawAffinity, err := s.Affinity.UserVendorAffinity(ctx, userID)
	if err != nil {
		return nil, -1, fmt.Errorf("user vendor affinity: %w", err)
	}
	affinity := normaliseAffinity(rawAffinity)

	ids := make([]string, 0, len(popularity))
	for id := range popularity {
		ids = append(ids, id)
	}
	items, err := s.Popularity.MetaByIDs(ctx, ids)
	if err != nil {
		return nil, -1, fmt.Errorf("meta by ids: %w", err)
	}

	favIDs, err := s.favoriteIDSet(ctx, userID, targetDay.Format(dateLayout), plant)
	if err != nil {
		return nil, -1, fmt.Errorf("favorite id set: %w", err)
	}

	all := Score(RecommendInputs{
		Popularity:     popularity,
		VendorAffinity: affinity,
		Items:          items,
		FavoriteIDs:    favIDs,
		Alpha:          s.Alpha,
		Limit:          0, // 0 == no truncation; paginate manually below.
	})
	start := offset
	if start < 0 {
		start = 0
	}
	if start >= len(all) {
		return nil, -1, nil
	}
	end := start + limit
	if end > len(all) {
		end = len(all)
	}
	page := all[start:end]
	next := -1
	if end < len(all) {
		next = end
	}
	return page, next, nil
}

// favoriteIDSet returns the user's current favorite menu_item_ids, used to
// exclude them from the recommendation set. 50 is the repo cap; favorites
// > 50 is not a planned P9 UX.
func (s *HomeService) favoriteIDSet(ctx context.Context, userID, targetDay, plant string) (map[string]struct{}, error) {
	chips, _, err := s.FavoritesRepo.ListByUser(ctx, userID, targetDay, plant, 50, nil)
	if err != nil {
		return nil, err
	}
	out := make(map[string]struct{}, len(chips))
	for _, c := range chips {
		out[c.MenuItemID] = struct{}{}
	}
	return out, nil
}

// normaliseAffinity converts raw vendor counts into a 0..1 distribution
// (sums to 1). Empty / zero-sum input ⇒ empty output (cold start). Pure.
func normaliseAffinity(raw map[string]float64) map[string]float64 {
	if len(raw) == 0 {
		return map[string]float64{}
	}
	var sum float64
	for _, v := range raw {
		sum += v
	}
	if sum == 0 {
		return map[string]float64{}
	}
	out := make(map[string]float64, len(raw))
	for k, v := range raw {
		out[k] = v / sum
	}
	return out
}
