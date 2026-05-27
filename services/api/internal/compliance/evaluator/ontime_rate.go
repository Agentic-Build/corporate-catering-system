// Package evaluator hosts streaming compliance evaluators that subscribe to
// domain events on JetStream and emit anomalies when a vendor's behaviour
// crosses a threshold.
//
// OnTimeRateEvaluator maintains a per-vendor rolling window of order pickup
// outcomes (picked_up vs. no_show) and opens an `on_time_rate_drop` anomaly
// when the pickup rate falls below Threshold. State is kept in-memory, so a
// single replica is assumed for P6. A multi-replica deployment would need a
// shared store (Redis ZSET keyed by vendor) — explicitly out of scope here.
package evaluator

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/nats-io/nats.go/jetstream"

	"github.com/takalawang/corporate-catering-system/services/api/internal/compliance"
	"github.com/takalawang/corporate-catering-system/services/api/internal/platform/messaging"
)

// onTimeEvent is the minimal record kept in the rolling window.
type onTimeEvent struct {
	timestamp time.Time
	pickedUp  bool
}

// OnTimeRateEvaluator subscribes to order.picked_up.v1 + order.no_show.v1
// on ORDERS_V1, maintains a rolling Window per vendor, and opens an anomaly
// when the on-time rate (picked_up / total) falls below Threshold. The
// evaluator only emits once at least MinSamples events are in the window
// to avoid early false positives.
type OnTimeRateEvaluator struct {
	JS jetstream.JetStream
	// Pool backs DLQ writes when a message exhausts its delivery attempts.
	// Nil disables DLQ (the message is Nak'd instead).
	Pool       *pgxpool.Pool
	Anomaly    compliance.AnomalyRepository
	Window     time.Duration
	Threshold  float64
	HighThresh float64
	MinSamples int
	Logger     *slog.Logger

	mu   sync.Mutex
	data map[string][]onTimeEvent
}

// Run subscribes to the order events and processes them serially until ctx
// is cancelled. The consumer is a durable pull consumer so it survives
// worker restarts — but the in-memory window does NOT survive restart.
func (e *OnTimeRateEvaluator) Run(ctx context.Context) error {
	if e.Window <= 0 {
		e.Window = 7 * 24 * time.Hour
	}
	if e.Threshold == 0 {
		e.Threshold = 0.95
	}
	if e.HighThresh == 0 {
		e.HighThresh = 0.90
	}
	if e.MinSamples == 0 {
		e.MinSamples = 10
	}
	if e.data == nil {
		e.data = map[string][]onTimeEvent{}
	}

	stream, err := e.JS.Stream(ctx, "ORDERS_V1")
	if err != nil {
		return fmt.Errorf("get stream: %w", err)
	}
	cons, err := stream.CreateOrUpdateConsumer(ctx, jetstream.ConsumerConfig{
		Name:           "on-time-evaluator",
		Durable:        "on-time-evaluator",
		FilterSubjects: []string{"order.picked_up.v1", "order.no_show.v1"},
		AckPolicy:      jetstream.AckExplicitPolicy,
		MaxDeliver:     5,
	})
	if err != nil {
		return fmt.Errorf("create consumer: %w", err)
	}

	e.Logger.Info("on-time-rate evaluator started",
		"window", e.Window,
		"threshold", e.Threshold,
		"high_threshold", e.HighThresh,
		"min_samples", e.MinSamples,
	)

	it, err := cons.Messages()
	if err != nil {
		return fmt.Errorf("messages: %w", err)
	}
	defer it.Stop()
	go func() {
		<-ctx.Done()
		it.Stop()
	}()

	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		msg, err := it.Next()
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if errors.Is(err, jetstream.ErrMsgIteratorClosed) {
				return ctx.Err()
			}
			e.Logger.Warn("consumer next", "err", err)
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(500 * time.Millisecond):
			}
			continue
		}
		if err := e.handle(ctx, msg.Subject(), msg.Data()); err != nil {
			e.Logger.Warn("handle event", "subject", msg.Subject(), "err", err)
			// MaxDeliver above is 5; once exhausted, DLQ + Term so a poison
			// event stops being redelivered (and double-counted) forever.
			messaging.DLQOnExhaustion(ctx, msg, e.Pool, "on-time-evaluator", 5, err)
			continue
		}
		_ = msg.Ack()
	}
}

// handle parses a single event, updates the per-vendor rolling window, and
// opens an anomaly if the rate has dropped below Threshold (with at least
// MinSamples events in-window).
func (e *OnTimeRateEvaluator) handle(ctx context.Context, subject string, data []byte) error {
	var payload struct {
		VendorID string `json:"vendor_id"`
	}
	if err := json.Unmarshal(data, &payload); err != nil {
		return fmt.Errorf("decode payload: %w", err)
	}
	if payload.VendorID == "" {
		// No vendor on the event — nothing to evaluate. Ack as a no-op.
		return nil
	}
	pickedUp := subject == "order.picked_up.v1"

	now := time.Now()
	cutoff := now.Add(-e.Window)

	e.mu.Lock()
	events := append(e.data[payload.VendorID], onTimeEvent{timestamp: now, pickedUp: pickedUp})
	pruned := events[:0]
	for _, ev := range events {
		if ev.timestamp.After(cutoff) {
			pruned = append(pruned, ev)
		}
	}
	// Reallocate so the slice's underlying array doesn't grow unboundedly
	// even when the window keeps shifting.
	stored := make([]onTimeEvent, len(pruned))
	copy(stored, pruned)
	e.data[payload.VendorID] = stored
	total := len(stored)
	pickedUpCount := 0
	for _, ev := range stored {
		if ev.pickedUp {
			pickedUpCount++
		}
	}
	e.mu.Unlock()

	if total < e.MinSamples {
		return nil
	}
	rate := float64(pickedUpCount) / float64(total)
	if rate >= e.Threshold {
		return nil
	}

	sev := compliance.SeverityMedium
	if rate < e.HighThresh {
		sev = compliance.SeverityHigh
	}
	a := &compliance.Anomaly{
		Kind:       "on_time_rate_drop",
		TargetKind: "vendor",
		TargetID:   payload.VendorID,
		Severity:   sev,
		Payload: map[string]any{
			"rate":         rate,
			"total":        total,
			"picked_up":    pickedUpCount,
			"window_hours": e.Window.Hours(),
		},
	}
	if err := e.Anomaly.Open(ctx, a); err != nil {
		return fmt.Errorf("open anomaly: %w", err)
	}
	return nil
}
