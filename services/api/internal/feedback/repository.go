package feedback

import (
	"context"
	plaudit "github.com/Agentic-Build/corporate-catering-system/services/api/internal/platform/audit"
	"time"

	"github.com/jackc/pgx/v5"
)

// RatingRepository persists per-order meal ratings.
type RatingRepository interface {
	CreateTx(ctx context.Context, tx pgx.Tx, r *Rating) error
	GetByOrder(ctx context.Context, orderID string) (*Rating, error)
	// AggregateByVendorSince returns per-vendor avg score + sample count since
	// `since`. Used by FeedbackScanner.
	AggregateByVendorSince(ctx context.Context, since time.Time) ([]VendorRatingStat, error)
}

// ComplaintRepository persists meal complaints and their workflow state.
type ComplaintRepository interface {
	CreateTx(ctx context.Context, tx pgx.Tx, c *Complaint) error
	GetByID(ctx context.Context, id string) (*Complaint, error)
	// UpdateStatusTx: conditional transition (current==from); 0 rows →
	// ErrInvalidTransition. Mutable fields commit together.
	UpdateStatusTx(ctx context.Context, tx pgx.Tx, id string, from, to ComplaintStatus, fields ComplaintUpdate) error
	ListByUser(ctx context.Context, userID string) ([]*Complaint, error)
	ListByVendor(ctx context.Context, vendorID string, statuses []ComplaintStatus) ([]*Complaint, error)
	ListByStatus(ctx context.Context, statuses []ComplaintStatus) ([]*Complaint, error)
	// CountByVendorSince returns per-vendor counts since `since` (FeedbackScanner).
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

// OrderReader is the minimal order-repo subset Service needs.
type OrderReader interface {
	GetOrderInfo(ctx context.Context, id string) (*OrderInfo, error)
}

// OrderReverser reverses (沖銷) the salary deduction tied to a single order.
// Wired to payroll.Service.ReverseOrder. Implementations MUST be idempotent.
type OrderReverser interface {
	ReverseOrder(ctx context.Context, orderID string) error
}

// AuditTx mirrors the audit-repo shape used by other services.
type AuditTxWriter interface {
	WriteTx(ctx context.Context, tx pgx.Tx, e plaudit.Entry) error
}
