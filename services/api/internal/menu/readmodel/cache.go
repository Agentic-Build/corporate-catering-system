// Package readmodel hosts cache-backed projections for the hot employee read
// paths (home, menu availability, recommendation).
//
// Consistency model: bounded eventual. Write paths emit outbox events; the
// Invalidator consumes them from JetStream and invalidates affected keys.
// Failed invalidation falls back to TTL expiry (staleness, not inconsistency).
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

// Cache is the small surface the read-model wrappers consume. Implementations
// must be concurrency-safe and return ErrCacheMiss when a key is absent.
type Cache interface {
	Get(ctx context.Context, key string) ([]byte, error)
	Set(ctx context.Context, key string, value []byte, ttl time.Duration) error
	Invalidate(ctx context.Context, pattern string) error
}

// ErrCacheMiss is returned by Cache.Get when the key is not present.
var ErrCacheMiss = errors.New("readmodel cache miss")

// RedisCache implements Cache against the shared Valkey/Redis client. Keys
// are namespaced under Prefix so eviction patterns can target one surface
// (e.g. tbite:rm:home:*).
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

// Invalidate deletes keys matching pattern (appended to Prefix), via SCAN.
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

// Metrics holds the read-model OTel counters/gauges. Safe to call from
// multiple roles — OTel returns shared instruments.
type Metrics struct {
	Hits         metric.Int64Counter
	Misses       metric.Int64Counter
	Errors       metric.Int64Counter
	RecomputeLag metric.Float64Gauge
	TTL          metric.Float64Histogram
}

// NewMetrics returns the shared instrument set. Guards against nil providers
// so tests can run without an OTel SDK.
func NewMetrics() Metrics {
	meter := otel.GetMeterProvider().Meter("tbite.readmodel")
	hits, _ := meter.Int64Counter("tbite_readmodel_cache_hits_total",
		metric.WithDescription("Read-model cache hits, by surface."))
	miss, _ := meter.Int64Counter("tbite_readmodel_cache_misses_total",
		metric.WithDescription("Read-model cache misses, by surface."))
	errs, _ := meter.Int64Counter("tbite_readmodel_cache_errors_total",
		metric.WithDescription("Read-model cache backing-store errors, by surface."))
	// Synchronous gauge: dashboard plots the bare series, not a histogram.
	lag, _ := meter.Float64Gauge("tbite_readmodel_recompute_lag_seconds",
		metric.WithDescription("Duration of the most recent cache-miss recompute, by model."),
		metric.WithUnit("s"))
	// No unit on purpose: the meter View caps "s" histograms at 10s buckets,
	// but TTLs are ~30s. Empty unit uses default boundaries so data shows.
	ttl, _ := meter.Float64Histogram("tbite_readmodel_ttl_seconds",
		metric.WithDescription("Read-model cache entry TTL at Set, by model."))
	return Metrics{Hits: hits, Misses: miss, Errors: errs, RecomputeLag: lag, TTL: ttl}
}

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

func (m Metrics) recordRecomputeLag(ctx context.Context, model string, seconds float64) {
	if m.RecomputeLag == nil {
		return
	}
	m.RecomputeLag.Record(ctx, seconds, metric.WithAttributes(attribute.String("model", model)))
}

func (m Metrics) recordTTL(ctx context.Context, model string, seconds float64) {
	if m.TTL == nil {
		return
	}
	m.TTL.Record(ctx, seconds, metric.WithAttributes(attribute.String("model", model)))
}

// JSONCodec marshals values as JSON; per-key payloads are small (~200 B).
type JSONCodec[T any] struct{}

func (JSONCodec[T]) Encode(v T) ([]byte, error) { return json.Marshal(v) }
func (JSONCodec[T]) Decode(b []byte) (T, error) {
	var v T
	err := json.Unmarshal(b, &v)
	return v, err
}
