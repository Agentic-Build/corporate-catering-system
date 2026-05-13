package menu

import "time"

type ItemStatus string

const (
	ItemStatusDraft    ItemStatus = "draft"
	ItemStatusActive   ItemStatus = "active"
	ItemStatusArchived ItemStatus = "archived"
)

type Category struct {
	ID        string
	VendorID  string
	Name      string
	SortOrder int
	CreatedAt time.Time
}

type Item struct {
	ID          string
	VendorID    string
	CategoryID  *string
	Name        string
	Description string
	PriceMinor  int64
	Tags        []string
	Badges      []string
	Status      ItemStatus
	ArchivedAt  *time.Time
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

type Image struct {
	ID        string
	ItemID    string
	BlobURI   string
	Alt       string
	SortOrder int
	CreatedAt time.Time
}

// ActiveItemRow is the join result returned by ListActiveByPlant.
// Captures item + vendor display name + supply fields the employee menu view needs.
type ActiveItemRow struct {
	Item         Item
	VendorName   string
	SupplyDate   time.Time
	Capacity     int
	Remain       int
	PickupWindow string
	ETALabel     string
	CutoffAt     time.Time
}
