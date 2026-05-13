package scheduler

import (
	"context"
	"log/slog"
	"time"

	"github.com/takalawang/corporate-catering-system/services/api/internal/order"
)

// NoShowSweep periodically transitions READY orders older than MaxAge to NO_SHOW.
// Each per-order transition runs in its own transaction (see Service.MarkNoShow),
// so a single bad row never blocks the batch.
type NoShowSweep struct {
	Svc      *order.Service
	Interval time.Duration
	MaxAge   time.Duration
	Logger   *slog.Logger
}

// RunOnce processes all READY orders whose ready_at is older than MaxAge.
// Returns the number of orders transitioned.
func (n *NoShowSweep) RunOnce(ctx context.Context) (int, error) {
	if n.MaxAge <= 0 {
		n.MaxAge = 2 * time.Hour
	}
	return n.Svc.MarkNoShow(ctx, n.MaxAge)
}

// Run loops, calling RunOnce every Interval. Exits cleanly on ctx cancellation.
func (n *NoShowSweep) Run(ctx context.Context) error {
	if n.Interval <= 0 {
		n.Interval = 5 * time.Minute
	}
	n.Logger.Info("no-show sweep started", "interval", n.Interval, "max_age", n.MaxAge)
	ticker := time.NewTicker(n.Interval)
	defer ticker.Stop()
	if c, err := n.RunOnce(ctx); err != nil {
		n.Logger.Error("no-show initial run", "err", err)
	} else if c > 0 {
		n.Logger.Info("no-show transitioned", "count", c)
	}
	for {
		select {
		case <-ctx.Done():
			n.Logger.Info("no-show sweep stopping")
			return ctx.Err()
		case <-ticker.C:
			if c, err := n.RunOnce(ctx); err != nil {
				n.Logger.Error("no-show tick", "err", err)
			} else if c > 0 {
				n.Logger.Info("no-show transitioned", "count", c)
			}
		}
	}
}
