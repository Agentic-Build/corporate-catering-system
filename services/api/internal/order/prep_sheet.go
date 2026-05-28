package order

import (
	"context"
	"time"
)

// PrepSheetItem is an aggregated or per-order line item.
type PrepSheetItem struct {
	MenuItemID string
	Name       string
	Qty        int
}

// PrepSheetOrder is one order's label data — what to portion into a basket.
type PrepSheetOrder struct {
	OrderID         string
	OrderNumber     int64
	TotalPriceMinor int64
	Notes           string
	Items           []PrepSheetItem
}

// PrepSheetPlant is the per-plant section: an aggregated item breakdown
// (分區表) plus the individual orders for that plant (配送籃清單).
type PrepSheetPlant struct {
	Plant        string
	OrderCount   int
	PortionCount int
	Items        []PrepSheetItem
	Orders       []PrepSheetOrder
}

// PrepSheet is the whole-day prep & delivery output for one vendor.
type PrepSheet struct {
	Date          time.Time
	VendorID      string
	TotalOrders   int
	TotalPortions int
	Plants        []PrepSheetPlant
}

// prepNameOf returns the merchant-facing display name for menu_item_id, with a
// short-id fallback when names lacks an entry.
func prepNameOf(names map[string]string, id string) string {
	if n, ok := names[id]; ok && n != "" {
		return n
	}
	if len(id) > 8 {
		return id[:8]
	}
	return id
}

type prepPlantAcc struct {
	orders   []PrepSheetOrder
	itemQty  map[string]int
	itemSeen []string
}

// groupOrdersByPlant rolls each order into a per-plant accumulator (order list +
// first-seen item ordering + per-item totals). Plant insertion order is preserved.
func groupOrdersByPlant(orders []*Order, names map[string]string) (map[string]*prepPlantAcc, []string) {
	byPlant := map[string]*prepPlantAcc{}
	var plantOrder []string
	for _, o := range orders {
		pa, ok := byPlant[o.Plant]
		if !ok {
			pa = &prepPlantAcc{itemQty: map[string]int{}}
			byPlant[o.Plant] = pa
			plantOrder = append(plantOrder, o.Plant)
		}
		pso := PrepSheetOrder{OrderID: o.ID, OrderNumber: o.OrderNumber, TotalPriceMinor: o.TotalPriceMinor, Notes: o.Notes}
		for _, it := range o.Items {
			pso.Items = append(pso.Items, PrepSheetItem{
				MenuItemID: it.MenuItemID, Name: prepNameOf(names, it.MenuItemID), Qty: it.Qty,
			})
			if _, seen := pa.itemQty[it.MenuItemID]; !seen {
				pa.itemSeen = append(pa.itemSeen, it.MenuItemID)
			}
			pa.itemQty[it.MenuItemID] += it.Qty
		}
		pa.orders = append(pa.orders, pso)
	}
	return byPlant, plantOrder
}

// assemblePrepSheet groups a vendor's orders for a day into the per-plant
// breakdown, per-order labels, and basket lists the merchant needs to portion
// and dispatch. names maps menu_item_id to a display name; an unknown id
// falls back to a short id. Plants and items keep first-seen order so the
// printed sheet is stable.
func assemblePrepSheet(date time.Time, vendorID string, orders []*Order, names map[string]string) *PrepSheet {
	byPlant, plantOrder := groupOrdersByPlant(orders, names)
	sheet := &PrepSheet{Date: date, VendorID: vendorID, TotalOrders: len(orders)}
	for _, plant := range plantOrder {
		pa := byPlant[plant]
		pp := PrepSheetPlant{Plant: plant, OrderCount: len(pa.orders), Orders: pa.orders}
		for _, id := range pa.itemSeen {
			qty := pa.itemQty[id]
			pp.Items = append(pp.Items, PrepSheetItem{MenuItemID: id, Name: prepNameOf(names, id), Qty: qty})
			pp.PortionCount += qty
		}
		sheet.TotalPortions += pp.PortionCount
		sheet.Plants = append(sheet.Plants, pp)
	}
	return sheet
}

// prepSheetStatuses are the order states that still need a meal made and
// delivered — cancelled / no-show / picked-up / refunded are excluded.
var prepSheetStatuses = []Status{StatusPlaced, StatusCutoff, StatusReady}

// PrepSheet aggregates the vendor's still-to-fulfil orders on date into the
// per-plant prep & delivery output.
func (s *Service) PrepSheet(ctx context.Context, vendorID string, date time.Time) (*PrepSheet, error) {
	orders, err := s.Orders.ListByVendorDay(ctx, vendorID, date, prepSheetStatuses)
	if err != nil {
		return nil, err
	}
	names := map[string]string{}
	for _, o := range orders {
		for _, it := range o.Items {
			if _, ok := names[it.MenuItemID]; ok {
				continue
			}
			mi, err := s.Items.GetByID(ctx, it.MenuItemID)
			if err != nil {
				return nil, err
			}
			names[it.MenuItemID] = mi.Name
		}
	}
	return assemblePrepSheet(date, vendorID, orders, names), nil
}
