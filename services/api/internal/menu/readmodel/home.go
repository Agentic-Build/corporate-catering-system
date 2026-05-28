package readmodel

// Cached wrapper around menu.HomeService.Compute (employee landing-page
// projection). Memoises (user, plant, day-override) under a short TTL;
// the Invalidator pre-empts on order events for the affected plant/date.
// Strong-consistency surfaces (order placement, quota) bypass this wrapper.

import (
	"context"
	"fmt"
	"time"

	"github.com/Agentic-Build/corporate-catering-system/services/api/internal/menu"
)

const (
	HomeSurface          = "employee-home"
	homeModel            = "home" // metric "model" label value; matches Grafana legends
	defaultHomeTTL       = 30 * time.Second
	defaultHomeKeyFormat = "home:%s:%s:%s" // user_id : plant : day_override
)

// HomeComputer is the small surface CachedHome consumes (menu.HomeService.Compute in prod).
type HomeComputer interface {
	Compute(ctx context.Context, userID, plant, dayOverride string) (menu.HomeState, error)
}

// CachedHome wraps a HomeComputer with a read-model cache; the Invalidator
// pre-empts the TTL on order events for the affected plant/date.
type CachedHome struct {
	Inner   HomeComputer
	Cache   Cache
	Metrics Metrics
	TTL     time.Duration
}

func (h *CachedHome) Compute(ctx context.Context, userID, plant, dayOverride string) (menu.HomeState, error) {
	if h.Cache == nil {
		return h.Inner.Compute(ctx, userID, plant, dayOverride)
	}
	ttl := h.TTL
	if ttl <= 0 {
		ttl = defaultHomeTTL
	}
	codec := JSONCodec[menu.HomeState]{}
	key := fmt.Sprintf(defaultHomeKeyFormat, userID, plant, dayOverride)

	raw, err := h.Cache.Get(ctx, key)
	if err == nil {
		v, derr := codec.Decode(raw)
		if derr == nil {
			h.Metrics.recordHit(ctx, homeModel)
			return v, nil
		}
		// fall through to recompute on decode error
		h.Metrics.recordError(ctx, homeModel)
	} else if err != ErrCacheMiss {
		h.Metrics.recordError(ctx, homeModel)
	} else {
		h.Metrics.recordMiss(ctx, homeModel)
	}

	start := time.Now()
	state, err := h.Inner.Compute(ctx, userID, plant, dayOverride)
	h.Metrics.recordRecomputeLag(ctx, homeModel, time.Since(start).Seconds())
	if err != nil {
		return state, err
	}
	encoded, encErr := codec.Encode(state)
	if encErr == nil {
		if err := h.Cache.Set(ctx, key, encoded, ttl); err == nil {
			h.Metrics.recordTTL(ctx, homeModel, ttl.Seconds())
		}
	}
	return state, nil
}

// HomeKeyPattern returns a SCAN pattern for every cached home key on a
// plant/date (user_id is wildcarded — every employee shares source data).
func HomeKeyPattern(plant, dayOverride string) string {
	if dayOverride == "" {
		dayOverride = "*"
	}
	return fmt.Sprintf(defaultHomeKeyFormat, "*", plant, dayOverride)
}
