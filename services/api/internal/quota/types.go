package quota

import "time"

type Supply struct {
	ID           string
	MenuItemID   string
	SupplyDate   time.Time
	Capacity     int
	Remain       int
	PickupWindow string
	ETALabel     string
	CutoffAt     time.Time
	CreatedAt    time.Time
	UpdatedAt    time.Time
}
