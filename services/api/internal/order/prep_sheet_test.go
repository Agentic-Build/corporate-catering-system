package order

import (
	"testing"
	"time"
)

func TestAssemblePrepSheet(t *testing.T) {
	date := time.Date(2026, 5, 14, 0, 0, 0, 0, time.UTC)
	names := map[string]string{"item-a": "雞腿便當", "item-b": "排骨便當"}
	orders := []*Order{
		{
			ID: "o1", Plant: "F12B-3F", TotalPriceMinor: 220, Notes: "不要辣",
			Items: []Item{{MenuItemID: "item-a", Qty: 2}},
		},
		{
			ID: "o2", Plant: "F12B-3F", TotalPriceMinor: 310,
			Items: []Item{{MenuItemID: "item-a", Qty: 1}, {MenuItemID: "item-b", Qty: 1}},
		},
		{
			ID: "o3", Plant: "F15-2F", TotalPriceMinor: 200,
			Items: []Item{{MenuItemID: "item-b", Qty: 1}},
		},
	}

	sheet := assemblePrepSheet(date, "v1", orders, names)

	if sheet.TotalOrders != 3 {
		t.Fatalf("TotalOrders = %d, want 3", sheet.TotalOrders)
	}
	if sheet.TotalPortions != 5 { // 2+1+1 + 1
		t.Fatalf("TotalPortions = %d, want 5", sheet.TotalPortions)
	}
	if len(sheet.Plants) != 2 {
		t.Fatalf("plants = %d, want 2", len(sheet.Plants))
	}

	// First plant: F12B-3F — 2 orders, 4 portions, item-a aggregated to 3.
	p0 := sheet.Plants[0]
	if p0.Plant != "F12B-3F" || p0.OrderCount != 2 || p0.PortionCount != 4 {
		t.Fatalf("plant0 = %+v", p0)
	}
	var itemAQty int
	for _, it := range p0.Items {
		if it.MenuItemID == "item-a" {
			itemAQty = it.Qty
			if it.Name != "雞腿便當" {
				t.Errorf("item-a name = %q", it.Name)
			}
		}
	}
	if itemAQty != 3 {
		t.Errorf("aggregated item-a qty = %d, want 3", itemAQty)
	}
	// Per-order label data carries notes.
	if p0.Orders[0].OrderID != "o1" || p0.Orders[0].Notes != "不要辣" {
		t.Errorf("order label 0 = %+v", p0.Orders[0])
	}

	// Unknown menu item falls back to a short id rather than panicking.
	bare := assemblePrepSheet(date, "v1", []*Order{
		{ID: "o9", Plant: "P", Items: []Item{{MenuItemID: "0123456789abcdef", Qty: 1}}},
	}, nil)
	if got := bare.Plants[0].Items[0].Name; got != "01234567" {
		t.Errorf("fallback name = %q, want 01234567", got)
	}
}
