package relay

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strconv"
	"time"

	"github.com/Agentic-Build/corporate-catering-system/services/api/internal/order"
	"github.com/Agentic-Build/corporate-catering-system/services/api/internal/platform/messaging"
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
		// The outbox row id is a stable per-event dedup key: if a crash between
		// publish and MarkPublished causes a re-publish next cycle, JetStream
		// collapses it via Nats-Msg-Id.
		dedupID := "outbox-" + strconv.FormatInt(ev.ID, 10)
		if err := r.NATS.PublishTraced(ctx, ev.Subject, payload, dedupID); err != nil {
			r.Logger.Warn("publish failed", "event_id", ev.ID, "subject", ev.Subject, "err", err)
			// Stage the failure (attempts++, last_error) on the cycle tx without
			// committing; the failed event stays unpublished and gets re-locked
			// next cycle. The whole cycle commits once via MarkPublished below.
			if err2 := r.Outbox.MarkFailed(ctx, tx, ev.ID, err.Error()); err2 != nil {
				r.Logger.Error("mark failed errored", "err", err2)
			}
			continue
		}
		successIDs = append(successIDs, ev.ID)
		recordPublished(ctx, ev.AggregateType)
	}
	// Single commit point: persists the published marks above plus any staged
	// MarkFailed updates atomically. Commits even when successIDs is empty.
	if err := r.Outbox.MarkPublished(ctx, tx, successIDs); err != nil {
		return len(events), err
	}
	if len(successIDs) > 0 {
		r.Logger.Info("relay published", "count", len(successIDs))
	}
	return len(events), nil
}
