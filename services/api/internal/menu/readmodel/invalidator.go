package readmodel

// Outbox-driven cache invalidator (architecture #57 + #59). The
// invalidator subscribes to the ORDERS_V1 JetStream stream and
// drops cache entries scoped to the affected plant/date so the next
// home/menu read recomputes from authoritative state.
//
// The invalidator runs inside the realtime-gateway role and inside
// the api role. Both subscribe with AckNonePolicy + DeliverNewPolicy
// because the cache TTL is a safety net: a missed event becomes
// staleness up to TTL seconds, not data loss.

import (
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/nats-io/nats.go/jetstream"
)

// sanitizeConsumerToken replaces every character that is invalid in a NATS
// durable consumer name (anything outside [A-Za-z0-9_-], e.g. the '.' in a
// macOS hostname like "host.local") with '-'. NATS rejects '.' in names.
func sanitizeConsumerToken(s string) string {
	return strings.Map(func(r rune) rune {
		switch {
		case r >= 'A' && r <= 'Z', r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '_', r == '-':
			return r
		default:
			return '-'
		}
	}, s)
}

// orderEvent is the minimal projection the invalidator needs from
// ORDERS_V1 payloads. Plant + supply_date drive the home/popularity SCAN
// pattern; user_id (present on placed/modified events) drives the affinity
// key.
type orderEvent struct {
	OrderID    string `json:"order_id"`
	UserID     string `json:"user_id"`
	Plant      string `json:"plant"`
	SupplyDate string `json:"supply_date"` // YYYY-MM-DD
}

// RunOrderInvalidator wires the consumer that invalidates read-model
// entries when an order event arrives. Returns when ctx is cancelled
// or the consumer fails to set up; the caller decides whether to
// retry or to exit the process.
func RunOrderInvalidator(ctx context.Context, js jetstream.JetStream, cache Cache, logger *slog.Logger) error {
	stream, err := js.Stream(ctx, "ORDERS_V1")
	if err != nil {
		return err
	}
	hostname, _ := os.Hostname()
	cons, err := stream.CreateOrUpdateConsumer(ctx, jetstream.ConsumerConfig{
		Name:              "readmodel-invalidator-" + sanitizeConsumerToken(hostname),
		FilterSubject:     "order.>",
		AckPolicy:         jetstream.AckNonePolicy,
		DeliverPolicy:     jetstream.DeliverNewPolicy,
		InactiveThreshold: 30 * time.Second,
	})
	if err != nil {
		return err
	}
	cc, err := cons.Consume(func(msg jetstream.Msg) {
		var p orderEvent
		if err := json.Unmarshal(msg.Data(), &p); err != nil {
			return
		}
		if p.Plant == "" || p.SupplyDate == "" {
			return
		}
		// Home + plant popularity are both plant/date keyed; affinity is
		// keyed by user (only invalidated when the event carries user_id).
		patterns := []string{
			HomeKeyPattern(p.Plant, p.SupplyDate),
			PopularityKeyPattern(p.Plant, p.SupplyDate),
		}
		if p.UserID != "" {
			patterns = append(patterns, AffinityKeyPattern(p.UserID))
		}
		for _, pattern := range patterns {
			if err := cache.Invalidate(ctx, pattern); err != nil {
				logger.Warn("invalidate failed", "pattern", pattern, "err", err)
			}
		}
	})
	if err != nil {
		return err
	}
	defer cc.Stop()
	logger.Info("readmodel invalidator started", "stream", "ORDERS_V1")
	<-ctx.Done()
	return nil
}
