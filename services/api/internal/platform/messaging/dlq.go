package messaging

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/nats-io/nats.go/jetstream"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

var (
	dlqMessagesOnce    sync.Once
	dlqMessagesCounter metric.Int64Counter
)

// dlqMessages returns the lazily-bound DLQ write counter, observed by Grafana
// outbox-and-events.json. It binds on first use against the global meter
// provider so a process that never writes the DLQ does not register it; if the
// meter rejects the instrument it stays nil and callers no-op.
func dlqMessages() metric.Int64Counter {
	dlqMessagesOnce.Do(func() {
		meter := otel.GetMeterProvider().Meter("tbite.api")
		c, err := meter.Int64Counter("tbite_dlq_messages_total",
			metric.WithDescription("Messages written to the dead-letter queue, by source_stream."))
		if err == nil {
			dlqMessagesCounter = c
		}
	})
	return dlqMessagesCounter
}

// WriteDLQ records an irrecoverable message failure to dlq_message.
//
// Workers call this when MaxDeliver is exceeded, or when processing logic
// determines the message can't be retried (e.g. schema mismatch, missing
// referent that will never appear). Once written, the message is visible to
// admins via GET /api/admin/dlq and can be replayed or resolved manually.
//
// This is intentionally a thin helper rather than going through a repository:
// workers already hold a *pgxpool.Pool and we want to keep DLQ writes out of
// any per-message transaction (a DLQ write is the *escape hatch* when the
// normal tx couldn't succeed).
func WriteDLQ(ctx context.Context, pool *pgxpool.Pool, stream, subject, consumer string, payload, headers map[string]any, lastError string) error {
	if payload == nil {
		payload = map[string]any{}
	}
	if headers == nil {
		headers = map[string]any{}
	}
	p, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal dlq payload: %w", err)
	}
	h, err := json.Marshal(headers)
	if err != nil {
		return fmt.Errorf("marshal dlq headers: %w", err)
	}
	if _, err := pool.Exec(ctx, `
INSERT INTO dlq_message (source_stream, source_subject, source_consumer, payload, headers, last_error)
VALUES ($1, $2, $3, $4::jsonb, $5::jsonb, $6)`,
		stream, subject, consumer, p, h, lastError); err != nil {
		return fmt.Errorf("write dlq: %w", err)
	}
	if c := dlqMessages(); c != nil {
		c.Add(ctx, 1, metric.WithAttributes(attribute.String("source_stream", stream)))
	}
	return nil
}

// DLQOnExhaustion is the standard terminal-failure handler for a durable
// consumer: once a message's delivery attempts are exhausted
// (NumDelivered >= maxDeliver, which must match the consumer's MaxDeliver), it
// records the failure to the DLQ and Terms the message so JetStream stops
// redelivering it. Before exhaustion it Naks for another attempt. Returns true
// when the message was DLQ'd. If the DLQ write itself fails it Naks instead, so
// the message is parked rather than silently dropped.
func DLQOnExhaustion(ctx context.Context, msg jetstream.Msg, pool *pgxpool.Pool, consumer string, maxDeliver int, procErr error) bool {
	meta, err := msg.Metadata()
	if err != nil || pool == nil || meta.NumDelivered < uint64(maxDeliver) {
		_ = msg.Nak()
		return false
	}
	var payload map[string]any
	_ = json.Unmarshal(msg.Data(), &payload)
	if werr := WriteDLQ(ctx, pool, meta.Stream, msg.Subject(), consumer, payload, nil, procErr.Error()); werr != nil {
		_ = msg.Nak()
		return false
	}
	_ = msg.Term()
	return true
}
