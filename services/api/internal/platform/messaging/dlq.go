package messaging

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

// WriteDLQ records an irrecoverable message failure to dlq_message.
//
// Workers call this when MaxDeliver is exceeded, or when processing logic
// determines the message can't be retried (e.g. schema mismatch, missing
// referent that will never appear). Once written, the message is visible to
// admins via GET /api/admin/dlq and can be replayed or resolved manually.
//
// This is intentionally a thin helper rather than going through a repository:
// workers already hold a *pgxpool.Pool and we want to keep DLQ writes out of
// any per-message transaction (a DLQ write is the *escape hatch* when the
// normal tx couldn't succeed).
func WriteDLQ(ctx context.Context, pool *pgxpool.Pool, stream, subject, consumer string, payload, headers map[string]any, lastError string) error {
	if payload == nil {
		payload = map[string]any{}
	}
	if headers == nil {
		headers = map[string]any{}
	}
	p, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal dlq payload: %w", err)
	}
	h, err := json.Marshal(headers)
	if err != nil {
		return fmt.Errorf("marshal dlq headers: %w", err)
	}
	if _, err := pool.Exec(ctx, `
INSERT INTO dlq_message (source_stream, source_subject, source_consumer, payload, headers, last_error)
VALUES ($1, $2, $3, $4::jsonb, $5::jsonb, $6)`,
		stream, subject, consumer, p, h, lastError); err != nil {
		return fmt.Errorf("write dlq: %w", err)
	}
	return nil
}
