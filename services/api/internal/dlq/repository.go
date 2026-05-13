package dlq

import "context"

// Repository is the persistence contract for synthetic DLQ rows. Write is
// called from workers via messaging.WriteDLQ; the rest is consumed by the
// admin HTTP layer.
type Repository interface {
	// Write inserts a new DLQ row. ID/FirstSeenAt are populated by the DB
	// and reflected back onto the receiver.
	Write(ctx context.Context, m *Message) error
	// GetByID fetches a single row; returns ErrMessageNotFound if missing.
	GetByID(ctx context.Context, id string) (*Message, error)
	// ListPending returns rows that are neither replayed nor resolved,
	// optionally filtered by stream (empty = any stream).
	ListPending(ctx context.Context, stream string, limit int) ([]*Message, error)
	// MarkReplayed stamps replayed_at/replayed_by; the message must still be
	// pending. Returns ErrAlreadyResolved if it was already replayed or
	// resolved, ErrMessageNotFound if missing.
	MarkReplayed(ctx context.Context, id, replayedBy string) error
	// MarkResolved stamps resolved_at/resolved_by/resolved_notes; the message
	// must still be pending. Returns ErrAlreadyResolved if it was already
	// replayed or resolved, ErrMessageNotFound if missing.
	MarkResolved(ctx context.Context, id, resolvedBy, notes string) error
}
