// Package mcpserver — menu/vendor discovery tools.
//
// Employees use these tools to browse and search the daily menu before
// placing an order through the order.* tools. Every handler:
//  1. Authenticates via context (set by idhttp.AuthMiddleware or stdio bootstrap).
//  2. Enforces employee-only access (welfare_admin is allowed to read too).
//  3. Delegates to menu.Service / vendor.Service so business rules stay shared.
//  4. Returns compact JSON results suitable for LLM tool-use.
package mcpserver

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"github.com/takalawang/corporate-catering-system/services/api/internal/identity"
	"github.com/takalawang/corporate-catering-system/services/api/internal/menu"
	vendor "github.com/takalawang/corporate-catering-system/services/api/internal/vendors"
)

// canReadMenu returns true when the role can read employee-facing menus.
// Employees use this every day; admins occasionally use it for QA/support.
func canReadMenu(r identity.Role) bool {
	return r == identity.RoleEmployee || r == identity.RoleWelfareAdmin
}

// resolvePlant takes an explicit plant argument and falls back to the user's
// home plant when omitted. Returns an empty string when neither is available
// so the caller can return a clear error.
func resolvePlant(arg string, u *identity.User) string {
	if arg != "" {
		return arg
	}
	if u != nil && u.Plant != nil {
		return *u.Plant
	}
	return ""
}

func registerMenuTools(s *server.MCPServer, deps Deps) {
	// -------- menu.list_for_day --------
	s.AddTool(
		mcp.NewTool("menu.list_for_day",
			mcp.WithDescription("List available menu items for the caller's plant on a given supply date. Returns vendor name, price, capacity, sold-out flag, and pickup window."),
			mcp.WithString("supply_date",
				mcp.Description("Supply date YYYY-MM-DD. Defaults to today in server timezone."),
			),
			mcp.WithString("plant",
				mcp.Description("Plant code (e.g. F12B-3F). Defaults to caller's home plant."),
			),
			annoReadOnly(),
		),
		func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			u, ok := userFromCtx(ctx)
			if !ok {
				return mcp.NewToolResultError("not authenticated"), nil
			}
			if !canReadMenu(u.Role) {
				return mcp.NewToolResultError(fmt.Sprintf("role %s cannot read menu", u.Role)), nil
			}
			args := req.GetArguments()
			plant := resolvePlant(stringArg(args, "plant"), u)
			if plant == "" {
				return mcp.NewToolResultError("plant required (no home plant on user)"), nil
			}
			day, err := parseDayOrToday(stringArg(args, "supply_date"))
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			if deps.Menu == nil {
				return mcp.NewToolResultError("menu service not configured"), nil
			}
			items, err := deps.Menu.ListForEmployee(ctx, menu.EmployeeMenuFilter{Plant: plant, Day: day})
			if err != nil {
				return mcp.NewToolResultError(fmt.Sprintf("list menu: %v", err)), nil
			}
			data, _ := json.Marshal(map[string]any{
				"plant":       plant,
				"supply_date": day.Format("2006-01-02"),
				"count":       len(items),
				"items":       items,
			})
			return mcp.NewToolResultText(string(data)), nil
		},
	)

	// -------- menu.search --------
	// Searches the employee menu by keyword. Supports tag/price filters and
	// in-stock-only mode. The most generally useful tool for "what can I eat
	// today" prompts.
	s.AddTool(
		mcp.NewTool("menu.search",
			mcp.WithDescription("Search the day's menu by keyword. Filters by tags / price range / in-stock. Returns ranked items with vendor and pickup info."),
			mcp.WithString("query",
				mcp.Description("Free-text search against item name/description. Empty matches everything."),
			),
			mcp.WithString("supply_date",
				mcp.Description("Supply date YYYY-MM-DD. Defaults to today."),
			),
			mcp.WithString("plant",
				mcp.Description("Plant code. Defaults to caller's home plant."),
			),
			mcp.WithArray("tags",
				mcp.Description("Health/diet tag filter, e.g. [\"vegan\",\"low_carb\"]. Item matches if it has ANY of these tags."),
				mcp.Items(map[string]any{"type": "string"}),
			),
			mcp.WithNumber("price_min",
				mcp.Description("Minimum price in TWD (inclusive)."),
			),
			mcp.WithNumber("price_max",
				mcp.Description("Maximum price in TWD (inclusive)."),
			),
			mcp.WithBoolean("in_stock",
				mcp.Description("When true, omit sold-out items."),
			),
			mcp.WithString("sort",
				mcp.Description("One of: name, price_asc, price_desc, remain. Default ranks by vendor then name."),
				mcp.Enum("name", "price_asc", "price_desc", "remain"),
			),
			mcp.WithNumber("limit",
				mcp.Description("Maximum number of results (default 50, max 200)."),
			),
			annoReadOnly(),
		),
		func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			u, ok := userFromCtx(ctx)
			if !ok {
				return mcp.NewToolResultError("not authenticated"), nil
			}
			if !canReadMenu(u.Role) {
				return mcp.NewToolResultError(fmt.Sprintf("role %s cannot read menu", u.Role)), nil
			}
			if deps.Menu == nil {
				return mcp.NewToolResultError("menu service not configured"), nil
			}
			args := req.GetArguments()
			plant := resolvePlant(stringArg(args, "plant"), u)
			if plant == "" {
				return mcp.NewToolResultError("plant required (no home plant on user)"), nil
			}
			day, err := parseDayOrToday(stringArg(args, "supply_date"))
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			filter := menu.EmployeeMenuFilter{
				Plant: plant,
				Day:   day,
				Q:     strings.TrimSpace(stringArg(args, "query")),
				Tags:  stringSliceArg(args, "tags"),
				Sort:  menu.EmployeeMenuSort(stringArg(args, "sort")),
			}
			if v, ok := args["price_min"].(float64); ok {
				minV := int64(v) // price_minor is whole NTD, not cents — no *100
				filter.PriceMin = &minV
			}
			if v, ok := args["price_max"].(float64); ok {
				maxV := int64(v)
				filter.PriceMax = &maxV
			}
			if v, ok := args["in_stock"].(bool); ok {
				filter.InStock = &v
			}
			items, err := deps.Menu.ListForEmployee(ctx, filter)
			if err != nil {
				return mcp.NewToolResultError(fmt.Sprintf("search menu: %v", err)), nil
			}
			limit := 50
			if v, ok := args["limit"].(float64); ok && int(v) > 0 {
				limit = min(int(v), 200)
			}
			if len(items) > limit {
				items = items[:limit]
			}
			data, _ := json.Marshal(map[string]any{
				"plant":       plant,
				"supply_date": day.Format("2006-01-02"),
				"query":       filter.Q,
				"count":       len(items),
				"items":       items,
			})
			return mcp.NewToolResultText(string(data)), nil
		},
	)

	// -------- menu.get_item --------
	// Look up one menu item by ID. Used after menu.search returns IDs the
	// LLM wants to inspect further.
	s.AddTool(
		mcp.NewTool("menu.get_item",
			mcp.WithDescription("Fetch a single menu item by ID, including images and supply info for a given date+plant."),
			mcp.WithString("item_id",
				mcp.Required(),
				mcp.Description("UUID of the menu item."),
			),
			mcp.WithString("supply_date",
				mcp.Description("Supply date YYYY-MM-DD. Defaults to today."),
			),
			mcp.WithString("plant",
				mcp.Description("Plant code. Defaults to caller's home plant."),
			),
			annoReadOnly(),
		),
		func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			u, ok := userFromCtx(ctx)
			if !ok {
				return mcp.NewToolResultError("not authenticated"), nil
			}
			if !canReadMenu(u.Role) {
				return mcp.NewToolResultError(fmt.Sprintf("role %s cannot read menu", u.Role)), nil
			}
			if deps.Menu == nil {
				return mcp.NewToolResultError("menu service not configured"), nil
			}
			itemID, err := req.RequireString("item_id")
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			args := req.GetArguments()
			plant := resolvePlant(stringArg(args, "plant"), u)
			if plant == "" {
				return mcp.NewToolResultError("plant required (no home plant on user)"), nil
			}
			day, err := parseDayOrToday(stringArg(args, "supply_date"))
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			items, err := deps.Menu.ListForEmployee(ctx, menu.EmployeeMenuFilter{Plant: plant, Day: day})
			if err != nil {
				return mcp.NewToolResultError(fmt.Sprintf("get item: %v", err)), nil
			}
			for _, it := range items {
				if it.ID == itemID {
					data, _ := json.Marshal(it)
					return mcp.NewToolResultText(string(data)), nil
				}
			}
			return mcp.NewToolResultError("item not available for this plant/date"), nil
		},
	)

	// -------- vendor.list_open --------
	// Employee-friendly vendor listing: which approved vendors serve the
	// caller's plant. Distinct from the admin vendor.list (any status).
	s.AddTool(
		mcp.NewTool("vendor.list_open",
			mcp.WithDescription("List approved vendors serving the caller's plant, with their cutoff hour and preorder window."),
			mcp.WithString("plant",
				mcp.Description("Plant code. Defaults to caller's home plant."),
			),
			annoReadOnly(),
		),
		func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			u, ok := userFromCtx(ctx)
			if !ok {
				return mcp.NewToolResultError("not authenticated"), nil
			}
			if !canReadMenu(u.Role) {
				return mcp.NewToolResultError(fmt.Sprintf("role %s cannot list vendors", u.Role)), nil
			}
			if deps.Vendor == nil {
				return mcp.NewToolResultError("vendor service not configured"), nil
			}
			plant := resolvePlant(stringArg(req.GetArguments(), "plant"), u)
			if plant == "" {
				return mcp.NewToolResultError("plant required (no home plant on user)"), nil
			}
			vendors, err := deps.Vendor.List(ctx, []vendor.Status{vendor.StatusApproved})
			if err != nil {
				return mcp.NewToolResultError(fmt.Sprintf("list vendors: %v", err)), nil
			}
			out := make([]map[string]any, 0, len(vendors))
			for _, v := range vendors {
				plants, err := deps.Vendor.ListPlants(ctx, v.ID)
				if err != nil {
					continue
				}
				if !contains(plants, plant) {
					continue
				}
				out = append(out, map[string]any{
					"id":                   v.ID,
					"display_name":         v.DisplayName,
					"cutoff_hour":          v.CutoffHour,
					"preorder_window_days": v.PreorderWindowDays,
				})
			}
			data, _ := json.Marshal(map[string]any{
				"plant":   plant,
				"count":   len(out),
				"vendors": out,
			})
			return mcp.NewToolResultText(string(data)), nil
		},
	)
}

// stringArg extracts a string field from MCP arguments. Returns "" when
// missing or wrong type; callers decide whether that's an error.
func stringArg(args map[string]any, key string) string {
	v, _ := args[key].(string)
	return v
}

// stringSliceArg pulls a []string out of MCP arguments. JSON arrays decode as
// []any whose elements are individual strings; we tolerate both forms.
func stringSliceArg(args map[string]any, key string) []string {
	raw, ok := args[key].([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(raw))
	for _, v := range raw {
		if s, ok := v.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

// parseDayOrToday parses YYYY-MM-DD; on empty input it returns today
// truncated to midnight in the server's local zone.
func parseDayOrToday(s string) (time.Time, error) {
	if s == "" {
		now := time.Now()
		return time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.Local), nil
	}
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		return time.Time{}, fmt.Errorf("invalid date (YYYY-MM-DD): %v", err)
	}
	return t, nil
}

func contains(list []string, needle string) bool {
	return slices.Contains(list, needle)
}
