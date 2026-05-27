package readmodel

// Cached wrappers around the two raw-orders aggregates that back the
// recommendation chips: plant popularity (order_item qty sum per plant/date)
// and per-user vendor affinity (order count per vendor, last 30 days). Both
// were recomputed from the order tables on every /home and /recommendations
// request; these wrappers memoise them under a short TTL and let the
// outbox-driven Invalidator drop affected keys on order events.
//
// Popularity is plant/date keyed and shared by every employee in the plant,
// so it is the canonical read-model aggregate. Affinity is user keyed (its
// source is the user's own 30-day order history).

import (
	"context"
	"fmt"
	"time"
)

const (
	popularityModel      = "popularity"
	affinityModel        = "affinity"
	defaultPopularityTTL = 30 * time.Second
	defaultAffinityTTL   = 5 * time.Minute
	popularityKeyFormat  = "pop:%s:%s" // plant : day (YYYY-MM-DD)
	affinityKeyFormat    = "affinity:%s"
	dayLayout            = "2006-01-02"
)

// PlantPopularityComputer is the slice of menu/postgres.PopularityRepo the
// cached wrapper memoises.
type PlantPopularityComputer interface {
	PlantPopularity(ctx context.Context, plant string, day time.Time) (map[string]float64, error)
}

// AffinityComputer is the slice of menu/postgres.AffinityRepo the cached
// wrapper memoises.
type AffinityComputer interface {
	UserVendorAffinity(ctx context.Context, userID string) (map[string]float64, error)
}

// CachedPopularity wraps PlantPopularity with a read-model cache keyed by
// plant/date. The TTL bounds staleness; the Invalidator pre-empts it when an
// order event arrives for the affected plant/date.
type CachedPopularity struct {
	Inner   PlantPopularityComputer
	Cache   Cache
	Metrics Metrics
	TTL     time.Duration
}

func (p *CachedPopularity) PlantPopularity(ctx context.Context, plant string, day time.Time) (map[string]float64, error) {
	if p.Cache == nil {
		return p.Inner.PlantPopularity(ctx, plant, day)
	}
	ttl := p.TTL
	if ttl <= 0 {
		ttl = defaultPopularityTTL
	}
	key := fmt.Sprintf(popularityKeyFormat, plant, day.Format(dayLayout))
	return getOrRecompute(ctx, p.Cache, p.Metrics, popularityModel, key, ttl, func() (map[string]float64, error) {
		return p.Inner.PlantPopularity(ctx, plant, day)
	})
}

// PopularityKeyPattern returns a SCAN pattern targeting the cached popularity
// key for a plant/date; an empty day wildcards every day for the plant.
func PopularityKeyPattern(plant, day string) string {
	if day == "" {
		day = "*"
	}
	return fmt.Sprintf(popularityKeyFormat, plant, day)
}

// CachedAffinity wraps UserVendorAffinity with a read-model cache keyed by
// user. The TTL is longer than popularity's because the underlying signal is
// a 30-day rolling window — a single new order barely moves it; the
// Invalidator still drops the user's key on their order events.
type CachedAffinity struct {
	Inner   AffinityComputer
	Cache   Cache
	Metrics Metrics
	TTL     time.Duration
}

func (a *CachedAffinity) UserVendorAffinity(ctx context.Context, userID string) (map[string]float64, error) {
	if a.Cache == nil {
		return a.Inner.UserVendorAffinity(ctx, userID)
	}
	ttl := a.TTL
	if ttl <= 0 {
		ttl = defaultAffinityTTL
	}
	key := fmt.Sprintf(affinityKeyFormat, userID)
	return getOrRecompute(ctx, a.Cache, a.Metrics, affinityModel, key, ttl, func() (map[string]float64, error) {
		return a.Inner.UserVendorAffinity(ctx, userID)
	})
}

// AffinityKeyPattern returns the exact cached affinity key for a user.
func AffinityKeyPattern(userID string) string {
	return fmt.Sprintf(affinityKeyFormat, userID)
}

// getOrRecompute is the shared cache-aside flow for the two map-valued
// aggregates: serve a decoded hit, else recompute, record metrics, and
// best-effort Set. Mirrors CachedHome.Compute.
func getOrRecompute(
	ctx context.Context, cache Cache, m Metrics, model, key string, ttl time.Duration,
	recompute func() (map[string]float64, error),
) (map[string]float64, error) {
	codec := JSONCodec[map[string]float64]{}
	raw, err := cache.Get(ctx, key)
	if err == nil {
		v, derr := codec.Decode(raw)
		if derr == nil {
			m.recordHit(ctx, model)
			return v, nil
		}
		m.recordError(ctx, model)
	} else if err != ErrCacheMiss {
		m.recordError(ctx, model)
	} else {
		m.recordMiss(ctx, model)
	}

	start := time.Now()
	v, err := recompute()
	m.recordRecomputeLag(ctx, model, time.Since(start).Seconds())
	if err != nil {
		return nil, err
	}
	if encoded, encErr := codec.Encode(v); encErr == nil {
		if err := cache.Set(ctx, key, encoded, ttl); err == nil {
			m.recordTTL(ctx, model, ttl.Seconds())
		}
	}
	return v, nil
}
