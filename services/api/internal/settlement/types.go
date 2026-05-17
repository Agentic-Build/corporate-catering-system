package settlement

import "time"

// Status is the vendor_settlement lifecycle. A settlement is born `closed`
// (it records a finalised period) and can only move to `void` for correction.
type Status string

const (
	StatusClosed Status = "closed"
	StatusVoid   Status = "void"
)

// Settlement is one finalised per-vendor payable for a period. It is a snapshot:
// gross_minor / order_count / portion_count / order_ids are frozen at close time
// so later order mutations cannot retroactively change a closed settlement.
type Settlement struct {
	ID           string
	VendorID     string
	PeriodStart  time.Time
	PeriodEnd    time.Time
	OrderCount   int
	PortionCount int
	GrossMinor   int64
	OrderIDs     []string
	Status       Status
	ClosedAt     time.Time
	ClosedBy     *string
	CreatedAt    time.Time
}

// SettlementOrderLine is one order's contribution inside a settlement detail
// view, expanded from Settlement.OrderIDs.
type SettlementOrderLine struct {
	OrderID         string
	SupplyDate      time.Time
	Status          string
	TotalPriceMinor int64
	PortionCount    int
}

// VendorAggregate is the picked_up/no_show roll-up for one vendor over a period,
// computed live from the order table. It is the raw material both CloseSettlement
// and Reconciliation work from.
type VendorAggregate struct {
	VendorID     string
	OrderCount   int
	PortionCount int
	GrossMinor   int64
	OrderIDs     []string
}

// StatusBreakdown counts orders by lifecycle status over a period. picked_up and
// no_show feed the payable; cancelled and refunded are shown for transparency.
type StatusBreakdown struct {
	PickedUp  int `json:"picked_up"`
	NoShow    int `json:"no_show"`
	Cancelled int `json:"cancelled"`
	Refunded  int `json:"refunded"`
}

// Reconciliation is the live, unclosed monthly summary a merchant sees before a
// settlement is cut. Numbers are derived from the order table on every request.
type Reconciliation struct {
	VendorID     string
	PeriodStart  time.Time
	PeriodEnd    time.Time
	OrderCount   int
	PortionCount int
	GrossMinor   int64
	Breakdown    StatusBreakdown
}
