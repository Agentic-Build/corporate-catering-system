// Package postgres is the Postgres implementation of dlq.Repository.
package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Agentic-Build/corporate-catering-system/services/api/internal/dlq"
)

// DLQRepo persists DLQ rows in Postgres.
type DLQRepo struct{ pool *pgxpool.Pool }

// NewDLQRepo wires a DLQRepo to the given pgx pool.
func NewDLQRepo(p *pgxpool.Pool) *DLQRepo { return &DLQRepo{pool: p} }

const dlqCols = `id, source_stream, source_subject, source_consumer, payload, headers,
       last_error, first_seen_at, replayed_at, replayed_by, resolved_at, resolved_by, resolved_notes`

// Write inserts a new DLQ row and populates ID/FirstSeenAt on the receiver.
func (r *DLQRepo) Write(ctx context.Context, m *dlq.Message) error {
	payload := m.Payload
	if payload == nil {
		payload = map[string]any{}
	}
	headers := m.Headers
	if headers == nil {
		headers = map[string]any{}
	}
	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal dlq payload: %w", err)
	}
	headersJSON, err := json.Marshal(headers)
	if err != nil {
		return fmt.Errorf("marshal dlq headers: %w", err)
	}
	err = r.pool.QueryRow(ctx, `
INSERT INTO dlq_message (source_stream, source_subject, source_consumer, payload, headers, last_error)
VALUES ($1, $2, $3, $4::jsonb, $5::jsonb, $6)
RETURNING id, first_seen_at`,
		m.SourceStream, m.SourceSubject, m.SourceConsumer, payloadJSON, headersJSON, m.LastError,
	).Scan(&m.ID, &m.FirstSeenAt)
	if err != nil {
		return fmt.Errorf("insert dlq: %w", err)
	}
	m.Payload = payload
	m.Headers = headers
	return nil
}

// GetByID returns the single row matching id.
func (r *DLQRepo) GetByID(ctx context.Context, id string) (*dlq.Message, error) {
	var m dlq.Message
	var payloadJSON, headersJSON []byte
	err := r.pool.QueryRow(ctx, `SELECT `+dlqCols+` FROM dlq_message WHERE id=$1`, id).Scan(
		&m.ID, &m.SourceStream, &m.SourceSubject, &m.SourceConsumer,
		&payloadJSON, &headersJSON, &m.LastError, &m.FirstSeenAt,
		&m.ReplayedAt, &m.ReplayedBy, &m.ResolvedAt, &m.ResolvedBy, &m.ResolvedNotes,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, dlq.ErrMessageNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("scan dlq: %w", err)
	}
	if len(payloadJSON) > 0 {
		_ = json.Unmarshal(payloadJSON, &m.Payload)
	}
	if len(headersJSON) > 0 {
		_ = json.Unmarshal(headersJSON, &m.Headers)
	}
	return &m, nil
}

// ListPending returns pending rows (no replay/resolve stamp). When stream is
// non-empty it filters to that stream. limit caps the result.
func (r *DLQRepo) ListPending(ctx context.Context, stream string, limit int) ([]*dlq.Message, error) {
	if limit <= 0 {
		limit = 100
	}
	q := `SELECT ` + dlqCols + ` FROM dlq_message
WHERE replayed_at IS NULL AND resolved_at IS NULL`
	args := []any{}
	if stream != "" {
		args = append(args, stream)
		q += fmt.Sprintf(" AND source_stream=$%d", len(args))
	}
	args = append(args, limit)
	q += fmt.Sprintf(" ORDER BY first_seen_at DESC LIMIT $%d", len(args))

	rows, err := r.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("query dlq: %w", err)
	}
	defer rows.Close()
	var out []*dlq.Message
	for rows.Next() {
		var m dlq.Message
		var payloadJSON, headersJSON []byte
		if err := rows.Scan(
			&m.ID, &m.SourceStream, &m.SourceSubject, &m.SourceConsumer,
			&payloadJSON, &headersJSON, &m.LastError, &m.FirstSeenAt,
			&m.ReplayedAt, &m.ReplayedBy, &m.ResolvedAt, &m.ResolvedBy, &m.ResolvedNotes,
		); err != nil {
			return nil, fmt.Errorf("scan dlq row: %w", err)
		}
		if len(payloadJSON) > 0 {
			_ = json.Unmarshal(payloadJSON, &m.Payload)
		}
		if len(headersJSON) > 0 {
			_ = json.Unmarshal(headersJSON, &m.Headers)
		}
		out = append(out, &m)
	}
	return out, rows.Err()
}

// MarkReplayed stamps replayed_at/replayed_by on a pending row.
func (r *DLQRepo) MarkReplayed(ctx context.Context, id, replayedBy string) error {
	return r.markTerminal(ctx, id, `
UPDATE dlq_message
SET replayed_at = now(), replayed_by = $2
WHERE id = $1 AND replayed_at IS NULL AND resolved_at IS NULL`, replayedBy)
}

// MarkResolved stamps resolved_at/resolved_by/resolved_notes on a pending row.
func (r *DLQRepo) MarkResolved(ctx context.Context, id, resolvedBy, notes string) error {
	return r.markTerminal(ctx, id, `
UPDATE dlq_message
SET resolved_at = now(), resolved_by = $2, resolved_notes = $3
WHERE id = $1 AND replayed_at IS NULL AND resolved_at IS NULL`, resolvedBy, notes)
}

// markTerminal runs an UPDATE that should affect exactly one row when the
// target is pending; otherwise it discriminates between not-found and
// already-terminal via a second probe.
func (r *DLQRepo) markTerminal(ctx context.Context, id, sql string, args ...any) error {
	queryArgs := append([]any{id}, args...)
	tag, err := r.pool.Exec(ctx, sql, queryArgs...)
	if err != nil {
		return fmt.Errorf("update dlq: %w", err)
	}
	if tag.RowsAffected() == 1 {
		return nil
	}
	// Zero rows: either id doesn't exist, or the row is already terminal.
	var exists bool
	if err := r.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM dlq_message WHERE id=$1)`, id).Scan(&exists); err != nil {
		return fmt.Errorf("probe dlq existence: %w", err)
	}
	if !exists {
		return dlq.ErrMessageNotFound
	}
	return dlq.ErrAlreadyResolved
}
