// menu/vendor discovery tools. Each handler: authenticates via context,
// gates on employee/welfare_admin, delegates to menu.Service / vendor.Service,
// returns compact JSON for LLM tool-use.
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

	"github.com/Agentic-Build/corporate-catering-system/services/api/internal/identity"
	"github.com/Agentic-Build/corporate-catering-system/services/api/internal/menu"
	vendor "github.com/Agentic-Build/corporate-catering-system/services/api/internal/vendors"
)

const descPlantCodeDefault = "Plant code. Defaults to caller's home plant."

// canReadMenu returns true when the role can read employee-facing menus.
func canReadMenu(r identity.Role) bool {
	return r == identity.RoleEmployee || r == identity.RoleWelfareAdmin
}

// resolvePlant returns arg, falling back to the user's home plant ("" when neither).
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
		menuListForDayHandler(deps),
	)

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
				mcp.Description(descPlantCodeDefault),
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
		menuSearchHandler(deps),
	)

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
				mcp.Description(descPlantCodeDefault),
			),
			annoReadOnly(),
		),
		menuGetItemHandler(deps),
	)

	// vendor.list_open — approved vendors serving the caller's plant
	// (distinct from admin vendor.list, which spans any status).
	s.AddTool(
		mcp.NewTool("vendor.list_open",
			mcp.WithDescription("List approved vendors serving the caller's plant, with their cutoff hour and preorder window."),
			mcp.WithString("plant",
				mcp.Description(descPlantCodeDefault),
			),
			annoReadOnly(),
		),
		vendorListOpenHandler(deps),
	)
}

// menuPrelude runs the auth/plant/menu-config gate shared across menu tools.
// Returns the resolved plant + non-nil error result when the gate trips.
func menuPrelude(ctx context.Context, deps Deps, plantArg string) (string, *mcp.CallToolResult) {
	u, ok := userFromCtx(ctx)
	if !ok {
		return "", mcp.NewToolResultError(errNotAuthenticated)
	}
	if !canReadMenu(u.Role) {
		return "", mcp.NewToolResultError(fmt.Sprintf(errRoleCannotReadMenu, u.Role))
	}
	plant := resolvePlant(plantArg, u)
	if plant == "" {
		return "", mcp.NewToolResultError(errPlantRequired)
	}
	if deps.Menu == nil {
		return "", mcp.NewToolResultError(errMenuNotConfigured)
	}
	return plant, nil
}

func menuListForDayHandler(deps Deps) func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		args := req.GetArguments()
		plant, errRes := menuPrelude(ctx, deps, stringArg(args, "plant"))
		if errRes != nil {
			return errRes, nil
		}
		day, err := parseDayOrToday(stringArg(args, "supply_date"))
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		items, err := deps.Menu.ListForEmployee(ctx, menu.EmployeeMenuFilter{Plant: plant, Day: day})
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("list menu: %v", err)), nil
		}
		data, _ := json.Marshal(map[string]any{
			"plant":       plant,
			"supply_date": day.Format(dateLayoutISO),
			"count":       len(items),
			"items":       items,
		})
		return mcp.NewToolResultText(string(data)), nil
	}
}

// buildMenuSearchFilter constructs an EmployeeMenuFilter from the search args.
func buildMenuSearchFilter(args map[string]any, plant string, day time.Time) menu.EmployeeMenuFilter {
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
	return filter
}

func menuSearchHandler(deps Deps) func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		args := req.GetArguments()
		plant, errRes := menuPrelude(ctx, deps, stringArg(args, "plant"))
		if errRes != nil {
			return errRes, nil
		}
		day, err := parseDayOrToday(stringArg(args, "supply_date"))
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		filter := buildMenuSearchFilter(args, plant, day)
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
			"supply_date": day.Format(dateLayoutISO),
			"query":       filter.Q,
			"count":       len(items),
			"items":       items,
		})
		return mcp.NewToolResultText(string(data)), nil
	}
}

func menuGetItemHandler(deps Deps) func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		args := req.GetArguments()
		plant, errRes := menuPrelude(ctx, deps, stringArg(args, "plant"))
		if errRes != nil {
			return errRes, nil
		}
		itemID, err := req.RequireString("item_id")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
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
	}
}

func vendorListOpenHandler(deps Deps) func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		u, ok := userFromCtx(ctx)
		if !ok {
			return mcp.NewToolResultError(errNotAuthenticated), nil
		}
		if !canReadMenu(u.Role) {
			return mcp.NewToolResultError(fmt.Sprintf("role %s cannot list vendors", u.Role)), nil
		}
		if deps.Vendor == nil {
			return mcp.NewToolResultError(errVendorNotConfigured), nil
		}
		plant := resolvePlant(stringArg(req.GetArguments(), "plant"), u)
		if plant == "" {
			return mcp.NewToolResultError(errPlantRequired), nil
		}
		vendors, err := deps.Vendor.List(ctx, []vendor.Status{vendor.StatusApproved})
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("list vendors: %v", err)), nil
		}
		out := buildVendorListForPlant(ctx, deps, vendors, plant)
		data, _ := json.Marshal(map[string]any{
			"plant":   plant,
			"count":   len(out),
			"vendors": out,
		})
		return mcp.NewToolResultText(string(data)), nil
	}
}

// buildVendorListForPlant filters approved vendors to those serving the plant.
func buildVendorListForPlant(ctx context.Context, deps Deps, vendors []*vendor.Vendor, plant string) []map[string]any {
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
	return out
}

// stringArg extracts a string field from MCP arguments ("" when missing).
func stringArg(args map[string]any, key string) string {
	v, _ := args[key].(string)
	return v
}

// stringSliceArg pulls a []string from MCP arguments (JSON arrays decode as []any).
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

// parseDayOrToday parses YYYY-MM-DD; empty input → today at midnight local.
func parseDayOrToday(s string) (time.Time, error) {
	if s == "" {
		now := time.Now()
		return time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.Local), nil
	}
	t, err := time.Parse(dateLayoutISO, s)
	if err != nil {
		return time.Time{}, fmt.Errorf("invalid date (YYYY-MM-DD): %v", err)
	}
	return t, nil
}

func contains(list []string, needle string) bool {
	return slices.Contains(list, needle)
}
