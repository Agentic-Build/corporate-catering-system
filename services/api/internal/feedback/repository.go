package feedback

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"
)

// RatingRepository persists per-order meal ratings.
type RatingRepository interface {
	// CreateTx inserts a rating row inside an existing transaction so the
	// rating insert and its audit row commit or fail together.
	CreateTx(ctx context.Context, tx pgx.Tx, r *Rating) error
	GetByOrder(ctx context.Context, orderID string) (*Rating, error)
	// AggregateByVendorSince returns per-vendor avg score + sample count for
	// ratings created on or after `since`. Used by FeedbackScanner.
	AggregateByVendorSince(ctx context.Context, since time.Time) ([]VendorRatingStat, error)
}

// ComplaintRepository persists meal complaints and their workflow state.
type ComplaintRepository interface {
	// CreateTx inserts a complaint row inside an existing transaction.
	CreateTx(ctx context.Context, tx pgx.Tx, c *Complaint) error
	GetByID(ctx context.Context, id string) (*Complaint, error)
	// UpdateStatusTx performs a conditional status transition inside a
	// transaction: it only succeeds when the row's current status equals
	// `from`. Returns ErrInvalidTransition when 0 rows match. The mutable
	// fields carried by the transition are written together.
	UpdateStatusTx(ctx context.Context, tx pgx.Tx, id string, from, to ComplaintStatus, fields ComplaintUpdate) error
	ListByUser(ctx context.Context, userID string) ([]*Complaint, error)
	ListByVendor(ctx context.Context, vendorID string, statuses []ComplaintStatus) ([]*Complaint, error)
	ListByStatus(ctx context.Context, statuses []ComplaintStatus) ([]*Complaint, error)
	// CountByVendorSince returns per-vendor complaint counts for complaints
	// created on or after `since`. Used by FeedbackScanner.
	CountByVendorSince(ctx context.Context, since time.Time) ([]VendorComplaintStat, error)
}

// ComplaintUpdate carries the mutable fields written alongside a status
// transition. Only the fields relevant to the transition are populated.
type ComplaintUpdate struct {
	VendorResponse string
	Resolution     string
	ResolvedBy     *string
}

// OrderInfo is the order projection feedback needs to validate ownership and
// status before accepting a rating or complaint.
type OrderInfo struct {
	ID       string
	UserID   string
	VendorID string
	Status   string
}

// OrderReader is the minimal order-repo subset Service needs. It is satisfied
// by an adapter over order.Repository so feedback does not depend on the full
// order aggregate shape.
type OrderReader interface {
	GetOrderInfo(ctx context.Context, id string) (*OrderInfo, error)
}

// OrderReverser reverses (沖銷) the salary deduction tied to a single order.
// It is the hook used by AdminResolveComplaint when the welfare committee
// resolves an escalated complaint with compensation. The payroll.Service
// already exposes a matching ReverseOrder method; the orchestrator wires it
// into Service.Reverser at startup. Implementations must be idempotent: the
// same orderID may be passed more than once for the same complaint.
type OrderReverser interface {
	ReverseOrder(ctx context.Context, orderID string) error
}

// AuditTx mirrors the audit-repo shape used by order/payroll/compliance
// services so the same postgres impl serves feedback writes.
type AuditTx interface {
	WriteTx(ctx context.Context, tx pgx.Tx, actorID, actorRole *string, action, targetKind, targetID string, payload map[string]any, requestID string) error
}
