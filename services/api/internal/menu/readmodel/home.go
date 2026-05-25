package readmodel

// Cached wrapper around menu.HomeService.Compute. Compute is the
// employee landing page projection — today's order summary plus the
// target_day used to render the chip carousels. Recomputing it on
// every request fans hot reads into the order and menu tables; this
// wrapper memoises the result by (user, plant, day-override) under a
// short TTL and lets the Invalidator clear keys for the affected
// plant/date when an order event arrives.
//
// Strong-consistency surfaces (order placement, quota mutation) do
// NOT use this wrapper — they read from the transactional path
// directly.

import (
	"context"
	"fmt"
	"time"

	"github.com/takalawang/corporate-catering-system/services/api/internal/menu"
)

const (
	HomeSurface          = "employee-home"
	defaultHomeTTL       = 30 * time.Second
	defaultHomeKeyFormat = "home:%s:%s:%s" // user_id : plant : day_override
)

// HomeComputer is the small surface CachedHome consumes. The
// production wiring satisfies it with menu.HomeService.Compute; tests
// substitute a stub.
type HomeComputer interface {
	Compute(ctx context.Context, userID, plant, dayOverride string) (menu.HomeState, error)
}

// CachedHome wraps a HomeComputer with a read-model cache. The TTL
// bounds staleness for surfaces whose workflow tolerates it; the
// outbox-driven Invalidator pre-empts the TTL when an order event
// arrives for the affected plant/date.
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
			h.Metrics.recordHit(ctx, HomeSurface)
			return v, nil
		}
		// fall through to recompute on decode error
		h.Metrics.recordError(ctx, HomeSurface)
	} else if err != ErrCacheMiss {
		h.Metrics.recordError(ctx, HomeSurface)
	} else {
		h.Metrics.recordMiss(ctx, HomeSurface)
	}

	state, err := h.Inner.Compute(ctx, userID, plant, dayOverride)
	if err != nil {
		return state, err
	}
	encoded, encErr := codec.Encode(state)
	if encErr == nil {
		_ = h.Cache.Set(ctx, key, encoded, ttl)
	}
	return state, nil
}

// HomeKeyPattern returns a SCAN pattern that targets every cached
// home key for a given plant/date. The Invalidator uses this to
// clear affected entries on order / menu events; the `user_id`
// position is left as a wildcard because every employee in the plant
// shares the same source data.
func HomeKeyPattern(plant, dayOverride string) string {
	if dayOverride == "" {
		dayOverride = "*"
	}
	return fmt.Sprintf(defaultHomeKeyFormat, "*", plant, dayOverride)
}
