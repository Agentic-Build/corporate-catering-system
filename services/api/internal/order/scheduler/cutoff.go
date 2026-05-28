package scheduler

import (
	"context"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Agentic-Build/corporate-catering-system/services/api/internal/order"
)

// OrdersTx is the order-repo subset Cutoff uses inside a transaction.
type OrdersTx interface {
	UpdateStatusTx(ctx context.Context, tx pgx.Tx, id string, from, to order.Status) error
}

// StateTx is the state-event-repo subset used inside a transaction.
type StateTx interface {
	AppendTx(ctx context.Context, tx pgx.Tx, ev *order.StateEvent) error
}

// AuditTx is the audit-repo subset used inside a transaction.
type AuditTx interface {
	WriteTx(ctx context.Context, tx pgx.Tx, actorID, actorRole *string, action, targetKind, targetID string, payload map[string]any, requestID string) error
}

// OutboxTx is the outbox-repo subset used inside a transaction.
type OutboxTx interface {
	AppendTx(ctx context.Context, tx pgx.Tx, aggregateType, aggregateID, subject string, payload map[string]any, headers map[string]any) error
}

// Clock allows tests to control "now".
type Clock interface{ Now() time.Time }

// Cutoff runs the daily cutoff transition (placed → cutoff) for orders past cutoff_at.
type Cutoff struct {
	Pool     *pgxpool.Pool
	Orders   order.Repository
	OrdersTx OrdersTx
	StateTx  StateTx
	AuditTx  AuditTx
	OutboxTx OutboxTx
	Clock    Clock
	Logger   *slog.Logger
}

// RunOnce processes all orders whose cutoff_at has passed. Each order is transitioned
// in its own per-order transaction so a single failure doesn't block the batch.
// Returns the number of orders transitioned.
func (c *Cutoff) RunOnce(ctx context.Context) (int, error) {
	now := c.Clock.Now()
	pending, err := c.Orders.ListPlacedDueForCutoff(ctx, now)
	if err != nil {
		return 0, err
	}
	if len(pending) == 0 {
		return 0, nil
	}
	transitioned := 0
	for _, o := range pending {
		if err := c.transitionOne(ctx, o); err != nil {
			c.Logger.Warn("cutoff transition failed", "order_id", o.ID, "err", err)
			continue
		}
		transitioned++
	}
	if transitioned > 0 {
		c.Logger.Info("cutoff transitioned", "count", transitioned, "of", len(pending))
	}
	return transitioned, nil
}

func (c *Cutoff) transitionOne(ctx context.Context, o *order.Order) error {
	return pgx.BeginFunc(ctx, c.Pool, func(tx pgx.Tx) error {
		if err := c.OrdersTx.UpdateStatusTx(ctx, tx, o.ID, order.StatusPlaced, order.StatusCutoff); err != nil {
			return err
		}
		from := order.StatusPlaced
		// actor_role is a user_role enum (employee|vendor_operator|welfare_admin)
		// and is nullable. System-initiated transitions carry no actor; the
		// "scheduler_cutoff" reason + "order.cutoff" audit action identify the source.
		if err := c.StateTx.AppendTx(ctx, tx, &order.StateEvent{
			OrderID:   o.ID,
			FromState: &from,
			ToState:   order.StatusCutoff,
			ActorRole: nil,
			Reason:    "scheduler_cutoff",
			Payload:   map[string]any{},
		}); err != nil {
			return err
		}
		payload := map[string]any{"order_id": o.ID, "vendor_id": o.VendorID, "plant": o.Plant}
		if err := c.OutboxTx.AppendTx(ctx, tx, "order", o.ID, "order.cutoff.v1", payload, map[string]any{}); err != nil {
			return err
		}
		if err := c.AuditTx.WriteTx(ctx, tx, nil, nil, "order.cutoff", "order", o.ID, payload, ""); err != nil {
			return err
		}
		return nil
	})
}

// Run loops, calling RunOnce every `interval`. Exits cleanly on ctx cancellation.
func (c *Cutoff) Run(ctx context.Context, interval time.Duration) error {
	if interval <= 0 {
		interval = 60 * time.Second
	}
	c.Logger.Info("cutoff scheduler started", "interval", interval)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	if _, err := c.RunOnce(ctx); err != nil {
		c.Logger.Error("cutoff initial run", "err", err)
	}
	for {
		select {
		case <-ctx.Done():
			c.Logger.Info("cutoff scheduler stopping")
			return ctx.Err()
		case <-ticker.C:
			if _, err := c.RunOnce(ctx); err != nil {
				c.Logger.Error("cutoff tick", "err", err)
			}
		}
	}
}
