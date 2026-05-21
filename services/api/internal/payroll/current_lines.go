package payroll

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

// currentLinesQuery returns one row per chargeable order in the employee's
// in-progress payroll period. See Service.ListCurrentLines for the definition
// of "current period".
const currentLinesQuery = `
WITH last_locked AS (
  SELECT max(period_end) AS period_end
    FROM payroll_batch
   WHERE status IN ('locked', 'exported', 'closed')
)
SELECT o.id,
       to_char(o.supply_date, 'YYYY-MM-DD') AS supply_date,
       v.display_name AS vendor_name,
       COALESCE(items.summary, '') AS items_summary,
       o.total_price_minor,
       CASE o.status
         WHEN 'no_show'  THEN 'no_show'
         WHEN 'refunded' THEN 'reversed'
         ELSE 'charged'
       END AS line_status,
       (rt.order_id IS NOT NULL) AS rated,
       cp.complaint_id
  FROM "order" o
  JOIN vendor v ON v.id = o.vendor_id
  LEFT JOIN LATERAL (
    SELECT string_agg(oi.qty || 'x ' || mi.name, ', ' ORDER BY mi.name) AS summary
      FROM order_item oi
      JOIN menu_item mi ON mi.id = oi.menu_item_id
     WHERE oi.order_id = o.id
  ) items ON true
  LEFT JOIN meal_rating rt ON rt.order_id = o.id
  LEFT JOIN LATERAL (
    SELECT mc.id AS complaint_id
      FROM meal_complaint mc
     WHERE mc.order_id = o.id
     ORDER BY mc.created_at DESC
     LIMIT 1
  ) cp ON true
 WHERE o.user_id = $1
   AND o.status IN ('picked_up', 'no_show', 'refunded')
   AND o.supply_date > COALESCE((SELECT period_end FROM last_locked), '0001-01-01'::date)
 ORDER BY o.supply_date DESC, o.id`

// QueryCurrentLines runs the current-payroll-lines query against pool. It is
// the single canonical implementation: Service.ListCurrentLines uses it as the
// default, and postgres.CurrentLinesRepo delegates to it so tests exercise the
// exact production query.
func QueryCurrentLines(ctx context.Context, pool *pgxpool.Pool, userID string) ([]CurrentPayrollLine, error) {
	rows, err := pool.Query(ctx, currentLinesQuery, userID)
	if err != nil {
		return nil, fmt.Errorf("query current lines: %w", err)
	}
	defer rows.Close()

	var out []CurrentPayrollLine
	for rows.Next() {
		var l CurrentPayrollLine
		if err := rows.Scan(
			&l.OrderID, &l.SupplyDate, &l.VendorName, &l.ItemsSummary,
			&l.AmountMinor, &l.Status, &l.Rated, &l.ComplaintID,
		); err != nil {
			return nil, fmt.Errorf("scan current line: %w", err)
		}
		out = append(out, l)
	}
	return out, rows.Err()
}
