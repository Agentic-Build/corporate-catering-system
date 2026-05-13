package vendor

import "context"

type Repository interface {
	GetByID(ctx context.Context, id string) (*Vendor, error)
	GetByEmail(ctx context.Context, email string) (*Vendor, error)
	Create(ctx context.Context, v *Vendor) error
	UpdateStatus(ctx context.Context, id string, status Status, approvedBy *string) error
	List(ctx context.Context, statuses []Status) ([]*Vendor, error)
}

type PlantMappingRepository interface {
	ListByVendor(ctx context.Context, vendorID string) ([]*PlantMapping, error)
	ListVendorsForPlant(ctx context.Context, plant string) ([]string, error)
	Set(ctx context.Context, vendorID string, plants []string) error
}
