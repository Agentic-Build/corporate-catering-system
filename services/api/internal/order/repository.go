package order

import (
	"context"
	plaudit "github.com/Agentic-Build/corporate-catering-system/services/api/internal/platform/audit"
	"time"
)

type Repository interface {
	Create(ctx context.Context, o *Order) error
	GetByID(ctx context.Context, id string) (*Order, error)
	UpdateStatus(ctx context.Context, id string, from, to Status, actorID *string, actorRole *string, reason string) error
	ListByUser(ctx context.Context, userID string, sinceDate time.Time) ([]*Order, error)
	ListPlacedDueForCutoff(ctx context.Context, before time.Time) ([]*Order, error)
	ListReadyOlderThan(ctx context.Context, threshold time.Time) ([]*Order, error)
	ListByVendorDay(ctx context.Context, vendorID string, day time.Time, statuses []Status) ([]*Order, error)
	// ListPickedOrNoShowInPeriod returns all orders in {picked_up, no_show} whose
	// supply_date falls within [from, to] inclusive. Used by the payroll Build step
	// to aggregate per-user entries for a monthly batch.
	ListPickedOrNoShowInPeriod(ctx context.Context, from, to time.Time) ([]*Order, error)
}

type StateEventRepository interface {
	Append(ctx context.Context, ev *StateEvent) error
	ListByOrder(ctx context.Context, orderID string) ([]*StateEvent, error)
}

type AuditWriter interface {
	Write(ctx context.Context, e plaudit.Entry) error
}

type OutboxRepository interface {
	// Append within an existing transaction (the order's transaction).
	Append(ctx context.Context, tx Tx, aggregateType, aggregateID, subject string, payload map[string]any, headers map[string]any) error
	// Used by relay worker (not service callers).
	LockBatch(ctx context.Context, limit int) ([]*OutboxEvent, Tx, error)
	MarkPublished(ctx context.Context, tx Tx, ids []int64) error
	MarkFailed(ctx context.Context, tx Tx, id int64, lastError string) error
}

type OutboxEvent struct {
	ID            int64
	AggregateType string
	AggregateID   string
	Subject       string
	Payload       map[string]any
	Headers       map[string]any
	CreatedAt     time.Time
	PublishedAt   *time.Time
	Attempts      int
	LastError     *string
}

// Tx is an opaque database transaction handle used by Append/MarkPublished/MarkFailed.
type Tx interface{}
