package identity

import "time"

type Role string

const (
	RoleEmployee       Role = "employee"
	RoleVendorOperator Role = "vendor_operator"
	RoleWelfareAdmin   Role = "welfare_admin"
)

type Status string

const (
	StatusActive     Status = "active"
	StatusSuspended  Status = "suspended"
	StatusTerminated Status = "terminated"
)

type Provider string

type User struct {
	ID           string
	PrimaryEmail string
	DisplayName  string
	Role         Role
	Status       Status
	EmployeeID   *string
	VendorID     *string
	Plant        *string
	Department   *string
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

type UserIdentity struct {
	ID              string
	UserID          string
	Provider        Provider
	ExternalSubject string
	RawClaims       map[string]any
	LinkedAt        time.Time
}
