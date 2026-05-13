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
