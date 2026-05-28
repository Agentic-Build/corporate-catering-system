package postgres

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Agentic-Build/corporate-catering-system/services/api/internal/menu"
)

// RecentOrdersRepo serves the read-only queries behind the employee Home
// "再點一次" chips and target-day order check. Projections live in menu
// (RecentOrderRow, UserOrderToday) to break a menu→order/postgres→order→menu cycle.
type RecentOrdersRepo struct{ pool *pgxpool.Pool }

func NewRecentOrdersRepo(p *pgxpool.Pool) *RecentOrdersRepo { return &RecentOrdersRepo{pool: p} }

// RecentOrdersByUser returns one row per vendor the user ordered from in the
// last 30 days (most-recent order with that vendor), ranked by frequency.
// Status set: cutoff/ready/picked_up (excludes placed = pre-cutoff and
// no_show/cancelled = not worth re-offering).
func (r *RecentOrdersRepo) RecentOrdersByUser(
	ctx context.Context, userID string, limit, offset int,
) ([]menu.RecentOrderRow, error) {
	if limit < 1 {
		limit = 1
	}
	if limit > 50 {
		limit = 50
	}
	if offset < 0 {
		offset = 0
	}
	rows, err := r.pool.Query(ctx, `
WITH ranked AS (
  SELECT o.id, o.vendor_id, o.supply_date, o.total_price_minor,
         COUNT(*) OVER (PARTITION BY o.vendor_id) AS freq,
         ROW_NUMBER() OVER (PARTITION BY o.vendor_id ORDER BY o.supply_date DESC, o.id) AS rn
    FROM "order" o
   WHERE o.user_id = $1
     AND o.status IN ('cutoff','ready','picked_up')
     AND o.supply_date >= CURRENT_DATE - INTERVAL '30 days'
)
SELECT id, vendor_id, supply_date, total_price_minor, freq::int
  FROM ranked
 WHERE rn = 1
 ORDER BY freq DESC, supply_date DESC, id
 LIMIT $2 OFFSET $3`, userID, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]menu.RecentOrderRow, 0, limit)
	for rows.Next() {
		var rc menu.RecentOrderRow
		if err := rows.Scan(&rc.OrderID, &rc.VendorID, &rc.SupplyDate, &rc.TotalPriceMinor, &rc.Freq); err != nil {
			return nil, err
		}
		out = append(out, rc)
	}
	return out, rows.Err()
}

// GetOrderByUserDate returns the user's order for (supply_date, plant) if one
// exists, or (nil, nil) when absent. Used by HomeService to derive target_day.
func (r *RecentOrdersRepo) GetOrderByUserDate(
	ctx context.Context, userID string, day time.Time, plant string,
) (*menu.UserOrderToday, error) {
	var u menu.UserOrderToday
	err := r.pool.QueryRow(ctx, `
SELECT id, vendor_id, status::text, total_price_minor, cutoff_at, picked_up_at
  FROM "order"
 WHERE user_id=$1 AND supply_date=$2 AND plant=$3
 ORDER BY created_at DESC
 LIMIT 1`, userID, day, plant).
		Scan(&u.OrderID, &u.VendorID, &u.Status, &u.TotalPriceMinor, &u.CutoffAt, &u.PickedUpAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &u, nil
}

// ItemNamesByOrderIDs returns up to `cap` item names per order_id (sorted by
// menu_item.name). Single batched query for items_preview.
func (r *RecentOrdersRepo) ItemNamesByOrderIDs(
	ctx context.Context, orderIDs []string, cap int,
) (map[string][]string, error) {
	out := make(map[string][]string, len(orderIDs))
	if len(orderIDs) == 0 {
		return out, nil
	}
	if cap < 1 {
		cap = 1
	}
	rows, err := r.pool.Query(ctx, `
SELECT oi.order_id, mi.name
  FROM order_item oi
  JOIN menu_item mi ON mi.id = oi.menu_item_id
 WHERE oi.order_id = ANY($1)
 ORDER BY oi.order_id, mi.name`, orderIDs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var oid, name string
		if err := rows.Scan(&oid, &name); err != nil {
			return nil, err
		}
		if len(out[oid]) >= cap {
			continue
		}
		out[oid] = append(out[oid], name)
	}
	return out, rows.Err()
}

// OrderAvailability returns order_id → bool: true iff at least one of the
// order's items has a meal_supply row on `day`. Single batched query.
func (r *RecentOrdersRepo) OrderAvailability(
	ctx context.Context, orderIDs []string, day time.Time,
) (map[string]bool, error) {
	out := make(map[string]bool, len(orderIDs))
	for _, id := range orderIDs {
		out[id] = false
	}
	if len(orderIDs) == 0 {
		return out, nil
	}
	rows, err := r.pool.Query(ctx, `
SELECT DISTINCT oi.order_id
  FROM order_item oi
  JOIN meal_supply ms
    ON ms.menu_item_id = oi.menu_item_id AND ms.supply_date = $2
 WHERE oi.order_id = ANY($1)`, orderIDs, day)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var oid string
		if err := rows.Scan(&oid); err != nil {
			return nil, err
		}
		out[oid] = true
	}
	return out, rows.Err()
}
