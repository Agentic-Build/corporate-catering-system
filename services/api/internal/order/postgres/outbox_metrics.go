package postgres

// Outbox observable gauges, observed by Grafana outbox-and-events.json +
// async-plane.json. A single callback runs two bounded queries on each
// metric collection and observes the results. A query error is returned
// (OTel logs it) rather than panicking, so a transient DB hiccup just
// skips one scrape.

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

const outboxPendingQuery = `
SELECT aggregate_type, count(*)
  FROM outbox_event
 WHERE published_at IS NULL
 GROUP BY aggregate_type`

const outboxOldestQuery = `
SELECT COALESCE(EXTRACT(EPOCH FROM (now() - min(created_at))), 0)
  FROM outbox_event
 WHERE published_at IS NULL`

// RegisterOutboxGauges registers three OpenTelemetry observable gauges on the
// "tbite.api" meter that reflect outbox backlog straight from outbox_event.
// Names mirror Grafana outbox-and-events.json + async-plane.json exactly:
//
//   - tbite_outbox_pending                    attr: aggregate_type (unpublished row count)
//   - tbite_outbox_oldest_seconds             no attrs, unit "s" (age of oldest unpublished row)
//   - tbite_outbox_oldest_unpublished_seconds no attrs, unit "s" (same value, alternate name)
//
// The two oldest gauges carry the same value under two distinct names because
// the two dashboards reference different names for the same concept.
func RegisterOutboxGauges(pool *pgxpool.Pool) error {
	meter := otel.GetMeterProvider().Meter("tbite.api")

	pendingGauge, err := meter.Int64ObservableGauge("tbite_outbox_pending",
		metric.WithDescription("Unpublished outbox rows, by aggregate_type."))
	if err != nil {
		return err
	}
	oldestGauge, err := meter.Float64ObservableGauge("tbite_outbox_oldest_seconds",
		metric.WithDescription("Age in seconds of the oldest unpublished outbox row."),
		metric.WithUnit("s"))
	if err != nil {
		return err
	}
	oldestUnpublishedGauge, err := meter.Float64ObservableGauge("tbite_outbox_oldest_unpublished_seconds",
		metric.WithDescription("Age in seconds of the oldest unpublished outbox row (alternate name)."),
		metric.WithUnit("s"))
	if err != nil {
		return err
	}

	_, err = meter.RegisterCallback(func(ctx context.Context, o metric.Observer) error {
		rows, err := pool.Query(ctx, outboxPendingQuery)
		if err != nil {
			return fmt.Errorf("outbox pending query: %w", err)
		}
		defer rows.Close()
		for rows.Next() {
			var aggregateType string
			var count int64
			if err := rows.Scan(&aggregateType, &count); err != nil {
				return fmt.Errorf("outbox pending scan: %w", err)
			}
			o.ObserveInt64(pendingGauge, count, metric.WithAttributes(
				attribute.String("aggregate_type", aggregateType)))
		}
		if err := rows.Err(); err != nil {
			return fmt.Errorf("outbox pending rows: %w", err)
		}

		var oldest float64
		if err := pool.QueryRow(ctx, outboxOldestQuery).Scan(&oldest); err != nil {
			return fmt.Errorf("outbox oldest query: %w", err)
		}
		o.ObserveFloat64(oldestGauge, oldest)
		o.ObserveFloat64(oldestUnpublishedGauge, oldest)
		return nil
	}, pendingGauge, oldestGauge, oldestUnpublishedGauge)

	return err
}
