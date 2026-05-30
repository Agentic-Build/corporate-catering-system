package readmodel

import (
	"context"
	"errors"
	"testing"
)

// TestGetOrRecomputeNonMissGetErrorRecomputes exercises the branch where
// cache.Get returns an error that is NOT ErrCacheMiss: getOrRecompute records
// an error and still falls through to recompute (treating it like a miss).
func TestGetOrRecomputeNonMissGetErrorRecomputes(t *testing.T) {
	cache := newMapCache()
	cache.getErr = errors.New("backing store down") // not ErrCacheMiss
	inner := &stubPopularity{ret: map[string]float64{"a": 1}}
	p := &CachedPopularity{Inner: inner, Cache: cache, Metrics: NewMetrics()}

	got, err := p.PlantPopularity(context.Background(), "plant-a", testDay)
	if err != nil {
		t.Fatalf("plant popularity: %v", err)
	}
	if inner.calls != 1 {
		t.Fatalf("inner.calls = %d, want 1 (recompute after non-miss Get error)", inner.calls)
	}
	if got["a"] != 1 {
		t.Fatalf("got = %v, want {a:1}", got)
	}
}

// TestGetOrRecomputeDecodeErrorRecomputes exercises the cache-hit-but-corrupt
// branch of getOrRecompute (cache.Get succeeds, codec.Decode fails) via the
// popularity wrapper: it must record an error and fall through to recompute,
// then overwrite the bad entry.
func TestGetOrRecomputeDecodeErrorRecomputes(t *testing.T) {
	cache := newMapCache()
	// Seed the exact popularity key for plant-a / testDay with non-JSON bytes.
	key := "pop:plant-a:" + testDay.Format(dayLayout)
	cache.data[key] = []byte("not-json{")

	inner := &stubPopularity{ret: map[string]float64{"a": 7}}
	p := &CachedPopularity{Inner: inner, Cache: cache, Metrics: NewMetrics()}

	got, err := p.PlantPopularity(context.Background(), "plant-a", testDay)
	if err != nil {
		t.Fatalf("plant popularity: %v", err)
	}
	if inner.calls != 1 {
		t.Fatalf("inner.calls = %d, want 1 (recompute after decode error)", inner.calls)
	}
	if got["a"] != 7 {
		t.Fatalf("got = %v, want {a:7}", got)
	}
	// The corrupt entry was overwritten with the fresh, decodable encoding.
	cache.mu.Lock()
	raw := cache.data[key]
	cache.mu.Unlock()
	if string(raw) == "not-json{" {
		t.Fatalf("corrupt cache entry was not overwritten on recompute")
	}
}
