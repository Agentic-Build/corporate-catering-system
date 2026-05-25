package postgres

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

// supplyMetricsQuery bounds cardinality to a ±2-week window of active dates so
// the observable gauges stay well under the documented ~3k series budget.
const supplyMetricsQuery = `
SELECT to_char(ms.supply_date, 'YYYY-MM-DD') AS supply_date,
       mi.vendor_id, mi.id AS menu_item_id,
       mi.name AS item_name, v.display_name AS vendor_name,
       ms.capacity, ms.remain
  FROM meal_supply ms
  JOIN menu_item mi ON mi.id = ms.menu_item_id
  JOIN vendor v ON v.id = mi.vendor_id
 WHERE ms.supply_date >= CURRENT_DATE - INTERVAL '1 day'
   AND ms.supply_date <= CURRENT_DATE + INTERVAL '14 days'`

// RegisterSupplyGauges registers three OpenTelemetry observable gauges on the
// "tbite.api" meter that reflect vendor supply/capacity straight from
// meal_supply. Names mirror Grafana supply-health.json exactly:
//
//   - tbite_supply_capacity        attrs: supply_date, vendor_id (SUM per vendor/date)
//   - tbite_supply_remain          attrs: supply_date, vendor_id (SUM per vendor/date)
//   - tbite_item_supply_capacity   attrs: menu_item_id, item_name, vendor_name, vendor_id (per item)
//
// A single callback runs the bounded query on each metric collection and
// observes every aggregate/row. A query error is returned (OTel logs it) rather
// than panicking, so a transient DB hiccup just skips one scrape.
func RegisterSupplyGauges(pool *pgxpool.Pool) error {
	meter := otel.GetMeterProvider().Meter("tbite.api")

	capacityGauge, err := meter.Int64ObservableGauge("tbite_supply_capacity")
	if err != nil {
		return err
	}
	remainGauge, err := meter.Int64ObservableGauge("tbite_supply_remain")
	if err != nil {
		return err
	}
	itemCapacityGauge, err := meter.Int64ObservableGauge("tbite_item_supply_capacity")
	if err != nil {
		return err
	}

	_, err = meter.RegisterCallback(func(ctx context.Context, o metric.Observer) error {
		rows, err := pool.Query(ctx, supplyMetricsQuery)
		if err != nil {
			return fmt.Errorf("supply gauges query: %w", err)
		}
		defer rows.Close()

		type vendorDate struct {
			date     string
			vendorID string
		}
		capByVD := map[vendorDate]int64{}
		remByVD := map[vendorDate]int64{}

		for rows.Next() {
			var (
				supplyDate           string
				vendorID, itemID     string
				itemName, vendorName string
				capacity, remain     int64
			)
			if err := rows.Scan(&supplyDate, &vendorID, &itemID, &itemName, &vendorName, &capacity, &remain); err != nil {
				return fmt.Errorf("supply gauges scan: %w", err)
			}
			vd := vendorDate{date: supplyDate, vendorID: vendorID}
			capByVD[vd] += capacity
			remByVD[vd] += remain

			o.ObserveInt64(itemCapacityGauge, capacity, metric.WithAttributes(
				attribute.String("menu_item_id", itemID),
				attribute.String("item_name", itemName),
				attribute.String("vendor_name", vendorName),
				attribute.String("vendor_id", vendorID),
			))
		}
		if err := rows.Err(); err != nil {
			return fmt.Errorf("supply gauges rows: %w", err)
		}

		for vd, total := range capByVD {
			o.ObserveInt64(capacityGauge, total, metric.WithAttributes(
				attribute.String("supply_date", vd.date),
				attribute.String("vendor_id", vd.vendorID),
			))
		}
		for vd, total := range remByVD {
			o.ObserveInt64(remainGauge, total, metric.WithAttributes(
				attribute.String("supply_date", vd.date),
				attribute.String("vendor_id", vd.vendorID),
			))
		}
		return nil
	}, capacityGauge, remainGauge, itemCapacityGauge)

	return err
}
