package relay

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"time"

	"github.com/takalawang/corporate-catering-system/services/api/internal/order"
	"github.com/takalawang/corporate-catering-system/services/api/internal/platform/messaging"
)

type Relay struct {
	Outbox order.OutboxRepository
	NATS   *messaging.Client
	Logger *slog.Logger
	Batch  int
	Sleep  time.Duration
}

func (r *Relay) Run(ctx context.Context) error {
	if r.Batch <= 0 {
		r.Batch = 100
	}
	if r.Sleep <= 0 {
		r.Sleep = 500 * time.Millisecond
	}
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		n, err := r.cycle(ctx)
		if err != nil && !errors.Is(err, context.Canceled) {
			r.Logger.Error("relay cycle", "err", err)
		}
		if n == 0 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(r.Sleep):
			}
		}
	}
}

func (r *Relay) cycle(ctx context.Context) (int, error) {
	events, tx, err := r.Outbox.LockBatch(ctx, r.Batch)
	if err != nil {
		return 0, err
	}
	if len(events) == 0 {
		return 0, nil
	}
	successIDs := make([]int64, 0, len(events))
	for _, ev := range events {
		payload, _ := json.Marshal(ev.Payload)
		if err := r.NATS.PublishTraced(ctx, ev.Subject, payload); err != nil {
			r.Logger.Warn("publish failed", "event_id", ev.ID, "subject", ev.Subject, "err", err)
			// Mark this one failed but don't kill the batch; the tx still
			// commits via MarkPublished below — but the failed event remains
			// unpublished (published_at = NULL, attempts incremented).
			if err2 := r.Outbox.MarkFailed(ctx, tx, ev.ID, err.Error()); err2 != nil {
				r.Logger.Error("mark failed errored", "err", err2)
			}
			continue
		}
		successIDs = append(successIDs, ev.ID)
		recordPublished(ctx, ev.AggregateType)
	}
	// MarkPublished commits the transaction even if successIDs is empty
	if err := r.Outbox.MarkPublished(ctx, tx, successIDs); err != nil {
		return len(events), err
	}
	if len(successIDs) > 0 {
		r.Logger.Info("relay published", "count", len(successIDs))
	}
	return len(events), nil
}
