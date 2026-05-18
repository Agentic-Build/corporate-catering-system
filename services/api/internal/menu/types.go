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

// EmployeeMenuSort enumerates the supported sort orders for the employee menu.
// An empty value preserves the historical default ordering (vendor, then name).
type EmployeeMenuSort string

const (
	EmployeeMenuSortDefault   EmployeeMenuSort = ""
	EmployeeMenuSortName      EmployeeMenuSort = "name"
	EmployeeMenuSortPriceAsc  EmployeeMenuSort = "price_asc"
	EmployeeMenuSortPriceDesc EmployeeMenuSort = "price_desc"
	EmployeeMenuSortRemain    EmployeeMenuSort = "remain"
)

// EmployeeMenuFilter carries the plant/day selectors plus the optional
// search/filter/sort criteria for the employee menu listing. Every optional
// field has a zero value meaning "not supplied"; a zero-valued filter (apart
// from Plant/Day) yields behaviour identical to the unfiltered listing.
type EmployeeMenuFilter struct {
	Plant string
	Day   time.Time

	Q        string           // keyword matched against name/description
	Tags     []string         // health tags; item matches if it has ANY of these
	PriceMin *int64           // inclusive lower price bound (minor units)
	PriceMax *int64           // inclusive upper price bound (minor units)
	InStock  *bool            // when true, exclude sold-out items
	Sort     EmployeeMenuSort // result ordering
}

// ActiveItemRow is the join result returned by ListActiveByPlant.
// Captures item + vendor display name + supply fields the employee menu view needs.
type ActiveItemRow struct {
	Item         Item
	VendorName   string
	SupplyDate   time.Time
	Capacity     int
	Remain       int
	SoldOut      bool
	PickupWindow string
	ETALabel     string
	CutoffAt     time.Time
}

// MerchantItemRow is the join result returned by ListByVendor. It augments a
// menu item with read-only usage stats for the merchant meal-library view:
// LastUsed is the most recent meal_supply.supply_date (nil if never scheduled),
// TotalSold is the cumulative order_item.qty over picked-up orders (0 if none).
type MerchantItemRow struct {
	Item      Item
	LastUsed  *time.Time
	TotalSold int
}
