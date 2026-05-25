package order

import (
	"testing"
	"time"
)

// recv reads one event from ch within a short deadline, or fails.
func recv(t *testing.T, ch <-chan BoardEvent) BoardEvent {
	t.Helper()
	select {
	case ev := <-ch:
		return ev
	case <-time.After(time.Second):
		t.Fatal("expected an event, got none")
		return BoardEvent{}
	}
}

func TestBoardHub_PublishRoutesByVendor(t *testing.T) {
	hub := NewBoardHub()
	v1, unsub1 := hub.Subscribe("vendor-1")
	defer unsub1()
	v2, unsub2 := hub.Subscribe("vendor-2")
	defer unsub2()

	hub.Publish("vendor-1", BoardEvent{Kind: "placed", OrderID: "o1"})

	got := recv(t, v1)
	if got.Kind != "placed" || got.OrderID != "o1" {
		t.Fatalf("vendor-1 got %+v", got)
	}
	// vendor-2 must not receive vendor-1's event.
	select {
	case ev := <-v2:
		t.Fatalf("vendor-2 should not receive vendor-1 events, got %+v", ev)
	case <-time.After(50 * time.Millisecond):
	}
}

func TestBoardHub_UnsubscribeStopsDelivery(t *testing.T) {
	hub := NewBoardHub()
	ch, unsub := hub.Subscribe("vendor-1")
	unsub()

	// Publishing after unsubscribe must not panic and the channel is closed.
	hub.Publish("vendor-1", BoardEvent{Kind: "placed", OrderID: "o1"})
	if _, ok := <-ch; ok {
		t.Fatal("channel should be closed after unsubscribe")
	}
}

func TestBoardHub_FullBufferDropsRatherThanBlocks(t *testing.T) {
	hub := NewBoardHub()
	ch, unsub := hub.Subscribe("vendor-1")
	defer unsub()

	// Publish far more than the channel buffer; Publish must never block.
	done := make(chan struct{})
	go func() {
		for i := 0; i < 1000; i++ {
			hub.Publish("vendor-1", BoardEvent{Kind: "placed", OrderID: "o"})
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Publish blocked on a full subscriber buffer")
	}
	// At least the buffered events are still readable.
	_ = recv(t, ch)
}

func TestBoardHub_SubscriberCountSumsAcrossVendors(t *testing.T) {
	hub := NewBoardHub()
	_, ub1 := hub.Subscribe("vendor-1")
	_, ub2 := hub.Subscribe("vendor-2")
	_, ub3 := hub.Subscribe("vendor-2")

	if got := hub.SubscriberCount(); got != 3 {
		t.Fatalf("SubscriberCount = %d, want 3", got)
	}
	ub1()
	ub2()
	ub3()
	if got := hub.SubscriberCount(); got != 0 {
		t.Fatalf("SubscriberCount after unsubscribe = %d, want 0", got)
	}
}

func TestMenuHub_SubscriberCount(t *testing.T) {
	hub := NewMenuHub()
	_, ua := hub.Subscribe()
	_, ub := hub.Subscribe()

	if got := hub.SubscriberCount(); got != 2 {
		t.Fatalf("SubscriberCount = %d, want 2", got)
	}
	ua()
	ub()
	if got := hub.SubscriberCount(); got != 0 {
		t.Fatalf("SubscriberCount after unsubscribe = %d, want 0", got)
	}
}

func TestKindFromSubject(t *testing.T) {
	cases := map[string]string{
		"order.placed.v1":    "placed",
		"order.modified.v1":  "modified",
		"order.cancelled.v1": "cancelled",
		"weird":              "weird",
	}
	for subject, want := range cases {
		if got := kindFromSubject(subject); got != want {
			t.Errorf("kindFromSubject(%q) = %q, want %q", subject, got, want)
		}
	}
}

func TestMenuHub_BroadcastReachesSubscribers(t *testing.T) {
	hub := NewMenuHub()
	a, unsubA := hub.Subscribe()
	defer unsubA()
	b, unsubB := hub.Subscribe()
	defer unsubB()

	hub.Broadcast()
	for name, ch := range map[string]<-chan struct{}{"A": a, "B": b} {
		select {
		case <-ch:
		case <-time.After(time.Second):
			t.Fatalf("subscriber %s missed the broadcast", name)
		}
	}
}

func TestMenuHub_UnsubscribeStopsDelivery(t *testing.T) {
	hub := NewMenuHub()
	ch, unsub := hub.Subscribe()
	unsub()
	hub.Broadcast() // must not panic on a closed channel
	if _, ok := <-ch; ok {
		t.Fatal("channel should be closed after unsubscribe")
	}
}
