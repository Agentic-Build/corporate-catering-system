package relay

// Relay-side OTel instrument for the transactional outbox, observed by
// Grafana outbox-and-events.json + async-plane.json.
//
// The counter is lazily bound on first use so a process that never runs
// the relay does not register it; the binding reads the global meter
// provider, which the relay role installs during boot.

import (
	"context"
	"sync"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

var (
	publishedOnce    sync.Once
	publishedCounter metric.Int64Counter
)

// outboxPublishedCounter returns the lazily-bound published counter. It is
// nil-guarded by callers: if the meter rejects the instrument it stays nil.
func outboxPublishedCounter() metric.Int64Counter {
	publishedOnce.Do(func() {
		meter := otel.GetMeterProvider().Meter("tbite.api")
		c, err := meter.Int64Counter("tbite_outbox_published_total",
			metric.WithDescription("Outbox rows successfully published by the relay, by aggregate_type."))
		if err == nil {
			publishedCounter = c
		}
	})
	return publishedCounter
}

// recordPublished increments the published counter once for a single
// successfully-published outbox row, labelled by aggregate_type.
func recordPublished(ctx context.Context, aggregateType string) {
	c := outboxPublishedCounter()
	if c == nil {
		return
	}
	c.Add(ctx, 1, metric.WithAttributes(attribute.String("aggregate_type", aggregateType)))
}
