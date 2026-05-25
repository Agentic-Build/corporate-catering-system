// Package readmodel hosts the cache-backed projections for the hot
// employee read paths: home, menu availability, and recommendation.
// The package is deliberately small: a Cache interface, a Valkey/Redis
// implementation, and a wrapper that memoises HomeService.Compute
// results by (user, plant, day).
//
// The consistency model is bounded eventual consistency: write paths
// (order placement, quota mutation, menu draft publish) emit outbox
// events; the Invalidator (see invalidator.go) consumes those events
// from JetStream and invalidates the affected keys. Failures to
// invalidate fall back to TTL expiry, so a missed event becomes
// staleness, not an inconsistency that requires manual recovery.
package readmodel

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

// Cache is the small surface the read-model wrappers consume. A real
// implementation must be safe for concurrent use and return
// ErrCacheMiss when a key is not present.
type Cache interface {
	Get(ctx context.Context, key string) ([]byte, error)
	Set(ctx context.Context, key string, value []byte, ttl time.Duration) error
	Invalidate(ctx context.Context, pattern string) error
}

// ErrCacheMiss is returned by Cache.Get when the key is not present.
var ErrCacheMiss = errors.New("readmodel cache miss")

// RedisCache implements Cache against the Valkey HA / Redis client
// already wired by the api role. Keys are namespaced under a
// configurable prefix so eviction patterns can target a single
// read-model surface (e.g. tbite:rm:home:*).
type RedisCache struct {
	C      *redis.Client
	Prefix string
}

func (r *RedisCache) Get(ctx context.Context, key string) ([]byte, error) {
	v, err := r.C.Get(ctx, r.Prefix+key).Bytes()
	if errors.Is(err, redis.Nil) {
		return nil, ErrCacheMiss
	}
	return v, err
}

func (r *RedisCache) Set(ctx context.Context, key string, value []byte, ttl time.Duration) error {
	return r.C.Set(ctx, r.Prefix+key, value, ttl).Err()
}

// Invalidate deletes keys matching the supplied pattern. The pattern
// is expanded by SCAN; for the small per-plant-per-day key cardinality
// we expect, this is comfortably faster than waiting for TTL expiry.
// The pattern is appended to the configured Prefix.
func (r *RedisCache) Invalidate(ctx context.Context, pattern string) error {
	full := r.Prefix + pattern
	var cursor uint64
	for {
		keys, next, err := r.C.Scan(ctx, cursor, full, 256).Result()
		if err != nil {
			return fmt.Errorf("scan %s: %w", full, err)
		}
		if len(keys) > 0 {
			if err := r.C.Del(ctx, keys...).Err(); err != nil {
				return fmt.Errorf("del: %w", err)
			}
		}
		if next == 0 {
			return nil
		}
		cursor = next
	}
}

// Metrics holds the read-model OTel counters / gauges. NewMetrics is
// safe to call from multiple roles; the OTel meter provider returns
// shared instruments.
type Metrics struct {
	Hits         metric.Int64Counter
	Misses       metric.Int64Counter
	Errors       metric.Int64Counter
	RecomputeLag metric.Float64Gauge
	TTL          metric.Float64Histogram
}

// NewMetrics returns the shared instrument set for the read-model
// surfaces. The implementation guards against nil providers so tests
// can run without an OTel SDK.
func NewMetrics() Metrics {
	meter := otel.GetMeterProvider().Meter("tbite.readmodel")
	hits, _ := meter.Int64Counter("tbite_readmodel_cache_hits_total",
		metric.WithDescription("Read-model cache hits, by surface."))
	miss, _ := meter.Int64Counter("tbite_readmodel_cache_misses_total",
		metric.WithDescription("Read-model cache misses, by surface."))
	errs, _ := meter.Int64Counter("tbite_readmodel_cache_errors_total",
		metric.WithDescription("Read-model cache backing-store errors, by surface."))
	// recompute lag is a synchronous gauge: the dashboard plots the bare
	// series over time, so the last recompute duration per model is what
	// we want, not a histogram of all of them.
	lag, _ := meter.Float64Gauge("tbite_readmodel_recompute_lag_seconds",
		metric.WithDescription("Duration of the most recent cache-miss recompute, by model."),
		metric.WithUnit("s"))
	// ttl histogram intentionally has NO unit: the meter has a View that
	// caps "s"-unit histograms at 10s buckets, but TTLs are ~30s and would
	// all overflow. Empty unit uses the default boundaries so data shows.
	ttl, _ := meter.Float64Histogram("tbite_readmodel_ttl_seconds",
		metric.WithDescription("Read-model cache entry TTL at Set, by model."))
	return Metrics{Hits: hits, Misses: miss, Errors: errs, RecomputeLag: lag, TTL: ttl}
}

// recordHit / recordMiss / recordError safely emit the labelled
// counter regardless of whether the OTel SDK has been initialised.
func (m Metrics) recordHit(ctx context.Context, model string) {
	if m.Hits == nil {
		return
	}
	m.Hits.Add(ctx, 1, metric.WithAttributes(attribute.String("model", model)))
}
func (m Metrics) recordMiss(ctx context.Context, model string) {
	if m.Misses == nil {
		return
	}
	m.Misses.Add(ctx, 1, metric.WithAttributes(attribute.String("model", model)))
}
func (m Metrics) recordError(ctx context.Context, model string) {
	if m.Errors == nil {
		return
	}
	m.Errors.Add(ctx, 1, metric.WithAttributes(attribute.String("model", model)))
}

// recordRecomputeLag records the duration of a cache-miss recompute as
// an instantaneous gauge value, labelled by model.
func (m Metrics) recordRecomputeLag(ctx context.Context, model string, seconds float64) {
	if m.RecomputeLag == nil {
		return
	}
	m.RecomputeLag.Record(ctx, seconds, metric.WithAttributes(attribute.String("model", model)))
}

// recordTTL records the TTL (in seconds) of a cache entry at Set time,
// labelled by model.
func (m Metrics) recordTTL(ctx context.Context, model string, seconds float64) {
	if m.TTL == nil {
		return
	}
	m.TTL.Record(ctx, seconds, metric.WithAttributes(attribute.String("model", model)))
}

// JSONCodec marshals values as JSON. Read models use JSON because
// they are read by handlers that need typed access; the marshalling
// cost is bounded by the small per-key payloads (a HomeState is
// roughly 200 bytes).
type JSONCodec[T any] struct{}

func (JSONCodec[T]) Encode(v T) ([]byte, error) { return json.Marshal(v) }
func (JSONCodec[T]) Decode(b []byte) (T, error) {
	var v T
	err := json.Unmarshal(b, &v)
	return v, err
}
