package compliance

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"
)

type DocumentRepository interface {
	Create(ctx context.Context, d *Document) error
	CreateTx(ctx context.Context, tx pgx.Tx, d *Document) error
	GetByID(ctx context.Context, id string) (*Document, error)
	ListByVendor(ctx context.Context, vendorID string, includeAll bool) ([]*Document, error)
	UpdateStatus(ctx context.Context, id string, status DocumentStatus, reviewedBy *string, notes string) error
	UpdateStatusTx(ctx context.Context, tx pgx.Tx, id string, status DocumentStatus, reviewedBy *string, notes string) error
	ListExpiringBefore(ctx context.Context, before time.Time) ([]*Document, error)
	ListPastExpiry(ctx context.Context, now time.Time) ([]*Document, error)
}

type AnomalyRepository interface {
	// Open creates or updates an open anomaly. The partial unique index
	// (kind, target_kind, target_id) WHERE status='open' enforces dedup;
	// we use INSERT ... ON CONFLICT DO UPDATE on payload/severity/evidence/updated_at.
	Open(ctx context.Context, a *Anomaly) error
	GetByID(ctx context.Context, id string) (*Anomaly, error)
	List(ctx context.Context, statuses []AnomalyStatus, severities []AnomalySeverity) ([]*Anomaly, error)
	Triage(ctx context.Context, id string, by string, notes string) error
	TriageTx(ctx context.Context, tx pgx.Tx, id string, by string, notes string) error
	Close(ctx context.Context, id string, by string, notes string) error
	CloseTx(ctx context.Context, tx pgx.Tx, id string, by string, notes string) error
}
