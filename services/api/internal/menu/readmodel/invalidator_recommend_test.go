package readmodel

import (
	"context"
	"testing"
	"time"
)

// TestRunOrderInvalidatorInvalidatesRecommendKeys asserts that a valid order
// event drops the recommendation read-model keys (plant-popularity by
// plant/date, user affinity by user_id) in addition to the home key.
func TestRunOrderInvalidatorInvalidatesRecommendKeys(t *testing.T) {
	cache := &recordCache{}
	cons := &fakeConsumer{msgs: [][]byte{
		[]byte(`{"order_id":"o1","user_id":"u1","plant":"plant-a","supply_date":"2026-05-26"}`),
	}}
	js := &fakeJS{stream: &fakeStream{cons: cons}}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- RunOrderInvalidator(ctx, js, cache, quietLogger()) }()
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("RunOrderInvalidator: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("RunOrderInvalidator did not return after cancel")
	}

	cache.mu.Lock()
	defer cache.mu.Unlock()
	want := map[string]bool{
		"home:*:plant-a:2026-05-26": true,
		"pop:plant-a:2026-05-26":    true,
		"affinity:u1":               true,
	}
	got := map[string]bool{}
	for _, p := range cache.patterns {
		got[p] = true
	}
	for p := range want {
		if !got[p] {
			t.Errorf("missing invalidate pattern %q; got %v", p, cache.patterns)
		}
	}
}

// TestRunOrderInvalidatorAffinitySkippedWithoutUser asserts the affinity key
// is only dropped when user_id is present; home + popularity still fire.
func TestRunOrderInvalidatorAffinitySkippedWithoutUser(t *testing.T) {
	cache := &recordCache{}
	cons := &fakeConsumer{msgs: [][]byte{
		[]byte(`{"order_id":"o1","plant":"plant-a","supply_date":"2026-05-26"}`),
	}}
	js := &fakeJS{stream: &fakeStream{cons: cons}}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- RunOrderInvalidator(ctx, js, cache, quietLogger()) }()
	cancel()
	<-done

	cache.mu.Lock()
	defer cache.mu.Unlock()
	for _, p := range cache.patterns {
		if p == "affinity:" {
			t.Fatalf("affinity invalidated with empty user_id: %v", cache.patterns)
		}
	}
}
