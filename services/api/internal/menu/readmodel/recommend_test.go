package readmodel

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

// mapCache is an in-memory Cache that records Set keys and supports
// pattern-prefix Invalidate (the test patterns end with "*").
type mapCache struct {
	mu      sync.Mutex
	data    map[string][]byte
	sets    int
	getErr  error
	setErr  error
	deleted []string
}

func newMapCache() *mapCache { return &mapCache{data: map[string][]byte{}} }

func (c *mapCache) Get(_ context.Context, key string) ([]byte, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.getErr != nil {
		return nil, c.getErr
	}
	v, ok := c.data[key]
	if !ok {
		return nil, ErrCacheMiss
	}
	return v, nil
}

func (c *mapCache) Set(_ context.Context, key string, value []byte, _ time.Duration) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.sets++
	if c.setErr != nil {
		return c.setErr
	}
	c.data[key] = value
	return nil
}

func (c *mapCache) Invalidate(_ context.Context, pattern string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.deleted = append(c.deleted, pattern)
	prefix := pattern
	if len(prefix) > 0 && prefix[len(prefix)-1] == '*' {
		prefix = prefix[:len(prefix)-1]
	}
	for k := range c.data {
		if len(k) >= len(prefix) && k[:len(prefix)] == prefix {
			delete(c.data, k)
		}
	}
	return nil
}

// stubPopularity counts PlantPopularity calls and returns a fixed map.
type stubPopularity struct {
	calls int
	ret   map[string]float64
	err   error
}

func (s *stubPopularity) PlantPopularity(_ context.Context, _ string, _ time.Time) (map[string]float64, error) {
	s.calls++
	if s.err != nil {
		return nil, s.err
	}
	return s.ret, nil
}

// stubAffinity counts UserVendorAffinity calls and returns a fixed map.
type stubAffinity struct {
	calls int
	ret   map[string]float64
	err   error
}

func (s *stubAffinity) UserVendorAffinity(_ context.Context, _ string) (map[string]float64, error) {
	s.calls++
	if s.err != nil {
		return nil, s.err
	}
	return s.ret, nil
}

var testDay = time.Date(2026, 5, 26, 0, 0, 0, 0, time.UTC)

// ---------- CachedPopularity ----------

func TestCachedPopularityNilCacheBypasses(t *testing.T) {
	inner := &stubPopularity{ret: map[string]float64{"a": 1}}
	p := &CachedPopularity{Inner: inner}
	if _, err := p.PlantPopularity(context.Background(), "plant-a", testDay); err != nil {
		t.Fatalf("plant popularity: %v", err)
	}
	if inner.calls != 1 {
		t.Fatalf("inner.calls = %d, want 1", inner.calls)
	}
}

func TestCachedPopularityHitSkipsRecompute(t *testing.T) {
	inner := &stubPopularity{ret: map[string]float64{"a": 3, "b": 1}}
	p := &CachedPopularity{Inner: inner, Cache: newMapCache(), Metrics: NewMetrics()}
	ctx := context.Background()

	first, err := p.PlantPopularity(ctx, "plant-a", testDay)
	if err != nil {
		t.Fatalf("miss: %v", err)
	}
	second, err := p.PlantPopularity(ctx, "plant-a", testDay)
	if err != nil {
		t.Fatalf("hit: %v", err)
	}
	if inner.calls != 1 {
		t.Fatalf("inner.calls = %d, want 1 (second served from cache)", inner.calls)
	}
	if first["a"] != 3 || second["a"] != 3 || second["b"] != 1 {
		t.Fatalf("decoded popularity mismatch: first=%v second=%v", first, second)
	}
}

func TestCachedPopularityKeyedByPlantDate(t *testing.T) {
	inner := &stubPopularity{ret: map[string]float64{"a": 1}}
	cache := newMapCache()
	p := &CachedPopularity{Inner: inner, Cache: cache, Metrics: NewMetrics()}
	ctx := context.Background()

	_, _ = p.PlantPopularity(ctx, "plant-a", testDay)
	_, _ = p.PlantPopularity(ctx, "plant-b", testDay)              // different plant -> miss
	_, _ = p.PlantPopularity(ctx, "plant-a", testDay.AddDate(0, 0, 1)) // different day -> miss
	if inner.calls != 3 {
		t.Fatalf("inner.calls = %d, want 3 (distinct plant/date keys all miss)", inner.calls)
	}
}

func TestCachedPopularityInnerErrorPropagates(t *testing.T) {
	want := errors.New("db down")
	cache := newMapCache()
	p := &CachedPopularity{Inner: &stubPopularity{err: want}, Cache: cache, Metrics: NewMetrics()}
	if _, err := p.PlantPopularity(context.Background(), "plant-a", testDay); !errors.Is(err, want) {
		t.Fatalf("err = %v, want %v", err, want)
	}
	if cache.sets != 0 {
		t.Fatalf("cache.sets = %d, want 0 (no Set on inner error)", cache.sets)
	}
}

func TestCachedPopularitySetErrorSwallowed(t *testing.T) {
	cache := newMapCache()
	cache.setErr = errors.New("set failed")
	inner := &stubPopularity{ret: map[string]float64{"a": 1}}
	p := &CachedPopularity{Inner: inner, Cache: cache, Metrics: NewMetrics()}
	got, err := p.PlantPopularity(context.Background(), "plant-a", testDay)
	if err != nil {
		t.Fatalf("plant popularity: %v", err)
	}
	if got["a"] != 1 {
		t.Fatalf("got = %v, want {a:1}", got)
	}
}

func TestPopularityKeyPattern(t *testing.T) {
	if got := PopularityKeyPattern("plant-a", "2026-05-26"); got != "pop:plant-a:2026-05-26" {
		t.Errorf("PopularityKeyPattern = %q", got)
	}
	if got := PopularityKeyPattern("plant-a", ""); got != "pop:plant-a:*" {
		t.Errorf("PopularityKeyPattern empty day = %q", got)
	}
}

// ---------- CachedAffinity ----------

func TestCachedAffinityNilCacheBypasses(t *testing.T) {
	inner := &stubAffinity{ret: map[string]float64{"v1": 2}}
	a := &CachedAffinity{Inner: inner}
	if _, err := a.UserVendorAffinity(context.Background(), "u1"); err != nil {
		t.Fatalf("affinity: %v", err)
	}
	if inner.calls != 1 {
		t.Fatalf("inner.calls = %d, want 1", inner.calls)
	}
}

func TestCachedAffinityHitSkipsRecompute(t *testing.T) {
	inner := &stubAffinity{ret: map[string]float64{"v1": 2, "v2": 1}}
	a := &CachedAffinity{Inner: inner, Cache: newMapCache(), Metrics: NewMetrics()}
	ctx := context.Background()

	if _, err := a.UserVendorAffinity(ctx, "u1"); err != nil {
		t.Fatalf("miss: %v", err)
	}
	got, err := a.UserVendorAffinity(ctx, "u1")
	if err != nil {
		t.Fatalf("hit: %v", err)
	}
	if inner.calls != 1 {
		t.Fatalf("inner.calls = %d, want 1 (second served from cache)", inner.calls)
	}
	if got["v1"] != 2 || got["v2"] != 1 {
		t.Fatalf("decoded affinity mismatch: %v", got)
	}
}

func TestCachedAffinityKeyedByUser(t *testing.T) {
	inner := &stubAffinity{ret: map[string]float64{"v1": 1}}
	a := &CachedAffinity{Inner: inner, Cache: newMapCache(), Metrics: NewMetrics()}
	ctx := context.Background()
	_, _ = a.UserVendorAffinity(ctx, "u1")
	_, _ = a.UserVendorAffinity(ctx, "u2") // different user -> miss
	if inner.calls != 2 {
		t.Fatalf("inner.calls = %d, want 2 (distinct user keys both miss)", inner.calls)
	}
}

func TestCachedAffinityInnerErrorPropagates(t *testing.T) {
	want := errors.New("db down")
	cache := newMapCache()
	a := &CachedAffinity{Inner: &stubAffinity{err: want}, Cache: cache, Metrics: NewMetrics()}
	if _, err := a.UserVendorAffinity(context.Background(), "u1"); !errors.Is(err, want) {
		t.Fatalf("err = %v, want %v", err, want)
	}
	if cache.sets != 0 {
		t.Fatalf("cache.sets = %d, want 0 (no Set on inner error)", cache.sets)
	}
}

func TestAffinityKeyPattern(t *testing.T) {
	if got := AffinityKeyPattern("u1"); got != "affinity:u1" {
		t.Errorf("AffinityKeyPattern = %q", got)
	}
}
