package payroll

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"
)

type BatchRepository interface {
	Create(ctx context.Context, b *Batch) error
	// CreateTx inserts a batch row inside an existing transaction so the batch
	// insert and downstream entry inserts succeed or fail together.
	CreateTx(ctx context.Context, tx pgx.Tx, b *Batch) error
	GetByID(ctx context.Context, id string) (*Batch, error)
	GetByPeriod(ctx context.Context, start, end time.Time) (*Batch, error)
	UpdateStatusTx(ctx context.Context, tx pgx.Tx, id string, from, to BatchStatus, lockedBy *string) error
	SetExportInfoTx(ctx context.Context, tx pgx.Tx, id, uri string, exportedAt time.Time) error
	List(ctx context.Context, statuses []BatchStatus) ([]*Batch, error)
}

type EntryRepository interface {
	CreateTx(ctx context.Context, tx pgx.Tx, e *Entry) error
	GetByID(ctx context.Context, id string) (*Entry, error)
	ListByBatch(ctx context.Context, batchID string) ([]*Entry, error)
	IncrementRefundedTx(ctx context.Context, tx pgx.Tx, id string, refund int64) error
	// FindByOrderForUser returns the entry id whose order_ids array contains
	// orderID and whose user_id matches userID. Returns ErrEntryNotFound when
	// no row matches.
	FindByOrderForUser(ctx context.Context, userID, orderID string) (string, error)
	// ListByUser returns a user's entries joined with their batches, newest
	// period first — the employee deduction-history view.
	ListByUser(ctx context.Context, userID string) ([]*EmployeeEntry, error)
}

// CurrentLinesLister loads the per-order lines for an employee's
// in-progress (not-yet-locked) payroll period. Kept separate from
// EntryRepository so the per-order detail query can evolve independently of
// the batch/entry aggregates.
type CurrentLinesLister interface {
	// ListCurrentLines returns one line per chargeable order belonging to
	// userID whose supply_date falls after the latest locked batch period.
	ListCurrentLines(ctx context.Context, userID string) ([]CurrentPayrollLine, error)
}

type ExceptionRepository interface {
	// UpsertDepartedTx detects batch entries whose employee is no longer
	// active and inserts an employee_departed exception for each. Idempotent
	// via the (batch_id, entry_id, kind) unique index.
	UpsertDepartedTx(ctx context.Context, tx pgx.Tx, batchID string) error
	// UpsertDeparted is the pool-based variant for on-demand re-detection.
	UpsertDeparted(ctx context.Context, batchID string) error
	Create(ctx context.Context, e *Exception) error
	GetByID(ctx context.Context, id string) (*Exception, error)
	ListByBatch(ctx context.Context, batchID string) ([]*Exception, error)
	Resolve(ctx context.Context, id string, status ExceptionStatus, resolution, resolvedBy string) error
}

type DisputeRepository interface {
	Create(ctx context.Context, d *Dispute) error
	GetByID(ctx context.Context, id string) (*Dispute, error)
	UpdateStatusTx(ctx context.Context, tx pgx.Tx, id string, status DisputeStatus, resolvedBy *string, resolution string, refundMinor int64) error
	ListByStatus(ctx context.Context, statuses []DisputeStatus) ([]*Dispute, error)
	ListByUser(ctx context.Context, userID string) ([]*Dispute, error)
}
