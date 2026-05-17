package order

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/nats-io/nats.go/jetstream"
)

// BoardEvent is the lightweight payload pushed to a merchant's live prep
// board. The board re-fetches its data on receipt, so only the change kind
// and the affected order id need to travel.
type BoardEvent struct {
	Kind    string `json:"kind" doc:"Order event kind, e.g. placed / modified / ready / cancelled"`
	OrderID string `json:"order_id" doc:"Affected order id; empty for keep-alive pings"`
}

// BoardHub fans order events out to per-vendor SSE subscribers. It is safe for
// concurrent use.
type BoardHub struct {
	mu   sync.Mutex
	subs map[string]map[chan BoardEvent]struct{} // vendorID -> set of subscriber channels
}

// NewBoardHub returns an empty hub ready for Subscribe / Publish.
func NewBoardHub() *BoardHub {
	return &BoardHub{subs: map[string]map[chan BoardEvent]struct{}{}}
}

// Subscribe registers a subscriber for vendorID and returns its event channel
// plus an unsubscribe func the caller MUST invoke (e.g. via defer) when done.
// The channel is buffered; the unsubscribe func closes it.
func (h *BoardHub) Subscribe(vendorID string) (<-chan BoardEvent, func()) {
	ch := make(chan BoardEvent, 16)
	h.mu.Lock()
	if h.subs[vendorID] == nil {
		h.subs[vendorID] = map[chan BoardEvent]struct{}{}
	}
	h.subs[vendorID][ch] = struct{}{}
	h.mu.Unlock()

	return ch, func() {
		h.mu.Lock()
		defer h.mu.Unlock()
		set := h.subs[vendorID]
		if set == nil {
			return
		}
		if _, ok := set[ch]; ok {
			delete(set, ch)
			close(ch)
		}
		if len(set) == 0 {
			delete(h.subs, vendorID)
		}
	}
}

// Publish delivers ev to every current subscriber of vendorID. A subscriber
// whose buffer is full is skipped rather than blocked — the board re-fetches
// on the next event it does receive, so a dropped ping is harmless.
func (h *BoardHub) Publish(vendorID string, ev BoardEvent) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for ch := range h.subs[vendorID] {
		select {
		case ch <- ev:
		default:
		}
	}
}

// kindFromSubject extracts the event kind from a NATS subject such as
// "order.placed.v1" -> "placed". Unrecognised shapes return the raw subject.
func kindFromSubject(subject string) string {
	parts := strings.Split(subject, ".")
	if len(parts) >= 2 {
		return parts[1]
	}
	return subject
}

// RunBoardConsumer taps the ORDERS_V1 stream and feeds order events into the
// hub until ctx is cancelled. It uses an ephemeral, ack-none, deliver-new
// consumer: the board is a live view, so missed or replayed events are
// undesirable — only events that occur while a board is open matter.
func RunBoardConsumer(ctx context.Context, js jetstream.JetStream, hub *BoardHub, logger *slog.Logger) error {
	stream, err := js.Stream(ctx, "ORDERS_V1")
	if err != nil {
		return err
	}
	cons, err := stream.CreateOrUpdateConsumer(ctx, jetstream.ConsumerConfig{
		FilterSubject:     "order.>",
		AckPolicy:         jetstream.AckNonePolicy,
		DeliverPolicy:     jetstream.DeliverNewPolicy,
		InactiveThreshold: time.Hour,
	})
	if err != nil {
		return err
	}
	cc, err := cons.Consume(func(msg jetstream.Msg) {
		var p struct {
			OrderID  string `json:"order_id"`
			VendorID string `json:"vendor_id"`
		}
		if err := json.Unmarshal(msg.Data(), &p); err != nil {
			return
		}
		if p.VendorID == "" {
			return
		}
		hub.Publish(p.VendorID, BoardEvent{Kind: kindFromSubject(msg.Subject()), OrderID: p.OrderID})
	})
	if err != nil {
		return err
	}
	defer cc.Stop()
	logger.Info("board consumer started, tapping ORDERS_V1")
	<-ctx.Done()
	return nil
}
