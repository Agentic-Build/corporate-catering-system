package postgres

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"
)

// AffinityRepo computes per-user vendor affinity from the last 30 days of
// materialised orders. The returned counts are RAW (not normalised) — the
// caller (HomeService) divides by the sum so Score() sees a 0..1 distribution.
type AffinityRepo struct{ pool *pgxpool.Pool }

func NewAffinityRepo(p *pgxpool.Pool) *AffinityRepo { return &AffinityRepo{pool: p} }

// UserVendorAffinity returns vendor_id → count(order_item) for the user's
// orders in the last 30 days whose status is in (cutoff, ready, picked_up).
// Returns an empty map for cold-start users; never returns nil.
//
// Status set note: see PopularityRepo.PlantPopularity — the order_status enum
// has no 'confirmed' member; we use the equivalent materialised states.
func (r *AffinityRepo) UserVendorAffinity(ctx context.Context, userID string) (map[string]float64, error) {
	rows, err := r.pool.Query(ctx, `
SELECT mi.vendor_id, COUNT(*)::float AS cnt
  FROM "order" o
  JOIN order_item oi ON oi.order_id = o.id
  JOIN menu_item mi ON mi.id = oi.menu_item_id
 WHERE o.user_id = $1
   AND o.supply_date >= CURRENT_DATE - INTERVAL '30 days'
   AND o.status IN ('cutoff','ready','picked_up')
 GROUP BY mi.vendor_id`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make(map[string]float64)
	for rows.Next() {
		var vid string
		var cnt float64
		if err := rows.Scan(&vid, &cnt); err != nil {
			return nil, err
		}
		out[vid] = cnt
	}
	return out, rows.Err()
}
