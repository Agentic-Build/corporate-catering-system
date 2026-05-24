package quota

import (
	"context"
	"errors"
	"time"

	"github.com/takalawang/corporate-catering-system/services/api/internal/menu"
	"github.com/takalawang/corporate-catering-system/services/api/internal/platform/observability"
)

// Service exposes vendor-facing quota operations (capacity management + reads).
// Decrement/Restore stay repo-only — order placement (P3) consumes them directly.
type Service struct {
	Supplies Repository
	Items    menu.ItemRepository
}

type SetCapacityInput struct {
	MenuItemID   string
	Date         time.Time
	Capacity     int
	PickupWindow string
	ETALabel     string
	CutoffAt     time.Time
}

// SetCapacity upserts a supply row, enforcing vendor ownership of the menu_item.
// remain is initialized to capacity on first insert; subsequent upserts adjust
// capacity but DO NOT bump remain back up — already-sold units stay sold.
// When capacity drops below current remain, remain is clamped down to capacity.
func (s *Service) SetCapacity(ctx context.Context, vendorID string, in SetCapacityInput) (*Supply, error) {
	item, err := s.Items.GetByID(ctx, in.MenuItemID)
	if err != nil {
		return nil, err
	}
	if item.VendorID != vendorID {
		return nil, menu.ErrForbidden
	}
	// Check existing supply first to preserve remain across updates.
	existing, err := s.Supplies.Get(ctx, in.MenuItemID, in.Date)
	var newRemain int
	if errors.Is(err, ErrSupplyNotFound) {
		newRemain = in.Capacity
	} else if err != nil {
		return nil, err
	} else {
		// Preserve the number of already-sold units across capacity changes.
		// sold = how many units have been purchased (capacity - remain).
		// new remain = new capacity - sold, clamped to [0, new capacity].
		sold := existing.Capacity - existing.Remain
		newRemain = in.Capacity - sold
		if newRemain < 0 {
			newRemain = 0
		}
	}
	sp := &Supply{
		MenuItemID:   in.MenuItemID,
		SupplyDate:   in.Date,
		Capacity:     in.Capacity,
		Remain:       newRemain,
		PickupWindow: in.PickupWindow,
		ETALabel:     in.ETALabel,
		CutoffAt:     in.CutoffAt,
	}
	if err := s.Supplies.Upsert(ctx, sp); err != nil {
		return nil, err
	}
	emitSupplyAdjusted(ctx, vendorID, existing, sp)
	return sp, nil
}

// emitSupplyAdjusted converts a capacity change into a directional event so
// dashboards can see whether merchants are adding or pulling supply.
func emitSupplyAdjusted(ctx context.Context, vendorID string, existing, sp *Supply) {
	prev := 0
	if existing != nil {
		prev = existing.Capacity
	}
	delta := sp.Capacity - prev
	if delta == 0 {
		return
	}
	direction := "up"
	if delta < 0 {
		direction = "down"
		delta = -delta
	}
	observability.RecordSupplyAdjusted(ctx, vendorID, direction, delta)
}

// SetSoldOut flips the temporary sold-out flag for a vendor's supply on a
// given day. Capacity and remain are untouched — this is a reversible "we've
// run out today" switch, distinct from setting capacity to 0. Returns
// menu.ErrForbidden if the item is not the vendor's, ErrSupplyNotFound if no
// supply row exists for that day.
func (s *Service) SetSoldOut(ctx context.Context, vendorID, itemID string, date time.Time, soldOut bool) (*Supply, error) {
	item, err := s.Items.GetByID(ctx, itemID)
	if err != nil {
		return nil, err
	}
	if item.VendorID != vendorID {
		return nil, menu.ErrForbidden
	}
	if err := s.Supplies.SetSoldOut(ctx, itemID, date, soldOut); err != nil {
		return nil, err
	}
	return s.Supplies.Get(ctx, itemID, date)
}

// GetForItem returns the supply for a vendor's item on a given day.
// Returns menu.ErrForbidden if the item does not belong to the vendor.
func (s *Service) GetForItem(ctx context.Context, vendorID, itemID string, date time.Time) (*Supply, error) {
	item, err := s.Items.GetByID(ctx, itemID)
	if err != nil {
		return nil, err
	}
	if item.VendorID != vendorID {
		return nil, menu.ErrForbidden
	}
	return s.Supplies.Get(ctx, itemID, date)
}

// ListForVendor returns all supplies for a vendor on a given day.
func (s *Service) ListForVendor(ctx context.Context, vendorID string, date time.Time) ([]*Supply, error) {
	return s.Supplies.ListByVendor(ctx, vendorID, date)
}
