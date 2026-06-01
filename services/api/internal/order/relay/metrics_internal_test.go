package relay

import (
	"context"
	"testing"
)

// TestRecordPublished_NilCounterIsNoop covers the `if c == nil { return }`
// guard in recordPublished: when the lazy bind never produced a counter (the
// meter rejected the instrument), recordPublished must be a no-op and not
// panic. This file is named to sort last so it runs after TestRelay_-
// PublishedCounter has already fired publishedOnce against its ManualReader;
// we then force the cached counter to nil without re-firing the Once.
func TestRecordPublished_NilCounterIsNoop(t *testing.T) {
	// Ensure the Once is fired (no-op if TestRelay_PublishedCounter ran first),
	// so outboxPublishedCounter() returns the cached value rather than binding.
	_ = outboxPublishedCounter()

	saved := publishedCounter
	t.Cleanup(func() { publishedCounter = saved })

	publishedCounter = nil

	// Hits the c == nil early return; must not panic.
	recordPublished(context.Background(), "order")
}
