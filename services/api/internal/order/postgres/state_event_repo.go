package postgres

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/takalawang/corporate-catering-system/services/api/internal/order"
)

type StateEventRepo struct{ pool *pgxpool.Pool }

func NewStateEventRepo(p *pgxpool.Pool) *StateEventRepo { return &StateEventRepo{pool: p} }

func (r *StateEventRepo) AppendTx(ctx context.Context, tx pgx.Tx, ev *order.StateEvent) error {
	raw, err := json.Marshal(ev.Payload)
	if err != nil {
		return fmt.Errorf("marshal payload: %w", err)
	}
	var fromState any
	if ev.FromState != nil {
		fromState = string(*ev.FromState)
	}
	err = tx.QueryRow(ctx, `
INSERT INTO order_state_event (order_id, from_state, to_state, actor_id, actor_role, reason, payload)
VALUES ($1,$2,$3,$4,$5,$6,$7)
RETURNING id, at`,
		ev.OrderID, fromState, string(ev.ToState), ev.ActorID, ev.ActorRole, ev.Reason, raw,
	).Scan(&ev.ID, &ev.At)
	if err != nil {
		return fmt.Errorf("append state event: %w", err)
	}
	return nil
}

func (r *StateEventRepo) Append(ctx context.Context, ev *order.StateEvent) error {
	return pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error { return r.AppendTx(ctx, tx, ev) })
}

func (r *StateEventRepo) ListByOrder(ctx context.Context, orderID string) ([]*order.StateEvent, error) {
	rows, err := r.pool.Query(ctx, `
SELECT id, order_id, from_state, to_state, actor_id, actor_role, reason, payload, at
  FROM order_state_event WHERE order_id=$1 ORDER BY at`, orderID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*order.StateEvent
	for rows.Next() {
		var ev order.StateEvent
		var fromState *string
		var toState string
		var payload []byte
		if err := rows.Scan(&ev.ID, &ev.OrderID, &fromState, &toState, &ev.ActorID, &ev.ActorRole, &ev.Reason, &payload, &ev.At); err != nil {
			return nil, err
		}
		ev.ToState = order.Status(toState)
		if fromState != nil {
			s := order.Status(*fromState)
			ev.FromState = &s
		}
		if len(payload) > 0 {
			_ = json.Unmarshal(payload, &ev.Payload)
		}
		out = append(out, &ev)
	}
	return out, rows.Err()
}
