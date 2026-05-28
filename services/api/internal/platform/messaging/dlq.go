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

// dlqMessages returns the lazily-bound DLQ write counter. Nil-safe: callers no-op
// when the meter rejects the instrument or it was never bound.
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

// DLQEntry bundles the message-failure attributes WriteDLQ records.
type DLQEntry struct {
	Stream    string
	Subject   string
	Consumer  string
	Payload   map[string]any
	Headers   map[string]any
	LastError string
}

// WriteDLQ records an irrecoverable message failure to dlq_message. Workers
// call this when MaxDeliver is exceeded or processing can't be retried.
// Deliberately not inside any per-message tx — it's the escape hatch when
// the normal tx couldn't succeed. Visible via GET /api/admin/dlq.
func WriteDLQ(ctx context.Context, pool *pgxpool.Pool, e DLQEntry) error {
	payload := e.Payload
	if payload == nil {
		payload = map[string]any{}
	}
	headers := e.Headers
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
		e.Stream, e.Subject, e.Consumer, p, h, e.LastError); err != nil {
		return fmt.Errorf("write dlq: %w", err)
	}
	if c := dlqMessages(); c != nil {
		c.Add(ctx, 1, metric.WithAttributes(attribute.String("source_stream", e.Stream)))
	}
	return nil
}

// DLQOnExhaustion: durable-consumer terminal-failure handler. Once
// NumDelivered >= maxDeliver, write the DLQ and Term so JetStream stops
// redelivering; before exhaustion, Nak for another attempt. Returns true when
// DLQ'd. If the DLQ write itself fails, Nak instead (don't silently drop).
func DLQOnExhaustion(ctx context.Context, msg jetstream.Msg, pool *pgxpool.Pool, consumer string, maxDeliver int, procErr error) bool {
	meta, err := msg.Metadata()
	if err != nil || pool == nil || meta.NumDelivered < uint64(maxDeliver) {
		_ = msg.Nak()
		return false
	}
	var payload map[string]any
	_ = json.Unmarshal(msg.Data(), &payload)
	if WriteDLQ(ctx, pool, DLQEntry{
		Stream:    meta.Stream,
		Subject:   msg.Subject(),
		Consumer:  consumer,
		Payload:   payload,
		LastError: procErr.Error(),
	}) != nil {
		_ = msg.Nak()
		return false
	}
	_ = msg.Term()
	return true
}
