package order

import (
	"context"
	"encoding/json"
	"log/slog"
	"os"
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

// SubscriberCount returns the total number of board subscribers across all vendors.
func (h *BoardHub) SubscriberCount() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	n := 0
	for _, set := range h.subs {
		n += len(set)
	}
	return n
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

// MenuHub broadcasts a "menu changed" signal to every subscribed employee
// menu view. Unlike BoardHub it has no per-vendor key: any order activity can
// change remaining quota, so every open menu refetches. It is safe for
// concurrent use.
type MenuHub struct {
	mu   sync.Mutex
	subs map[chan struct{}]struct{}
}

// NewMenuHub returns an empty broadcast hub.
func NewMenuHub() *MenuHub {
	return &MenuHub{subs: map[chan struct{}]struct{}{}}
}

// Subscribe registers a subscriber and returns its signal channel plus an
// unsubscribe func the caller MUST invoke when done.
func (h *MenuHub) Subscribe() (<-chan struct{}, func()) {
	ch := make(chan struct{}, 1)
	h.mu.Lock()
	h.subs[ch] = struct{}{}
	h.mu.Unlock()
	return ch, func() {
		h.mu.Lock()
		defer h.mu.Unlock()
		if _, ok := h.subs[ch]; ok {
			delete(h.subs, ch)
			close(ch)
		}
	}
}

// SubscriberCount returns the number of menu subscribers.
func (h *MenuHub) SubscriberCount() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.subs)
}

// Broadcast signals every subscriber. A subscriber whose buffer is already
// full is skipped — one pending "refetch" signal is enough.
func (h *MenuHub) Broadcast() {
	h.mu.Lock()
	defer h.mu.Unlock()
	for ch := range h.subs {
		select {
		case ch <- struct{}{}:
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

// RunBoardConsumer taps the ORDERS_V1 stream and feeds order events into the
// per-vendor board hub and (if non-nil) the broadcast menu hub, until ctx is
// cancelled. It uses an ephemeral, ack-none, deliver-new consumer: the board
// is a live view, so missed or replayed events are undesirable.
func RunBoardConsumer(ctx context.Context, js jetstream.JetStream, hub *BoardHub, menuHub *MenuHub, logger *slog.Logger, onStarted func()) error {
	stream, err := js.Stream(ctx, "ORDERS_V1")
	if err != nil {
		return err
	}
	// Name per-pod so monitoring shows distinct entries; short InactiveThreshold
	// reaps crashed-pod consumers fast (else they zombie for ~1h and inflate lag).
	hostname, _ := os.Hostname()
	cons, err := stream.CreateOrUpdateConsumer(ctx, jetstream.ConsumerConfig{
		Name:              "board-fanout-" + sanitizeConsumerToken(hostname),
		FilterSubject:     "order.>",
		AckPolicy:         jetstream.AckNonePolicy,
		DeliverPolicy:     jetstream.DeliverNewPolicy,
		InactiveThreshold: 30 * time.Second,
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
		if menuHub != nil {
			menuHub.Broadcast()
		}
	})
	if err != nil {
		return err
	}
	defer cc.Stop()
	if onStarted != nil {
		onStarted()
	}
	logger.Info("board consumer started, tapping ORDERS_V1")
	<-ctx.Done()
	return nil
}
