package readmodel

import (
	"context"
	"sync"
	"testing"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/Agentic-Build/corporate-catering-system/services/api/internal/menu"
)

// memCache is an in-memory Cache stub safe for the single-goroutine test.
type memCache struct {
	mu   sync.Mutex
	data map[string][]byte
}

func newMemCache() *memCache { return &memCache{data: map[string][]byte{}} }

func (c *memCache) Get(_ context.Context, key string) ([]byte, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	v, ok := c.data[key]
	if !ok {
		return nil, ErrCacheMiss
	}
	return v, nil
}

func (c *memCache) Set(_ context.Context, key string, value []byte, _ time.Duration) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.data[key] = value
	return nil
}

func (c *memCache) Invalidate(_ context.Context, _ string) error { return nil }

// stubComputer is a HomeComputer that returns a fixed state and counts calls.
type stubComputer struct {
	calls int
}

func (s *stubComputer) Compute(_ context.Context, _, _, _ string) (menu.HomeState, error) {
	s.calls++
	return menu.HomeState{TargetDay: "2026-05-26", HasOrdered: false}, nil
}

// collectScope returns the scope metrics for the readmodel meter.
func collectScope(t *testing.T, r *sdkmetric.ManualReader) []metricdata.Metrics {
	t.Helper()
	var rm metricdata.ResourceMetrics
	if err := r.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("collect: %v", err)
	}
	for _, sm := range rm.ScopeMetrics {
		if sm.Scope.Name == "tbite.readmodel" {
			return sm.Metrics
		}
	}
	t.Fatalf("scope tbite.readmodel not found in %+v", rm.ScopeMetrics)
	return nil
}

func findMetric(metrics []metricdata.Metrics, name string) (metricdata.Metrics, bool) {
	for _, m := range metrics {
		if m.Name == name {
			return m, true
		}
	}
	return metricdata.Metrics{}, false
}

func TestCachedHomeCacheMissRecordsMetrics(t *testing.T) {
	reader := sdkmetric.NewManualReader()
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	otel.SetMeterProvider(mp)

	m := NewMetrics()
	cache := newMemCache()
	inner := &stubComputer{}

	h := &CachedHome{
		Inner:   inner,
		Cache:   cache,
		Metrics: m,
		TTL:     30 * time.Second,
	}

	ctx := context.Background()

	// First call: cache miss -> recompute -> Set.
	if _, err := h.Compute(ctx, "u1", "plant-a", ""); err != nil {
		t.Fatalf("compute (miss): %v", err)
	}
	// Second call: cache hit.
	if _, err := h.Compute(ctx, "u1", "plant-a", ""); err != nil {
		t.Fatalf("compute (hit): %v", err)
	}
	if inner.calls != 1 {
		t.Fatalf("expected inner.Compute called once (miss only), got %d", inner.calls)
	}

	metrics := collectScope(t, reader)

	// recompute lag: synchronous gauge, one data point, attribute model=home.
	lag, ok := findMetric(metrics, "tbite_readmodel_recompute_lag_seconds")
	if !ok {
		t.Fatalf("recompute lag metric not emitted; got %v", metricNames(metrics))
	}
	if lag.Unit != "s" {
		t.Errorf("recompute lag unit = %q, want %q", lag.Unit, "s")
	}
	gauge, ok := lag.Data.(metricdata.Gauge[float64])
	if !ok {
		t.Fatalf("recompute lag is %T, want Gauge[float64]", lag.Data)
	}
	if len(gauge.DataPoints) != 1 {
		t.Fatalf("recompute lag data points = %d, want 1", len(gauge.DataPoints))
	}
	if got := attrValue(t, gauge.DataPoints[0].Attributes, "model"); got != "home" {
		t.Errorf("recompute lag model = %q, want %q", got, "home")
	}
	if gauge.DataPoints[0].Value < 0 {
		t.Errorf("recompute lag value = %v, want >= 0", gauge.DataPoints[0].Value)
	}

	// ttl histogram: one observation, NO unit, attribute model=home, recorded ttl ~30s.
	ttl, ok := findMetric(metrics, "tbite_readmodel_ttl_seconds")
	if !ok {
		t.Fatalf("ttl metric not emitted; got %v", metricNames(metrics))
	}
	if ttl.Unit != "" {
		t.Errorf("ttl unit = %q, want empty (so the 10s-cap View does not apply)", ttl.Unit)
	}
	hist, ok := ttl.Data.(metricdata.Histogram[float64])
	if !ok {
		t.Fatalf("ttl is %T, want Histogram[float64]", ttl.Data)
	}
	if len(hist.DataPoints) != 1 {
		t.Fatalf("ttl data points = %d, want 1", len(hist.DataPoints))
	}
	dp := hist.DataPoints[0]
	if dp.Count != 1 {
		t.Errorf("ttl observation count = %d, want 1", dp.Count)
	}
	if dp.Sum != 30 {
		t.Errorf("ttl recorded sum = %v, want 30 (ttl.Seconds())", dp.Sum)
	}
	if got := attrValue(t, dp.Attributes, "model"); got != "home" {
		t.Errorf("ttl model = %q, want %q", got, "home")
	}

	// miss counter carries attribute key "model" with value "home".
	miss, ok := findMetric(metrics, "tbite_readmodel_cache_misses_total")
	if !ok {
		t.Fatalf("miss counter not emitted; got %v", metricNames(metrics))
	}
	assertCounterModel(t, miss, "home", 1)

	// hit counter carries attribute key "model" with value "home".
	hit, ok := findMetric(metrics, "tbite_readmodel_cache_hits_total")
	if !ok {
		t.Fatalf("hit counter not emitted; got %v", metricNames(metrics))
	}
	assertCounterModel(t, hit, "home", 1)
}

func assertCounterModel(t *testing.T, m metricdata.Metrics, wantModel string, wantValue int64) {
	t.Helper()
	sum, ok := m.Data.(metricdata.Sum[int64])
	if !ok {
		t.Fatalf("%s is %T, want Sum[int64]", m.Name, m.Data)
	}
	if len(sum.DataPoints) != 1 {
		t.Fatalf("%s data points = %d, want 1", m.Name, len(sum.DataPoints))
	}
	dp := sum.DataPoints[0]
	if dp.Value != wantValue {
		t.Errorf("%s value = %d, want %d", m.Name, dp.Value, wantValue)
	}
	if got := attrValue(t, dp.Attributes, "model"); got != wantModel {
		t.Errorf("%s model = %q, want %q (attribute key must be \"model\", not \"surface\")", m.Name, got, wantModel)
	}
}

func attrValue(t *testing.T, set attribute.Set, key string) string {
	t.Helper()
	v, ok := set.Value(attribute.Key(key))
	if !ok {
		t.Fatalf("attribute %q not present in %v", key, set.Encoded(attribute.DefaultEncoder()))
	}
	return v.AsString()
}

func metricNames(metrics []metricdata.Metrics) []string {
	names := make([]string, len(metrics))
	for i, m := range metrics {
		names[i] = m.Name
	}
	return names
}
