package ohttp

import (
	"context"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/Agentic-Build/corporate-catering-system/services/api/internal/httpserver"
	"github.com/Agentic-Build/corporate-catering-system/services/api/internal/order"
)

const dateLayout = "2006-01-02"

type markReadyInput struct {
	Body struct {
		OrderIDs []string `json:"order_ids" minItems:"1"`
	}
}

type listMerchantOrdersInput struct {
	Date   string `query:"date"`
	Status string `query:"status" enum:"placed,cutoff,ready,picked_up,no_show,cancelled,"`
	Plant  string `query:"plant"`
}

type merchantOrderDTO struct {
	ID              string         `json:"id"`
	OrderNumber     int64          `json:"order_number"`
	Plant           string         `json:"plant"`
	Status          string         `json:"status"`
	TotalPriceMinor int64          `json:"total_price_minor"`
	Notes           string         `json:"notes"`
	PlacedAt        *string        `json:"placed_at,omitempty"`
	ReadyAt         *string        `json:"ready_at,omitempty"`
	PickedUpAt      *string        `json:"picked_up_at,omitempty"`
	Items           []orderItemDTO `json:"items"`
}

type listMerchantOrdersOutput struct {
	Body struct {
		Date  string             `json:"date"`
		Items []merchantOrderDTO `json:"items"`
	}
}

type prepSheetItemDTO struct {
	MenuItemID string `json:"menu_item_id"`
	Name       string `json:"name"`
	Qty        int    `json:"qty"`
}

type prepSheetOrderDTO struct {
	OrderID         string             `json:"order_id"`
	OrderNumber     int64              `json:"order_number"`
	TotalPriceMinor int64              `json:"total_price_minor"`
	Notes           string             `json:"notes"`
	Items           []prepSheetItemDTO `json:"items"`
}

type prepSheetPlantDTO struct {
	Plant        string              `json:"plant"`
	OrderCount   int                 `json:"order_count"`
	PortionCount int                 `json:"portion_count"`
	Items        []prepSheetItemDTO  `json:"items"`
	Orders       []prepSheetOrderDTO `json:"orders"`
}

type prepSheetInput struct {
	Date string `query:"date" doc:"YYYY-MM-DD; defaults to today UTC"`
}

type prepSheetOutput struct {
	Body struct {
		Date          string              `json:"date"`
		TotalOrders   int                 `json:"total_orders"`
		TotalPortions int                 `json:"total_portions"`
		Plants        []prepSheetPlantDTO `json:"plants"`
	}
}

func toMerchantDTO(o *order.Order) merchantOrderDTO {
	d := merchantOrderDTO{
		ID:              o.ID,
		OrderNumber:     o.OrderNumber,
		Plant:           o.Plant,
		Status:          string(o.Status),
		TotalPriceMinor: o.TotalPriceMinor,
		Notes:           o.Notes,
		Items:           make([]orderItemDTO, 0, len(o.Items)),
	}
	d.PlacedAt = httpserver.FormatNullableTimePtr(o.PlacedAt)
	d.ReadyAt = httpserver.FormatNullableTimePtr(o.ReadyAt)
	d.PickedUpAt = httpserver.FormatNullableTimePtr(o.PickedUpAt)
	for _, it := range o.Items {
		d.Items = append(d.Items, orderItemDTO{
			ID:             it.ID,
			MenuItemID:     it.MenuItemID,
			Name:           it.Name,
			Qty:            it.Qty,
			UnitPriceMinor: it.UnitPriceMinor,
		})
	}
	return d
}

func prepItemDTOs(items []order.PrepSheetItem) []prepSheetItemDTO {
	out := make([]prepSheetItemDTO, 0, len(items))
	for _, it := range items {
		out = append(out, prepSheetItemDTO{MenuItemID: it.MenuItemID, Name: it.Name, Qty: it.Qty})
	}
	return out
}

func (a *API) markReady(ctx context.Context, in *markReadyInput) (*struct{}, error) {
	u, vendorID, err := a.requireVendor(ctx)
	if err != nil {
		return nil, err
	}
	if err := a.Svc.MarkReady(ctx, vendorID, in.Body.OrderIDs, u.ID); err != nil {
		return nil, mapErr(err)
	}
	return &struct{}{}, nil
}

func (a *API) listMerchantOrders(ctx context.Context, in *listMerchantOrdersInput) (*listMerchantOrdersOutput, error) {
	_, vendorID, err := a.requireVendor(ctx)
	if err != nil {
		return nil, err
	}
	day := time.Now().UTC().Truncate(24 * time.Hour)
	if in.Date != "" {
		d, perr := time.Parse(dateLayout, in.Date)
		if perr != nil {
			return nil, huma.Error400BadRequest("invalid date")
		}
		day = d.UTC()
	}
	var statuses []order.Status
	if in.Status != "" {
		statuses = []order.Status{order.Status(in.Status)}
	}
	orders, err := a.Svc.ListByVendorDay(ctx, vendorID, day, statuses)
	if err != nil {
		return nil, mapErr(err)
	}
	var resp listMerchantOrdersOutput
	resp.Body.Date = day.Format(dateLayout)
	resp.Body.Items = make([]merchantOrderDTO, 0, len(orders))
	for _, o := range orders {
		if in.Plant != "" && o.Plant != in.Plant {
			continue
		}
		resp.Body.Items = append(resp.Body.Items, toMerchantDTO(o))
	}
	return &resp, nil
}

func (a *API) prepSheet(ctx context.Context, in *prepSheetInput) (*prepSheetOutput, error) {
	_, vendorID, err := a.requireVendor(ctx)
	if err != nil {
		return nil, err
	}
	day := time.Now().UTC().Truncate(24 * time.Hour)
	if in.Date != "" {
		d, perr := time.Parse(dateLayout, in.Date)
		if perr != nil {
			return nil, huma.Error400BadRequest("invalid date (want YYYY-MM-DD)")
		}
		day = d.UTC()
	}
	sheet, err := a.Svc.PrepSheet(ctx, vendorID, day)
	if err != nil {
		return nil, mapErr(err)
	}
	var resp prepSheetOutput
	resp.Body.Date = sheet.Date.Format(dateLayout)
	resp.Body.TotalOrders = sheet.TotalOrders
	resp.Body.TotalPortions = sheet.TotalPortions
	resp.Body.Plants = make([]prepSheetPlantDTO, 0, len(sheet.Plants))
	for _, p := range sheet.Plants {
		pd := prepSheetPlantDTO{
			Plant:        p.Plant,
			OrderCount:   p.OrderCount,
			PortionCount: p.PortionCount,
			Items:        prepItemDTOs(p.Items),
			Orders:       make([]prepSheetOrderDTO, 0, len(p.Orders)),
		}
		for _, o := range p.Orders {
			pd.Orders = append(pd.Orders, prepSheetOrderDTO{
				OrderID:         o.OrderID,
				OrderNumber:     o.OrderNumber,
				TotalPriceMinor: o.TotalPriceMinor,
				Notes:           o.Notes,
				Items:           prepItemDTOs(o.Items),
			})
		}
		resp.Body.Plants = append(resp.Body.Plants, pd)
	}
	return &resp, nil
}
