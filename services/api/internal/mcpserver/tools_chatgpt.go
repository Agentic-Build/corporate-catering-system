// Package mcpserver — ChatGPT-convention search/fetch tools.
//
// ChatGPT (Custom Connectors and the Apps SDK) requires every MCP server it
// connects to expose two specific tools with a strict result shape:
//
//	search(query)            -> { "results": [ {id, title, text, url}, … ] }
//	fetch(id)                -> { id, title, text, url, metadata? }
//
// We map these into the same business operations the dedicated employee tools
// use, just with the ChatGPT shape so the connector works out of the box.
// Object IDs are prefixed (`menu:`, `order:`, `vendor:`) so fetch can route
// to the right service without an ambiguous lookup.
package mcpserver

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"github.com/takalawang/corporate-catering-system/services/api/internal/identity"
	"github.com/takalawang/corporate-catering-system/services/api/internal/menu"
)

// searchResult is the ChatGPT-required item shape for the `search` tool.
// The URL is a deep link into the employee web app so the user can confirm
// or follow up in-product.
type searchResult struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	Text  string `json:"text"`
	URL   string `json:"url"`
}

// fetchResult is the ChatGPT-required shape for the `fetch` tool. Metadata is
// optional but ChatGPT surfaces it in the citation card.
type fetchResult struct {
	ID       string         `json:"id"`
	Title    string         `json:"title"`
	Text     string         `json:"text"`
	URL      string         `json:"url"`
	Metadata map[string]any `json:"metadata,omitempty"`
}

const (
	prefixMenu   = "menu:"
	prefixOrder  = "order:"
	prefixVendor = "vendor:"
)

func registerChatGPTTools(s *server.MCPServer, deps Deps) {
	// -------- search --------
	// Unified search across menu items + the caller's own orders. Required by
	// ChatGPT Custom Connectors / Apps SDK. Returns at most 20 results to fit
	// inside ChatGPT's tool-result token budget.
	s.AddTool(
		mcp.NewTool("search",
			mcp.WithDescription("Search across the corporate catering platform. Returns matching menu items the employee can order today plus their recent orders. Use the returned IDs with the `fetch` tool to retrieve full details."),
			mcp.WithString("query",
				mcp.Required(),
				mcp.Description("Natural-language search query. Examples: 'vegan lunch under 150', 'my orders this week', 'sushi'."),
			),
			annoReadOnly(),
		),
		func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			u, ok := userFromCtx(ctx)
			if !ok {
				return mcp.NewToolResultError("not authenticated"), nil
			}
			if !canReadMenu(u.Role) {
				return mcp.NewToolResultError(fmt.Sprintf("role %s cannot search", u.Role)), nil
			}
			q, err := req.RequireString("query")
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			q = strings.TrimSpace(q)

			results := []searchResult{}

			// 1) Menu items — only when we have a plant to scope by. Plant
			//    fallback to the user's home plant.
			if deps.Menu != nil && u.Plant != nil && *u.Plant != "" {
				now := time.Now()
				day := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.Local)
				items, err := deps.Menu.ListForEmployee(ctx, menu.EmployeeMenuFilter{
					Plant: *u.Plant,
					Day:   day,
					Q:     q,
				})
				if err == nil {
					for _, it := range items {
						if len(results) >= 20 {
							break
						}
						results = append(results, searchResult{
							ID:    prefixMenu + it.ID,
							Title: it.Name,
							Text:  formatMenuSnippet(it),
							URL:   "/menu?date=" + day.Format("2006-01-02") + "&item=" + it.ID,
						})
					}
				}
			}

			// 2) Orders — filter the caller's recent orders client-side by
			//    matching the query against vendor + plant + supply_date.
			//    Cheap because list_mine is bounded to ~30 days.
			if deps.Order != nil && u.Role == identity.RoleEmployee && len(results) < 20 {
				orders, err := deps.Order.ListByUser(ctx, u.ID)
				if err == nil {
					lower := strings.ToLower(q)
					for _, o := range orders {
						if len(results) >= 20 {
							break
						}
						hay := strings.ToLower(o.Plant + " " + o.SupplyDate.Format("2006-01-02") + " " + string(o.Status))
						if lower == "" || strings.Contains(hay, lower) {
							results = append(results, searchResult{
								ID:    prefixOrder + o.ID,
								Title: fmt.Sprintf("Order %s — %s", shortID(o.ID), o.SupplyDate.Format("2006-01-02")),
								Text:  fmt.Sprintf("plant=%s status=%s items=%d", o.Plant, o.Status, len(o.Items)),
								URL:   "/orders/" + o.ID,
							})
						}
					}
				}
			}

			payload, _ := json.Marshal(map[string]any{"results": results})
			return mcp.NewToolResultText(string(payload)), nil
		},
	)

	// -------- fetch --------
	// Retrieves a full document for a single ID returned by `search`. Routes
	// by prefix: menu:<uuid>, order:<uuid>, vendor:<uuid>.
	s.AddTool(
		mcp.NewTool("fetch",
			mcp.WithDescription("Fetch the full content of one search result by ID. Accepts IDs returned by the `search` tool (prefixed with menu:, order:, or vendor:)."),
			mcp.WithString("id",
				mcp.Required(),
				mcp.Description("Result ID from the `search` tool, e.g. 'menu:<uuid>' or 'order:<uuid>'."),
			),
			annoReadOnly(),
		),
		func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			u, ok := userFromCtx(ctx)
			if !ok {
				return mcp.NewToolResultError("not authenticated"), nil
			}
			id, err := req.RequireString("id")
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			switch {
			case strings.HasPrefix(id, prefixMenu):
				return fetchMenuItem(ctx, deps, u, strings.TrimPrefix(id, prefixMenu))
			case strings.HasPrefix(id, prefixOrder):
				return fetchOrder(ctx, deps, u, strings.TrimPrefix(id, prefixOrder))
			case strings.HasPrefix(id, prefixVendor):
				return fetchVendor(ctx, deps, u, strings.TrimPrefix(id, prefixVendor))
			default:
				return mcp.NewToolResultError(fmt.Sprintf("unknown id prefix in %q; expected menu:, order:, or vendor:", id)), nil
			}
		},
	)
}

func fetchMenuItem(ctx context.Context, deps Deps, u *identity.User, itemID string) (*mcp.CallToolResult, error) {
	if !canReadMenu(u.Role) {
		return mcp.NewToolResultError("role cannot read menu"), nil
	}
	if deps.Menu == nil || u.Plant == nil {
		return mcp.NewToolResultError("menu service unavailable or no home plant"), nil
	}
	now := time.Now()
	day := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.Local)
	items, err := deps.Menu.ListForEmployee(ctx, menu.EmployeeMenuFilter{Plant: *u.Plant, Day: day})
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("fetch menu: %v", err)), nil
	}
	for _, it := range items {
		if it.ID != itemID {
			continue
		}
		out := fetchResult{
			ID:    prefixMenu + it.ID,
			Title: it.Name,
			Text:  formatMenuFull(it),
			URL:   "/menu?date=" + day.Format("2006-01-02") + "&item=" + it.ID,
			Metadata: map[string]any{
				"vendor":        it.VendorName,
				"price_minor":   it.PriceMinor,
				"tags":          it.Tags,
				"sold_out":      it.SoldOut,
				"remain":        it.Remain,
				"pickup_window": it.PickupWindow,
				"images":        it.Images,
			},
		}
		payload, _ := json.Marshal(out)
		return mcp.NewToolResultText(string(payload)), nil
	}
	return mcp.NewToolResultError("menu item not found for current plant/date"), nil
}

func fetchOrder(ctx context.Context, deps Deps, u *identity.User, orderID string) (*mcp.CallToolResult, error) {
	if u.Role != identity.RoleEmployee {
		return mcp.NewToolResultError("only employees can fetch their own orders"), nil
	}
	if deps.Order == nil {
		return mcp.NewToolResultError("order service unavailable"), nil
	}
	o, err := deps.Order.Get(ctx, orderID, u.ID)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	out := fetchResult{
		ID:    prefixOrder + o.ID,
		Title: fmt.Sprintf("Order %s — %s", shortID(o.ID), o.SupplyDate.Format("2006-01-02")),
		Text:  fmt.Sprintf("plant=%s status=%s items=%d", o.Plant, o.Status, len(o.Items)),
		URL:   "/orders/" + o.ID,
		Metadata: map[string]any{
			"plant":       o.Plant,
			"status":      o.Status,
			"supply_date": o.SupplyDate.Format("2006-01-02"),
			"items":       o.Items,
			"vendor_id":   o.VendorID,
			"created_at":  o.CreatedAt.Format(time.RFC3339),
		},
	}
	payload, _ := json.Marshal(out)
	return mcp.NewToolResultText(string(payload)), nil
}

func fetchVendor(ctx context.Context, deps Deps, u *identity.User, vendorID string) (*mcp.CallToolResult, error) {
	if !canReadMenu(u.Role) {
		return mcp.NewToolResultError("role cannot read vendors"), nil
	}
	if deps.Vendor == nil {
		return mcp.NewToolResultError("vendor service unavailable"), nil
	}
	v, err := deps.Vendor.Get(ctx, vendorID)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	out := fetchResult{
		ID:    prefixVendor + v.ID,
		Title: v.DisplayName,
		Text:  fmt.Sprintf("Approved vendor. Cutoff %02d:00, preorder window %d days.", v.CutoffHour, v.PreorderWindowDays),
		URL:   "/vendors/" + v.ID,
		Metadata: map[string]any{
			"status":               v.Status,
			"cutoff_hour":          v.CutoffHour,
			"preorder_window_days": v.PreorderWindowDays,
		},
	}
	payload, _ := json.Marshal(out)
	return mcp.NewToolResultText(string(payload)), nil
}

func formatMenuSnippet(it menu.EmployeeMenuItem) string {
	status := "available"
	if it.SoldOut {
		status = "sold out"
	}
	return fmt.Sprintf("%s · NT$%d · %s · %s", it.VendorName, it.PriceMinor, status, it.PickupWindow)
}

func formatMenuFull(it menu.EmployeeMenuItem) string {
	parts := []string{
		fmt.Sprintf("Vendor: %s", it.VendorName),
		fmt.Sprintf("Price: NT$%d", it.PriceMinor),
		fmt.Sprintf("Pickup: %s (%s)", it.PickupWindow, it.ETALabel),
	}
	if it.SoldOut {
		parts = append(parts, "Status: SOLD OUT")
	} else {
		parts = append(parts, fmt.Sprintf("Remaining: %d / %d", it.Remain, it.Capacity))
	}
	if len(it.Tags) > 0 {
		parts = append(parts, "Tags: "+strings.Join(it.Tags, ", "))
	}
	if it.Description != "" {
		parts = append(parts, "", it.Description)
	}
	return strings.Join(parts, "\n")
}

func shortID(id string) string {
	if len(id) > 8 {
		return id[:8]
	}
	return id
}
