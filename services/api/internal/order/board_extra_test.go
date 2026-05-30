package order

import "testing"

// TestBoardHub_DoubleUnsubscribe exercises the unsubscribe closure's
// `set == nil` guard: the first call removes the only subscriber and deletes
// the now-empty vendor set; the second call finds a nil set and returns early
// without panicking or double-closing the channel.
func TestBoardHub_DoubleUnsubscribe(t *testing.T) {
	hub := NewBoardHub()
	_, unsub := hub.Subscribe("vendor-1")

	unsub() // deletes ch and the empty vendor set
	unsub() // set is now nil → early return (no panic, no double close)

	if got := hub.SubscriberCount(); got != 0 {
		t.Fatalf("SubscriberCount = %d, want 0", got)
	}
}
