package postgres

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

const (
	dlqPendingQuery = `
SELECT source_stream, count(*)
  FROM dlq_message
 WHERE replayed_at IS NULL AND resolved_at IS NULL
 GROUP BY source_stream`

	dlqOldestQuery = `
SELECT COALESCE(EXTRACT(EPOCH FROM (now() - min(first_seen_at))), 0)
  FROM dlq_message
 WHERE replayed_at IS NULL AND resolved_at IS NULL`
)

// RegisterDLQGauges registers two OpenTelemetry observable gauges on the
// "tbite.api" meter that reflect dead-letter queue health straight from
// dlq_message. Names mirror Grafana async-plane.json exactly:
//
//   - tbite_dlq_pending          attr: source_stream (unresolved rows per stream)
//   - tbite_dlq_oldest_seconds   no attrs, unit "s" (age of oldest unresolved row)
//
// A single callback runs both queries on each metric collection and observes
// the results. A query error is returned (OTel logs it) rather than panicking,
// so a transient DB hiccup just skips one scrape.
func RegisterDLQGauges(pool *pgxpool.Pool) error {
	meter := otel.GetMeterProvider().Meter("tbite.api")

	pendingGauge, err := meter.Int64ObservableGauge("tbite_dlq_pending",
		metric.WithDescription("Unresolved DLQ rows, by source_stream."))
	if err != nil {
		return err
	}
	oldestGauge, err := meter.Float64ObservableGauge("tbite_dlq_oldest_seconds",
		metric.WithDescription("Age in seconds of the oldest unresolved DLQ row."),
		metric.WithUnit("s"))
	if err != nil {
		return err
	}

	_, err = meter.RegisterCallback(func(ctx context.Context, o metric.Observer) error {
		rows, err := pool.Query(ctx, dlqPendingQuery)
		if err != nil {
			return fmt.Errorf("dlq pending query: %w", err)
		}
		defer rows.Close()
		for rows.Next() {
			var (
				sourceStream string
				count        int64
			)
			if err := rows.Scan(&sourceStream, &count); err != nil {
				return fmt.Errorf("dlq pending scan: %w", err)
			}
			o.ObserveInt64(pendingGauge, count, metric.WithAttributes(
				attribute.String("source_stream", sourceStream),
			))
		}
		if err := rows.Err(); err != nil {
			return fmt.Errorf("dlq pending rows: %w", err)
		}

		var oldest float64
		if err := pool.QueryRow(ctx, dlqOldestQuery).Scan(&oldest); err != nil {
			return fmt.Errorf("dlq oldest query: %w", err)
		}
		o.ObserveFloat64(oldestGauge, oldest)
		return nil
	}, pendingGauge, oldestGauge)

	return err
}
