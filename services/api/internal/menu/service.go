package menu

import (
	"context"
	"time"
)

// Service orchestrates menu CRUD with vendor-ownership enforcement.
// It depends only on the repository interfaces; no transport concerns.
type Service struct {
	Categories CategoryRepository
	Items      ItemRepository
	Images     ImageRepository
}

type CreateCategoryInput struct {
	VendorID  string
	Name      string
	SortOrder int
}

type CreateItemInput struct {
	VendorID    string
	CategoryID  *string
	Name        string
	Description string
	PriceMinor  int64
	Tags        []string
	Badges      []string
}

type UpdateItemInput struct {
	Name        string
	Description string
	PriceMinor  int64
	Tags        []string
	Badges      []string
	CategoryID  *string
}

// CreateCategory creates a new category owned by the supplied vendor.
func (s *Service) CreateCategory(ctx context.Context, in CreateCategoryInput) (*Category, error) {
	c := &Category{VendorID: in.VendorID, Name: in.Name, SortOrder: in.SortOrder}
	if err := s.Categories.Create(ctx, c); err != nil {
		return nil, err
	}
	return c, nil
}

// ListCategories returns categories owned by the vendor.
func (s *Service) ListCategories(ctx context.Context, vendorID string) ([]*Category, error) {
	return s.Categories.ListByVendor(ctx, vendorID)
}

// CreateItem creates a menu item in draft status owned by the supplied vendor.
// Nil tag/badge slices are normalized to empty slices so JSON encoding emits [].
func (s *Service) CreateItem(ctx context.Context, in CreateItemInput) (*Item, error) {
	item := &Item{
		VendorID:    in.VendorID,
		CategoryID:  in.CategoryID,
		Name:        in.Name,
		Description: in.Description,
		PriceMinor:  in.PriceMinor,
		Tags:        in.Tags,
		Badges:      in.Badges,
		Status:      ItemStatusDraft,
	}
	if item.Tags == nil {
		item.Tags = []string{}
	}
	if item.Badges == nil {
		item.Badges = []string{}
	}
	if err := s.Items.Create(ctx, item); err != nil {
		return nil, err
	}
	return item, nil
}

// UpdateItem mutates editable fields. Returns ErrForbidden if the item does
// not belong to the supplied vendor.
func (s *Service) UpdateItem(ctx context.Context, itemID, vendorID string, in UpdateItemInput) (*Item, error) {
	existing, err := s.Items.GetByID(ctx, itemID)
	if err != nil {
		return nil, err
	}
	if existing.VendorID != vendorID {
		return nil, ErrForbidden
	}
	existing.Name = in.Name
	existing.Description = in.Description
	existing.PriceMinor = in.PriceMinor
	existing.Tags = in.Tags
	existing.Badges = in.Badges
	existing.CategoryID = in.CategoryID
	if existing.Tags == nil {
		existing.Tags = []string{}
	}
	if existing.Badges == nil {
		existing.Badges = []string{}
	}
	if err := s.Items.Update(ctx, existing); err != nil {
		return nil, err
	}
	return existing, nil
}

// Publish transitions an item to active. Vendor must own the item.
func (s *Service) Publish(ctx context.Context, itemID, vendorID string) error {
	item, err := s.Items.GetByID(ctx, itemID)
	if err != nil {
		return err
	}
	if item.VendorID != vendorID {
		return ErrForbidden
	}
	return s.Items.SetStatus(ctx, itemID, ItemStatusActive)
}

// Archive transitions an item to archived. Vendor must own the item.
func (s *Service) Archive(ctx context.Context, itemID, vendorID string) error {
	item, err := s.Items.GetByID(ctx, itemID)
	if err != nil {
		return err
	}
	if item.VendorID != vendorID {
		return ErrForbidden
	}
	return s.Items.SetStatus(ctx, itemID, ItemStatusArchived)
}

// ListByVendor returns the vendor's items, optionally including archived rows.
func (s *Service) ListByVendor(ctx context.Context, vendorID string, includeArchived bool) ([]*Item, error) {
	return s.Items.ListByVendor(ctx, vendorID, includeArchived)
}

// ListImagesByItem returns images attached to an item for display.
func (s *Service) ListImagesByItem(ctx context.Context, itemID string) ([]*Image, error) {
	return s.Images.ListByItem(ctx, itemID)
}

// EmployeeMenuItem is the projection returned to employee clients.
type EmployeeMenuItem struct {
	ID           string
	VendorID     string
	VendorName   string
	Name         string
	Description  string
	PriceMinor   int64
	Tags         []string
	Badges       []string
	Images       []string
	Capacity     int
	Remain       int
	SoldOut      bool
	PickupWindow string
	ETALabel     string
}

// ListForEmployee returns active menu items available at the given plant on
// the given day, including supply data and images. The plant filter ensures
// employees only see vendors that serve their plant (per vendor_plant_mapping).
func (s *Service) ListForEmployee(ctx context.Context, plant string, day time.Time) ([]EmployeeMenuItem, error) {
	rows, err := s.Items.ListActiveByPlant(ctx, plant, day)
	if err != nil {
		return nil, err
	}
	out := make([]EmployeeMenuItem, 0, len(rows))
	for _, r := range rows {
		imgs, err := s.Images.ListByItem(ctx, r.Item.ID)
		if err != nil {
			return nil, err
		}
		uris := make([]string, 0, len(imgs))
		for _, im := range imgs {
			uris = append(uris, im.BlobURI)
		}
		tags := r.Item.Tags
		if tags == nil {
			tags = []string{}
		}
		badges := r.Item.Badges
		if badges == nil {
			badges = []string{}
		}
		out = append(out, EmployeeMenuItem{
			ID:           r.Item.ID,
			VendorID:     r.Item.VendorID,
			VendorName:   r.VendorName,
			Name:         r.Item.Name,
			Description:  r.Item.Description,
			PriceMinor:   r.Item.PriceMinor,
			Tags:         tags,
			Badges:       badges,
			Images:       uris,
			Capacity:     r.Capacity,
			Remain:       r.Remain,
			SoldOut:      r.Remain == 0,
			PickupWindow: r.PickupWindow,
			ETALabel:     r.ETALabel,
		})
	}
	return out, nil
}
