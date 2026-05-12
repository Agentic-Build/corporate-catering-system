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
