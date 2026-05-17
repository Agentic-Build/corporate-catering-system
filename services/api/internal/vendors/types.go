package vendor

import "time"

type Status string

const (
	StatusPending    Status = "pending"
	StatusApproved   Status = "approved"
	StatusSuspended  Status = "suspended"
	StatusTerminated Status = "terminated"
)

type Vendor struct {
	ID           string
	DisplayName  string
	LegalName    string
	ContactEmail string
	Status       Status
	ApprovedAt   *time.Time
	ApprovedBy   *string
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

type PlantMapping struct {
	ID        string
	VendorID  string
	Plant     string
	Active    bool
	CreatedAt time.Time
}

type OperatorStatus string

const (
	OperatorStatusActive          OperatorStatus = "active"
	OperatorStatusSuspended       OperatorStatus = "suspended"
	OperatorStatusVendorSuspended OperatorStatus = "vendor_suspended"
)

type OperatorAccount struct {
	ID              string
	VendorID        string
	Email           string
	DisplayName     string
	Provider        string
	ExternalSubject *string
	Status          OperatorStatus
	SetupURL        *string
	LastSyncedAt    *time.Time
	CreatedAt       time.Time
	UpdatedAt       time.Time
}
