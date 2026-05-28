package postgres

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Agentic-Build/corporate-catering-system/services/api/internal/order"
)

type OutboxRepo struct{ pool *pgxpool.Pool }

func NewOutboxRepo(p *pgxpool.Pool) *OutboxRepo { return &OutboxRepo{pool: p} }

// AppendTx inserts an outbox row using the provided pgx tx (preferred).
func (r *OutboxRepo) AppendTx(ctx context.Context, tx pgx.Tx, aggregateType, aggregateID, subject string, payload map[string]any, headers map[string]any) error {
	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal payload: %w", err)
	}
	headersBytes, err := json.Marshal(headers)
	if err != nil {
		return fmt.Errorf("marshal headers: %w", err)
	}
	_, err = tx.Exec(ctx, `
INSERT INTO outbox_event (aggregate_type, aggregate_id, subject, payload, headers)
VALUES ($1,$2,$3,$4,$5)`,
		aggregateType, aggregateID, subject, payloadBytes, headersBytes)
	return err
}

// Append implements order.OutboxRepository. The opaque Tx must be a pgx.Tx.
func (r *OutboxRepo) Append(ctx context.Context, tx order.Tx, aggregateType, aggregateID, subject string, payload map[string]any, headers map[string]any) error {
	ptx, ok := tx.(pgx.Tx)
	if !ok {
		return fmt.Errorf("outbox: tx must be pgx.Tx")
	}
	return r.AppendTx(ctx, ptx, aggregateType, aggregateID, subject, payload, headers)
}

// LockBatch starts a new transaction and selects up to `limit` unpublished events
// using FOR UPDATE SKIP LOCKED so multiple relay workers do not double-lock.
// The caller MUST eventually call MarkPublished (optionally after staging
// per-event failures via MarkFailed) to commit and release the row locks.
// If no rows are available, returns (nil, nil, nil) with the tx already rolled back.
func (r *OutboxRepo) LockBatch(ctx context.Context, limit int) ([]*order.OutboxEvent, order.Tx, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, nil, err
	}
	rows, err := tx.Query(ctx, `
SELECT id, aggregate_type, aggregate_id, subject, payload, headers, created_at, published_at, attempts, last_error
  FROM outbox_event
 WHERE published_at IS NULL
 ORDER BY id
 LIMIT $1
 FOR UPDATE SKIP LOCKED`, limit)
	if err != nil {
		_ = tx.Rollback(ctx)
		return nil, nil, err
	}

	var out []*order.OutboxEvent
	for rows.Next() {
		var ev order.OutboxEvent
		var payload, headers []byte
		if err := rows.Scan(&ev.ID, &ev.AggregateType, &ev.AggregateID, &ev.Subject,
			&payload, &headers, &ev.CreatedAt, &ev.PublishedAt, &ev.Attempts, &ev.LastError); err != nil {
			rows.Close()
			_ = tx.Rollback(ctx)
			return nil, nil, err
		}
		_ = json.Unmarshal(payload, &ev.Payload)
		_ = json.Unmarshal(headers, &ev.Headers)
		out = append(out, &ev)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		_ = tx.Rollback(ctx)
		return nil, nil, err
	}
	if len(out) == 0 {
		_ = tx.Rollback(ctx)
		return nil, nil, nil
	}
	return out, tx, nil
}

// MarkPublished is the single commit point of a relay cycle: it stages
// published_at=now() for the given ids and commits the tx returned by LockBatch,
// atomically persisting both these published marks and any MarkFailed updates
// staged earlier on the same tx. Pass nil/empty ids to commit a cycle with no
// successful publishes (which also releases the row locks held by LockBatch).
func (r *OutboxRepo) MarkPublished(ctx context.Context, tx order.Tx, ids []int64) error {
	ptx, ok := tx.(pgx.Tx)
	if !ok {
		return fmt.Errorf("outbox: tx must be pgx.Tx")
	}
	if len(ids) == 0 {
		return ptx.Commit(ctx)
	}
	if _, err := ptx.Exec(ctx, `UPDATE outbox_event SET published_at = now() WHERE id = ANY($1)`, ids); err != nil {
		_ = ptx.Rollback(ctx)
		return err
	}
	return ptx.Commit(ctx)
}

// MarkFailed stages an attempts++ / last_error update for the given id on the
// cycle's tx. It does NOT commit — the relay records every per-event failure
// here and then commits the whole cycle once via MarkPublished. (Committing per
// failure mid-batch would close the tx and make the cycle-final MarkPublished
// run on a dead tx, leaving already-published events unmarked and re-delivered.)
func (r *OutboxRepo) MarkFailed(ctx context.Context, tx order.Tx, id int64, lastError string) error {
	ptx, ok := tx.(pgx.Tx)
	if !ok {
		return fmt.Errorf("outbox: tx must be pgx.Tx")
	}
	_, err := ptx.Exec(ctx, `UPDATE outbox_event SET attempts = attempts + 1, last_error = $2 WHERE id=$1`, id, lastError)
	return err
}
