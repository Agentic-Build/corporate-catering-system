package quota

import (
	"context"
	"time"
)

type Repository interface {
	Upsert(ctx context.Context, s *Supply) error
	Get(ctx context.Context, menuItemID string, date time.Time) (*Supply, error)
	ListByVendor(ctx context.Context, vendorID string, date time.Time) ([]*Supply, error)
	Decrement(ctx context.Context, menuItemID string, date time.Time, n int) (int, error)
	Restore(ctx context.Context, menuItemID string, date time.Time, n int) error
	// SetSoldOut flips the temporary sold-out flag on an existing supply row.
	SetSoldOut(ctx context.Context, menuItemID string, date time.Time, soldOut bool) error
}
