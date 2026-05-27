package vendor

import "context"

type Repository interface {
	GetByID(ctx context.Context, id string) (*Vendor, error)
	GetByEmail(ctx context.Context, email string) (*Vendor, error)
	Create(ctx context.Context, v *Vendor) error
	UpdateStatus(ctx context.Context, id string, status Status, approvedBy *string) error
	UpdateSettings(ctx context.Context, id string, cutoffHour, preorderWindowDays int) error
	UpdateContactEmail(ctx context.Context, id, email string) error
	List(ctx context.Context, statuses []Status) ([]*Vendor, error)
}

type PlantMappingRepository interface {
	ListByVendor(ctx context.Context, vendorID string) ([]*PlantMapping, error)
	ListVendorsForPlant(ctx context.Context, plant string) ([]string, error)
	Set(ctx context.Context, vendorID string, plants []string) error
	// SetWindow sets the service window for one vendor×plant mapping.
	SetWindow(ctx context.Context, vendorID, plant, window string) error
}

type OperatorRepository interface {
	Get(ctx context.Context, vendorID, operatorID string) (*OperatorAccount, error)
	ListByVendor(ctx context.Context, vendorID string) ([]*OperatorAccount, error)
	ListByVendorStatus(ctx context.Context, vendorID string, statuses []OperatorStatus) ([]*OperatorAccount, error)
	Upsert(ctx context.Context, op *OperatorAccount) error
	SetStatus(ctx context.Context, vendorID, operatorID string, status OperatorStatus) error
	SetStatuses(ctx context.Context, vendorID string, from []OperatorStatus, to OperatorStatus) error
}
