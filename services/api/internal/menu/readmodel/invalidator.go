package readmodel

// Outbox-driven cache invalidator (#57+#59). Subscribes ORDERS_V1 and drops
// cache entries scoped to the affected plant/date. AckNone + DeliverNew —
// the cache TTL is a safety net so a missed event = staleness, not data loss.

import (
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/nats-io/nats.go/jetstream"
)

// sanitizeConsumerToken replaces NATS-invalid characters (anything outside
// [A-Za-z0-9_-], e.g. '.' in "host.local") with '-'.
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

// orderEvent is the minimal ORDERS_V1 projection: plant + supply_date drive
// home/popularity SCAN patterns; user_id drives the affinity key.
type orderEvent struct {
	OrderID    string `json:"order_id"`
	UserID     string `json:"user_id"`
	Plant      string `json:"plant"`
	SupplyDate string `json:"supply_date"` // YYYY-MM-DD
}

// RunOrderInvalidator wires the consumer that invalidates read-model entries
// on order events. Returns when ctx is cancelled or consumer setup fails.
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
		// Home + popularity are plant/date keyed; affinity is user keyed.
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
