package menu

import (
	"context"
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
	Images      []string
}

type UpdateItemInput struct {
	Name        string
	Description string
	PriceMinor  int64
	Tags        []string
	CategoryID  *string
	Images      []string
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

// CreateItem creates an active menu item owned by the supplied vendor.
// Nil tag slices are normalised to [] so JSON encoding emits [].
func (s *Service) CreateItem(ctx context.Context, in CreateItemInput) (*Item, error) {
	item := &Item{
		VendorID:    in.VendorID,
		CategoryID:  in.CategoryID,
		Name:        in.Name,
		Description: in.Description,
		PriceMinor:  in.PriceMinor,
		Tags:        in.Tags,
		Status:      ItemStatusActive,
	}
	if item.Tags == nil {
		item.Tags = []string{}
	}
	if err := s.Items.Create(ctx, item); err != nil {
		return nil, err
	}
	if len(in.Images) > 0 {
		if err := s.Images.ReplaceForItem(ctx, item.ID, in.Images); err != nil {
			return nil, err
		}
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
	existing.CategoryID = in.CategoryID
	if existing.Tags == nil {
		existing.Tags = []string{}
	}
	if err := s.Items.Update(ctx, existing); err != nil {
		return nil, err
	}
	// The merchant edit form submits the full image set; replace unconditionally
	// so an empty slice clears all images.
	if err := s.Images.ReplaceForItem(ctx, existing.ID, in.Images); err != nil {
		return nil, err
	}
	return existing, nil
}

// CopyItem duplicates an item into a fresh draft owned by the same vendor
// (merchant's "上架改量"). Name is suffixed so the two are distinguishable.
func (s *Service) CopyItem(ctx context.Context, itemID, vendorID string) (*Item, error) {
	src, err := s.Items.GetByID(ctx, itemID)
	if err != nil {
		return nil, err
	}
	if src.VendorID != vendorID {
		return nil, ErrForbidden
	}
	imgs, err := s.Images.ListByItem(ctx, itemID)
	if err != nil {
		return nil, err
	}
	uris := make([]string, 0, len(imgs))
	for _, im := range imgs {
		uris = append(uris, im.BlobURI)
	}
	return s.CreateItem(ctx, CreateItemInput{
		VendorID:    vendorID,
		CategoryID:  src.CategoryID,
		Name:        src.Name + "（複製）",
		Description: src.Description,
		PriceMinor:  src.PriceMinor,
		Tags:        src.Tags,
		Images:      uris,
	})
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

// ListByVendor returns the vendor's items with usage stats, optionally
// including archived rows.
func (s *Service) ListByVendor(ctx context.Context, vendorID string, includeArchived bool) ([]*MerchantItemRow, error) {
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
	Images       []string
	Capacity     int
	Remain       int
	SoldOut      bool
	PickupWindow string
	ETALabel     string
}

// ListForEmployee returns active items at the filter's plant/day with supply
// data and images. Plant filter (vendor_plant_mapping) hides vendors that
// don't serve the employee's plant. Search/filter/sort push down to SQL.
func (s *Service) ListForEmployee(ctx context.Context, f EmployeeMenuFilter) ([]EmployeeMenuItem, error) {
	rows, err := s.Items.ListActiveByPlant(ctx, f)
	if err != nil {
		return nil, err
	}
	imagesByItem, err := s.imageURIsForRows(ctx, rows)
	if err != nil {
		return nil, err
	}
	out := make([]EmployeeMenuItem, 0, len(rows))
	for _, r := range rows {
		uris := imagesByItem[r.Item.ID]
		if uris == nil {
			uris = []string{}
		}
		tags := r.Item.Tags
		if tags == nil {
			tags = []string{}
		}
		out = append(out, EmployeeMenuItem{
			ID:           r.Item.ID,
			VendorID:     r.Item.VendorID,
			VendorName:   r.VendorName,
			Name:         r.Item.Name,
			Description:  r.Item.Description,
			PriceMinor:   r.Item.PriceMinor,
			Tags:         tags,
			Images:       uris,
			Capacity:     r.Capacity,
			Remain:       r.Remain,
			SoldOut:      r.Remain <= 0 || r.SoldOut,
			PickupWindow: r.PickupWindow,
			ETALabel:     r.ETALabel,
		})
	}
	return out, nil
}

// BatchImageLister is an optional capability (postgres ImageRepo): load
// images for many items in one query, avoiding ListForEmployee's N+1.
type BatchImageLister interface {
	ListByItems(ctx context.Context, itemIDs []string) (map[string][]*Image, error)
}

// imageURIsForRows returns item ID → ordered blob URIs for the given rows,
// using a single batched query when the image repo supports it.
func (s *Service) imageURIsForRows(ctx context.Context, rows []*ActiveItemRow) (map[string][]string, error) {
	out := make(map[string][]string, len(rows))
	toURIs := func(imgs []*Image) []string {
		uris := make([]string, 0, len(imgs))
		for _, im := range imgs {
			uris = append(uris, im.BlobURI)
		}
		return uris
	}
	if batch, ok := s.Images.(BatchImageLister); ok {
		ids := make([]string, len(rows))
		for i, r := range rows {
			ids[i] = r.Item.ID
		}
		byItem, err := batch.ListByItems(ctx, ids)
		if err != nil {
			return nil, err
		}
		for id, imgs := range byItem {
			out[id] = toURIs(imgs)
		}
		return out, nil
	}
	for _, r := range rows {
		imgs, err := s.Images.ListByItem(ctx, r.Item.ID)
		if err != nil {
			return nil, err
		}
		out[r.Item.ID] = toURIs(imgs)
	}
	return out, nil
}
