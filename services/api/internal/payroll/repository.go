package payroll

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"
)

type BatchRepository interface {
	Create(ctx context.Context, b *Batch) error
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
}

type DisputeRepository interface {
	Create(ctx context.Context, d *Dispute) error
	GetByID(ctx context.Context, id string) (*Dispute, error)
	UpdateStatusTx(ctx context.Context, tx pgx.Tx, id string, status DisputeStatus, resolvedBy *string, resolution string, refundMinor int64) error
	ListByStatus(ctx context.Context, statuses []DisputeStatus) ([]*Dispute, error)
	ListByUser(ctx context.Context, userID string) ([]*Dispute, error)
}
