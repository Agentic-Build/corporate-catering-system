package payroll

import "time"

type BatchStatus string

const (
	BatchStatusDraft    BatchStatus = "draft"
	BatchStatusLocked   BatchStatus = "locked"
	BatchStatusExported BatchStatus = "exported"
	BatchStatusClosed   BatchStatus = "closed"
)

type DisputeStatus string

const (
	DisputeStatusOpen           DisputeStatus = "open"
	DisputeStatusResolvedRefund DisputeStatus = "resolved_refund"
	DisputeStatusResolvedReject DisputeStatus = "resolved_reject"
	DisputeStatusCancelled      DisputeStatus = "cancelled"
)

type Batch struct {
	ID          string
	PeriodStart time.Time
	PeriodEnd   time.Time
	Status      BatchStatus
	LockedAt    *time.Time
	LockedBy    *string
	ExportedAt  *time.Time
	ExportURI   *string
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

type Entry struct {
	ID            string
	BatchID       string
	UserID        string
	OrderIDs      []string
	AmountMinor   int64
	RefundedMinor int64
	CreatedAt     time.Time
}

type ExceptionKind string

const (
	// ExceptionEmployeeDeparted is auto-detected: the entry's employee is no
	// longer active (suspended / terminated).
	ExceptionEmployeeDeparted ExceptionKind = "employee_departed"
	// ExceptionDeductionFailed is flagged manually by a welfare admin.
	ExceptionDeductionFailed ExceptionKind = "deduction_failed"
)

type ExceptionStatus string

const (
	ExceptionOpen     ExceptionStatus = "open"
	ExceptionResolved ExceptionStatus = "resolved" // handled outside the system; entry still deducted
	ExceptionExcluded ExceptionStatus = "excluded" // entry dropped from the HR deduction file
)

// Exception is a payroll entry that needs manual handling before the HR
// deduction file is exported.
type Exception struct {
	ID         string
	BatchID    string
	EntryID    string
	UserID     string
	Kind       ExceptionKind
	Status     ExceptionStatus
	Detail     string
	Resolution string
	ResolvedBy *string
	ResolvedAt *time.Time
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

type Dispute struct {
	ID          string
	EntryID     string
	OrderID     string
	OpenedBy    string
	Reason      string
	Status      DisputeStatus
	Resolution  string
	ResolvedBy  *string
	ResolvedAt  *time.Time
	RefundMinor int64
	CreatedAt   time.Time
	UpdatedAt   time.Time
}
