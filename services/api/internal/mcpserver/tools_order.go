// Package mcpserver — order tools.
//
// Exposes employee-facing order operations as MCP tools. Each handler:
//   1. Authenticates via context (idhttp.AuthMiddleware populated it).
//   2. Enforces the same role gate the HTTP handler uses (employee-only).
//   3. Delegates to order.Service so business rules stay shared.
//   4. Returns JSON-encoded results (or mcp.NewToolResultError on failure).
package mcpserver

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"github.com/takalawang/corporate-catering-system/services/api/internal/identity"
	"github.com/takalawang/corporate-catering-system/services/api/internal/order"
)

func registerOrderTools(s *server.MCPServer, deps Deps) {
	// -------- order.list_mine --------
	s.AddTool(
		mcp.NewTool("order.list_mine",
			mcp.WithDescription("List the authenticated employee's recent orders (last 30 days)"),
			annoReadOnly(),
		),
		func(ctx context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			u, ok := userFromCtx(ctx)
			if !ok {
				return mcp.NewToolResultError("not authenticated"), nil
			}
			if u.Role != identity.RoleEmployee {
				return mcp.NewToolResultError(fmt.Sprintf("role %s cannot list employee orders", u.Role)), nil
			}
			if deps.Order == nil {
				return mcp.NewToolResultError("order service not configured"), nil
			}
			orders, err := deps.Order.ListByUser(ctx, u.ID)
			if err != nil {
				return mcp.NewToolResultError(fmt.Sprintf("list orders: %v", err)), nil
			}
			auditAfter(ctx, deps, "order.list_mine", "order", "list", nil, u)
			data, _ := json.Marshal(map[string]any{
				"count":  len(orders),
				"orders": orders,
			})
			return mcp.NewToolResultText(string(data)), nil
		},
	)

	// -------- order.get --------
	s.AddTool(
		mcp.NewTool("order.get",
			mcp.WithDescription("Get an order by ID (owner only)"),
			mcp.WithString("order_id",
				mcp.Required(),
				mcp.Description("UUID of the order to fetch"),
			),
			annoReadOnly(),
		),
		func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			u, ok := userFromCtx(ctx)
			if !ok {
				return mcp.NewToolResultError("not authenticated"), nil
			}
			if u.Role != identity.RoleEmployee {
				return mcp.NewToolResultError("only employee can use this tool"), nil
			}
			if deps.Order == nil {
				return mcp.NewToolResultError("order service not configured"), nil
			}
			orderID, err := req.RequireString("order_id")
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			o, err := deps.Order.Get(ctx, orderID, u.ID)
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			auditAfter(ctx, deps, "order.get", "order", o.ID, nil, u)
			data, _ := json.Marshal(o)
			return mcp.NewToolResultText(string(data)), nil
		},
	)

	// -------- order.place --------
	s.AddTool(
		mcp.NewTool("order.place",
			mcp.WithDescription("Place a new order for the authenticated employee"),
			mcp.WithString("plant",
				mcp.Required(),
				mcp.Description("Plant code, e.g. F12B-3F"),
			),
			mcp.WithString("supply_date",
				mcp.Required(),
				mcp.Description("Supply date in YYYY-MM-DD"),
			),
			mcp.WithArray("items",
				mcp.Required(),
				mcp.Description("Array of {menu_item_id: UUID, qty: int>=1}"),
				mcp.Items(map[string]any{
					"type": "object",
					"properties": map[string]any{
						"menu_item_id": map[string]any{"type": "string"},
						"qty":          map[string]any{"type": "integer", "minimum": 1},
					},
					"required": []string{"menu_item_id", "qty"},
				}),
			),
			mcp.WithString("notes",
				mcp.Description("Optional free-text special requirements shown on the merchant prep board"),
			),
			annoCreate(),
		),
		func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			u, ok := userFromCtx(ctx)
			if !ok {
				return mcp.NewToolResultError("not authenticated"), nil
			}
			if u.Role != identity.RoleEmployee {
				return mcp.NewToolResultError("only employee can place orders"), nil
			}
			if deps.Order == nil {
				return mcp.NewToolResultError("order service not configured"), nil
			}
			plant, err := req.RequireString("plant")
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			dayStr, err := req.RequireString("supply_date")
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			day, err := time.Parse("2006-01-02", dayStr)
			if err != nil {
				return mcp.NewToolResultError("invalid supply_date (YYYY-MM-DD)"), nil
			}
			// mcp-go v0.53 has no RequireSlice for arrays of objects; pull from
			// GetArguments() directly. JSON arrays decode as []any whose entries
			// are map[string]any with JSON numbers arriving as float64.
			args := req.GetArguments()
			rawItems, ok := args["items"].([]any)
			if !ok || len(rawItems) == 0 {
				return mcp.NewToolResultError("items required (non-empty array)"), nil
			}
			items := make([]order.PlaceItem, 0, len(rawItems))
			for _, raw := range rawItems {
				m, ok := raw.(map[string]any)
				if !ok {
					return mcp.NewToolResultError("items entry must be an object"), nil
				}
				id, _ := m["menu_item_id"].(string)
				qty, _ := m["qty"].(float64)
				items = append(items, order.PlaceItem{MenuItemID: id, Qty: int(qty)})
			}
			notes, _ := args["notes"].(string)
			o, err := deps.Order.Place(ctx, order.PlaceOrderInput{
				UserID:     u.ID,
				Plant:      plant,
				SupplyDate: day,
				Items:      items,
				Notes:      notes,
			})
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			auditAfter(ctx, deps, "order.place", "order", o.ID, map[string]any{
				"plant":       plant,
				"supply_date": dayStr,
				"items_count": len(items),
			}, u)
			data, _ := json.Marshal(o)
			return mcp.NewToolResultText(string(data)), nil
		},
	)

	// -------- order.cancel --------
	s.AddTool(
		mcp.NewTool("order.cancel",
			mcp.WithDescription("Cancel an order owned by the authenticated employee"),
			mcp.WithString("order_id",
				mcp.Required(),
				mcp.Description("UUID of the order to cancel"),
			),
			annoReversible(),
		),
		func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			u, ok := userFromCtx(ctx)
			if !ok {
				return mcp.NewToolResultError("not authenticated"), nil
			}
			if u.Role != identity.RoleEmployee {
				return mcp.NewToolResultError("only employee can cancel orders"), nil
			}
			if deps.Order == nil {
				return mcp.NewToolResultError("order service not configured"), nil
			}
			orderID, err := req.RequireString("order_id")
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			if err := deps.Order.Cancel(ctx, orderID, u.ID); err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			auditAfter(ctx, deps, "order.cancel", "order", orderID, nil, u)
			return mcp.NewToolResultText(`{"status":"cancelled"}`), nil
		},
	)

	// -------- order.modify --------
	s.AddTool(
		mcp.NewTool("order.modify",
			mcp.WithDescription("Replace the items of a PLACED order owned by the authenticated employee (before cutoff)"),
			mcp.WithString("order_id",
				mcp.Required(),
				mcp.Description("UUID of the order to modify"),
			),
			mcp.WithArray("items",
				mcp.Required(),
				mcp.Description("New full item set: array of {menu_item_id: UUID, qty: int>=1}"),
				mcp.Items(map[string]any{
					"type": "object",
					"properties": map[string]any{
						"menu_item_id": map[string]any{"type": "string"},
						"qty":          map[string]any{"type": "integer", "minimum": 1},
					},
					"required": []string{"menu_item_id", "qty"},
				}),
			),
			mcp.WithString("notes",
				mcp.Description("Optional free-text special requirements; replaces the order's existing note"),
			),
			annoStateChange(),
		),
		func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			u, ok := userFromCtx(ctx)
			if !ok {
				return mcp.NewToolResultError("not authenticated"), nil
			}
			if u.Role != identity.RoleEmployee {
				return mcp.NewToolResultError("only employee can modify orders"), nil
			}
			if deps.Order == nil {
				return mcp.NewToolResultError("order service not configured"), nil
			}
			orderID, err := req.RequireString("order_id")
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			args := req.GetArguments()
			rawItems, ok := args["items"].([]any)
			if !ok || len(rawItems) == 0 {
				return mcp.NewToolResultError("items required (non-empty array)"), nil
			}
			items := make([]order.PlaceItem, 0, len(rawItems))
			for _, raw := range rawItems {
				m, ok := raw.(map[string]any)
				if !ok {
					return mcp.NewToolResultError("items entry must be an object"), nil
				}
				id, _ := m["menu_item_id"].(string)
				qty, _ := m["qty"].(float64)
				items = append(items, order.PlaceItem{MenuItemID: id, Qty: int(qty)})
			}
			notes, _ := args["notes"].(string)
			o, err := deps.Order.Modify(ctx, order.ModifyOrderInput{
				OrderID: orderID,
				UserID:  u.ID,
				Items:   items,
				Notes:   notes,
			})
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			auditAfter(ctx, deps, "order.modify", "order", o.ID, map[string]any{
				"items_count": len(items),
			}, u)
			data, _ := json.Marshal(o)
			return mcp.NewToolResultText(string(data)), nil
		},
	)
}
