package menu

import (
	"context"
)

type CategoryRepository interface {
	Create(ctx context.Context, c *Category) error
	Update(ctx context.Context, c *Category) error
	Delete(ctx context.Context, id string) error
	GetByID(ctx context.Context, id string) (*Category, error)
	ListByVendor(ctx context.Context, vendorID string) ([]*Category, error)
}

type ItemRepository interface {
	Create(ctx context.Context, i *Item) error
	Update(ctx context.Context, i *Item) error
	SetStatus(ctx context.Context, id string, status ItemStatus) error
	GetByID(ctx context.Context, id string) (*Item, error)
	ListByVendor(ctx context.Context, vendorID string, includeArchived bool) ([]*MerchantItemRow, error)
	ListActiveByPlant(ctx context.Context, f EmployeeMenuFilter) ([]*ActiveItemRow, error)
}

type ImageRepository interface {
	Add(ctx context.Context, img *Image) error
	Remove(ctx context.Context, id string) error
	ListByItem(ctx context.Context, itemID string) ([]*Image, error)
	// ReplaceForItem deletes all images for the item and re-inserts uris in
	// order (sort_order = index). A nil/empty slice clears all images.
	ReplaceForItem(ctx context.Context, itemID string, uris []string) error
}
