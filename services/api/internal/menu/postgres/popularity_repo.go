package postgres

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/takalawang/corporate-catering-system/services/api/internal/menu"
)

// PopularityRepo serves three closely related read-only queries that back the
// employee home page: plant-level menu_item popularity, target-day cutoff
// status (all menu items past cutoff → bump to tomorrow), and item-meta
// lookups for the recommender candidate set.
type PopularityRepo struct{ pool *pgxpool.Pool }

func NewPopularityRepo(p *pgxpool.Pool) *PopularityRepo { return &PopularityRepo{pool: p} }

// PlantPopularity returns the order_item qty sum per menu_item for the given
// plant + supply_date, considering only orders that materialised past the
// cutoff (cutoff, ready, picked_up). Cancelled / placed / no_show / draft are
// excluded — they do not represent "what colleagues actually ate today".
//
// Status set note: the plan doc names ('confirmed','ready','picked_up') but
// the order_status enum has no 'confirmed' member (see migration 000003 +
// order/state_machine.go). The equivalent post-cutoff materialised states are
// (cutoff, ready, picked_up); this is the set the popularity signal needs.
func (r *PopularityRepo) PlantPopularity(ctx context.Context, plant string, day time.Time) (map[string]float64, error) {
	rows, err := r.pool.Query(ctx, `
SELECT oi.menu_item_id, SUM(oi.qty)::float AS popularity
  FROM "order" o
  JOIN order_item oi ON oi.order_id = o.id
 WHERE o.supply_date = $1 AND o.plant = $2
   AND o.status IN ('cutoff','ready','picked_up')
 GROUP BY oi.menu_item_id`, day, plant)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make(map[string]float64)
	for rows.Next() {
		var id string
		var pop float64
		if err := rows.Scan(&id, &pop); err != nil {
			return nil, err
		}
		out[id] = pop
	}
	return out, rows.Err()
}

// MetaByIDs returns minimal item meta for the given ids, filtered to non-archived
// items. Returned order matches DB scan order (caller does not rely on order).
func (r *PopularityRepo) MetaByIDs(ctx context.Context, ids []string) ([]menu.MenuItemMeta, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	rows, err := r.pool.Query(ctx, `
SELECT id, name, price_minor, vendor_id
  FROM menu_item
 WHERE id = ANY($1) AND archived_at IS NULL`, ids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]menu.MenuItemMeta, 0, len(ids))
	for rows.Next() {
		var m menu.MenuItemMeta
		if err := rows.Scan(&m.ID, &m.Name, &m.UnitPrice, &m.VendorID); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// AllCutoffsPassed returns true iff at least one meal_supply row exists for the
// (plant, day) AND every such row's cutoff_at is ≤ now. Empty supply ⇒ false
// (we have no signal; caller leaves the user on "today"). Plant scoping is via
// vendor_plant_mapping → the same active+approved vendor filter the employee
// menu view uses.
func (r *PopularityRepo) AllCutoffsPassed(ctx context.Context, plant string, day time.Time, now time.Time) (bool, error) {
	var total, passed int
	err := r.pool.QueryRow(ctx, `
SELECT COUNT(*)              AS total,
       COUNT(*) FILTER (WHERE ms.cutoff_at <= $3) AS passed
  FROM meal_supply ms
  JOIN menu_item mi ON mi.id = ms.menu_item_id AND mi.status = 'active'
  JOIN vendor v ON v.id = mi.vendor_id AND v.status = 'approved'
  JOIN vendor_plant_mapping vpm
    ON vpm.vendor_id = v.id AND vpm.plant = $1 AND vpm.active = true
 WHERE ms.supply_date = $2`, plant, day, now).Scan(&total, &passed)
	if err != nil {
		return false, err
	}
	if total == 0 {
		return false, nil
	}
	return passed == total, nil
}
