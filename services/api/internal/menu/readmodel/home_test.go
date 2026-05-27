package readmodel

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/takalawang/corporate-catering-system/services/api/internal/menu"
)

// errCache is a Cache whose Get/Set behaviour is controlled per-field so
// individual CachedHome.Compute branches can be exercised.
type errCache struct {
	getErr  error
	getData []byte
	setErr  error
	sets    int
}

func (c *errCache) Get(_ context.Context, _ string) ([]byte, error) {
	return c.getData, c.getErr
}

func (c *errCache) Set(_ context.Context, _ string, _ []byte, _ time.Duration) error {
	c.sets++
	return c.setErr
}

func (c *errCache) Invalidate(_ context.Context, _ string) error { return nil }

// errComputer is a HomeComputer that returns a fixed error.
type errComputer struct{ err error }

func (e *errComputer) Compute(_ context.Context, _, _, _ string) (menu.HomeState, error) {
	return menu.HomeState{}, e.err
}

func TestCachedHomeNilCacheBypasses(t *testing.T) {
	inner := &stubComputer{}
	h := &CachedHome{Inner: inner}
	if _, err := h.Compute(context.Background(), "u1", "plant-a", ""); err != nil {
		t.Fatalf("compute: %v", err)
	}
	if inner.calls != 1 {
		t.Fatalf("inner.calls = %d, want 1", inner.calls)
	}
}

func TestCachedHomeDefaultTTLWhenUnset(t *testing.T) {
	cache := &errCache{getErr: ErrCacheMiss}
	inner := &stubComputer{}
	h := &CachedHome{Inner: inner, Cache: cache, Metrics: NewMetrics()} // TTL zero -> default
	if _, err := h.Compute(context.Background(), "u1", "plant-a", ""); err != nil {
		t.Fatalf("compute: %v", err)
	}
	if cache.sets != 1 {
		t.Fatalf("cache.sets = %d, want 1", cache.sets)
	}
}

func TestCachedHomeGetErrorRecomputes(t *testing.T) {
	cache := &errCache{getErr: errors.New("backing store down")}
	inner := &stubComputer{}
	h := &CachedHome{Inner: inner, Cache: cache, Metrics: NewMetrics(), TTL: time.Second}
	if _, err := h.Compute(context.Background(), "u1", "plant-a", ""); err != nil {
		t.Fatalf("compute: %v", err)
	}
	if inner.calls != 1 {
		t.Fatalf("inner.calls = %d, want 1 (recompute after non-miss error)", inner.calls)
	}
	if cache.sets != 1 {
		t.Fatalf("cache.sets = %d, want 1", cache.sets)
	}
}

func TestCachedHomeDecodeErrorRecomputes(t *testing.T) {
	cache := &errCache{getData: []byte("not-json")} // getErr nil -> decode path
	inner := &stubComputer{}
	h := &CachedHome{Inner: inner, Cache: cache, Metrics: NewMetrics(), TTL: time.Second}
	if _, err := h.Compute(context.Background(), "u1", "plant-a", ""); err != nil {
		t.Fatalf("compute: %v", err)
	}
	if inner.calls != 1 {
		t.Fatalf("inner.calls = %d, want 1 (recompute after decode error)", inner.calls)
	}
}

func TestCachedHomeInnerErrorPropagates(t *testing.T) {
	want := errors.New("compute failed")
	cache := &errCache{getErr: ErrCacheMiss}
	h := &CachedHome{Inner: &errComputer{err: want}, Cache: cache, Metrics: NewMetrics(), TTL: time.Second}
	if _, err := h.Compute(context.Background(), "u1", "plant-a", ""); !errors.Is(err, want) {
		t.Fatalf("err = %v, want %v", err, want)
	}
	if cache.sets != 0 {
		t.Fatalf("cache.sets = %d, want 0 (no Set on inner error)", cache.sets)
	}
}

func TestCachedHomeSetErrorSwallowed(t *testing.T) {
	cache := &errCache{getErr: ErrCacheMiss, setErr: errors.New("set failed")}
	inner := &stubComputer{}
	h := &CachedHome{Inner: inner, Cache: cache, Metrics: NewMetrics(), TTL: time.Second}
	// Set error must not fail the call; the state is still returned.
	st, err := h.Compute(context.Background(), "u1", "plant-a", "")
	if err != nil {
		t.Fatalf("compute: %v", err)
	}
	if st.TargetDay != "2026-05-26" {
		t.Fatalf("TargetDay = %q, want stub value", st.TargetDay)
	}
}

func TestHomeKeyPattern(t *testing.T) {
	if got := HomeKeyPattern("plant-a", "2026-05-26"); got != "home:*:plant-a:2026-05-26" {
		t.Errorf("HomeKeyPattern with day = %q", got)
	}
	if got := HomeKeyPattern("plant-a", ""); got != "home:*:plant-a:*" {
		t.Errorf("HomeKeyPattern empty day = %q", got)
	}
}

func TestMetricsNilRecordersAreNoops(t *testing.T) {
	var m Metrics // all instruments nil
	ctx := context.Background()
	m.recordHit(ctx, "home")
	m.recordMiss(ctx, "home")
	m.recordError(ctx, "home")
	m.recordRecomputeLag(ctx, "home", 1.0)
	m.recordTTL(ctx, "home", 30)
}
