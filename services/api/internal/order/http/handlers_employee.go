package ohttp

import (
	"context"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/Agentic-Build/corporate-catering-system/services/api/internal/order"
)

type placeOrderInput struct {
	Body struct {
		Plant      string `json:"plant"`
		SupplyDate string `json:"supply_date"` // YYYY-MM-DD
		Notes      string `json:"notes,omitempty" maxLength:"500" doc:"Free-text special requirements shown on the merchant prep board"`
		Items      []struct {
			MenuItemID string `json:"menu_item_id" format:"uuid"`
			Qty        int    `json:"qty" minimum:"1"`
		} `json:"items"`
	}
}

type placeOrderOutput struct {
	Body struct {
		Order orderDTO `json:"order"`
	}
}

type listOrdersOutput struct {
	Body struct {
		Items []orderDTO `json:"items"`
	}
}

type orderIDInput struct {
	ID string `path:"id" format:"uuid"`
}

type modifyOrderInput struct {
	ID   string `path:"id" format:"uuid"`
	Body struct {
		Notes string `json:"notes,omitempty" maxLength:"500" doc:"Free-text special requirements shown on the merchant prep board"`
		Items []struct {
			MenuItemID string `json:"menu_item_id" format:"uuid"`
			Qty        int    `json:"qty" minimum:"1"`
		} `json:"items"`
	}
}

type orderOutput struct {
	Body struct {
		Order orderDTO `json:"order"`
	}
}

func (a *API) place(ctx context.Context, in *placeOrderInput) (*placeOrderOutput, error) {
	u, err := a.requireEmployee(ctx)
	if err != nil {
		return nil, err
	}
	if len(in.Body.Items) == 0 {
		return nil, huma.Error400BadRequest("items required")
	}
	day, err := time.Parse("2006-01-02", in.Body.SupplyDate)
	if err != nil {
		return nil, huma.Error400BadRequest("invalid supply_date (YYYY-MM-DD)")
	}
	items := make([]order.PlaceItem, 0, len(in.Body.Items))
	for _, it := range in.Body.Items {
		items = append(items, order.PlaceItem{MenuItemID: it.MenuItemID, Qty: it.Qty})
	}
	o, err := a.Svc.Place(ctx, order.PlaceOrderInput{
		UserID:     u.ID,
		Plant:      in.Body.Plant,
		SupplyDate: day,
		Items:      items,
		Notes:      in.Body.Notes,
	})
	if err != nil {
		return nil, mapErr(err)
	}
	var resp placeOrderOutput
	resp.Body.Order = toDTO(o)
	return &resp, nil
}

func (a *API) list(ctx context.Context, _ *struct{}) (*listOrdersOutput, error) {
	u, err := a.requireEmployee(ctx)
	if err != nil {
		return nil, err
	}
	orders, err := a.Svc.ListByUser(ctx, u.ID)
	if err != nil {
		return nil, mapErr(err)
	}
	var resp listOrdersOutput
	resp.Body.Items = make([]orderDTO, 0, len(orders))
	for _, o := range orders {
		resp.Body.Items = append(resp.Body.Items, toDTO(o))
	}
	return &resp, nil
}

func (a *API) get(ctx context.Context, in *orderIDInput) (*orderOutput, error) {
	u, err := a.requireEmployee(ctx)
	if err != nil {
		return nil, err
	}
	o, err := a.Svc.Get(ctx, in.ID, u.ID)
	if err != nil {
		return nil, mapErr(err)
	}
	var resp orderOutput
	resp.Body.Order = toDTO(o)
	return &resp, nil
}

func (a *API) modify(ctx context.Context, in *modifyOrderInput) (*orderOutput, error) {
	u, err := a.requireEmployee(ctx)
	if err != nil {
		return nil, err
	}
	if len(in.Body.Items) == 0 {
		return nil, huma.Error400BadRequest("items required")
	}
	items := make([]order.PlaceItem, 0, len(in.Body.Items))
	for _, it := range in.Body.Items {
		items = append(items, order.PlaceItem{MenuItemID: it.MenuItemID, Qty: it.Qty})
	}
	o, err := a.Svc.Modify(ctx, order.ModifyOrderInput{
		OrderID: in.ID,
		UserID:  u.ID,
		Items:   items,
		Notes:   in.Body.Notes,
	})
	if err != nil {
		return nil, mapErr(err)
	}
	var resp orderOutput
	resp.Body.Order = toDTO(o)
	return &resp, nil
}

func (a *API) cancel(ctx context.Context, in *orderIDInput) (*struct{}, error) {
	u, err := a.requireEmployee(ctx)
	if err != nil {
		return nil, err
	}
	if err := a.Svc.Cancel(ctx, in.ID, u.ID); err != nil {
		return nil, mapErr(err)
	}
	return &struct{}{}, nil
}

func (a *API) pickup(ctx context.Context, in *orderIDInput) (*struct{}, error) {
	u, err := a.requireEmployee(ctx)
	if err != nil {
		return nil, err
	}
	if err := a.Svc.Pickup(ctx, in.ID, u.ID); err != nil {
		return nil, mapErr(err)
	}
	return &struct{}{}, nil
}
