package settlement

import (
	"context"
	plaudit "github.com/Agentic-Build/corporate-catering-system/services/api/internal/platform/audit"
	"time"

	"github.com/jackc/pgx/v5"
)

// SettlementRepository persists and reads vendor_settlement rows.
type SettlementRepository interface {
	// CreateTx inserts one settlement row using the caller's transaction so the
	// row insert and the audit_event write commit (or roll back) together.
	CreateTx(ctx context.Context, tx pgx.Tx, s *Settlement) error
	GetByID(ctx context.Context, id string) (*Settlement, error)
	// ListByVendor returns a vendor's settlements newest period first.
	ListByVendor(ctx context.Context, vendorID string) ([]*Settlement, error)
	// ListByPeriod returns every settlement whose period overlaps [start, end].
	// Used by the admin all-vendor overview.
	ListByPeriod(ctx context.Context, start, end time.Time) ([]*Settlement, error)
	// VoidTx flips a closed settlement to void inside a transaction. Returns
	// ErrInvalidTransition when the row is not currently closed.
	VoidTx(ctx context.Context, tx pgx.Tx, id string) error
}

// OrderAggregateRepository reads the order table to derive settlement numbers.
// Inclusion is intentionally identical to payroll: status ∈ {picked_up, no_show},
// sliced by supply_date — the vendor payable must reconcile against the employee
// deductions payroll computes from the same orders.
type OrderAggregateRepository interface {
	// AggregateByVendor rolls up picked_up/no_show orders in [start, end] into one
	// VendorAggregate per vendor that has at least one such order.
	AggregateByVendor(ctx context.Context, start, end time.Time) ([]*VendorAggregate, error)
	// AggregateForVendor rolls up one vendor's picked_up/no_show orders in
	// [start, end]. Zero-valued (but non-nil) when the vendor has none.
	AggregateForVendor(ctx context.Context, vendorID string, start, end time.Time) (*VendorAggregate, error)
	// StatusBreakdownForVendor counts a vendor's orders by status in [start, end].
	StatusBreakdownForVendor(ctx context.Context, vendorID string, start, end time.Time) (StatusBreakdown, error)
	// OrderLinesByIDs expands a settlement's frozen order_ids into order-level
	// detail (supply_date, status, total, portion count).
	OrderLinesByIDs(ctx context.Context, orderIDs []string) ([]*SettlementOrderLine, error)
}

// AuditTx mirrors the audit-repo shape shared across services so settlement
// writes can append an audit_event inside the same transaction.
type AuditTxWriter interface {
	WriteTx(ctx context.Context, tx pgx.Tx, e plaudit.Entry) error
}
