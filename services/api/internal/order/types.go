package order

import "time"

type Status string

const (
	StatusDraft     Status = "draft"
	StatusPlaced    Status = "placed"
	StatusCutoff    Status = "cutoff"
	StatusCancelled Status = "cancelled"
	StatusReady     Status = "ready"     // reserved, P4
	StatusPickedUp  Status = "picked_up" // reserved, P4
	StatusNoShow    Status = "no_show"   // reserved, P4
	StatusRefunded  Status = "refunded"  // reserved, P4
)

type Order struct {
	ID              string
	OrderNumber     int64
	UserID          string
	VendorID        string
	Plant           string
	SupplyDate      time.Time
	Status          Status
	TotalPriceMinor int64
	Notes           string
	TOTPSecret      []byte
	PlacedAt        *time.Time
	CutoffAt        time.Time
	ReadyAt         *time.Time
	PickedUpAt      *time.Time
	NoShowAt        *time.Time
	CancelledAt     *time.Time
	CreatedAt       time.Time
	UpdatedAt       time.Time
	Items           []Item
}

type Item struct {
	ID             string
	OrderID        string
	MenuItemID     string
	Name           string // menu item display name, populated on read via JOIN; empty if menu item was deleted
	Qty            int
	UnitPriceMinor int64
}

type StateEvent struct {
	ID        int64
	OrderID   string
	FromState *Status
	ToState   Status
	ActorID   *string
	ActorRole *string
	Reason    string
	Payload   map[string]any
	At        time.Time
}
