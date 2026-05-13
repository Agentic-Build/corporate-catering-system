// Package mcpserver — vendor tools.
//
// Admin-only read-side over vendor.Service for the MCP transport.
package mcpserver

import (
	"context"
	"encoding/json"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"github.com/takalawang/corporate-catering-system/services/api/internal/identity"
	vendor "github.com/takalawang/corporate-catering-system/services/api/internal/vendors"
)

func registerVendorTools(s *server.MCPServer, deps Deps) {
	s.AddTool(
		mcp.NewTool("vendor.list",
			mcp.WithDescription("List vendors with optional status filter (welfare_admin only)"),
			mcp.WithString("status",
				mcp.Description("Optional: pending | approved | suspended | terminated"),
			),
		),
		func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			u, ok := userFromCtx(ctx)
			if !ok {
				return mcp.NewToolResultError("not authenticated"), nil
			}
			if u.Role != identity.RoleWelfareAdmin {
				return mcp.NewToolResultError("only welfare_admin can list vendors"), nil
			}
			if deps.Vendor == nil {
				return mcp.NewToolResultError("vendor service not configured"), nil
			}
			var statuses []vendor.Status
			if statusStr := req.GetString("status", ""); statusStr != "" {
				statuses = []vendor.Status{vendor.Status(statusStr)}
			}
			vs, err := deps.Vendor.List(ctx, statuses)
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			data, _ := json.Marshal(map[string]any{"count": len(vs), "vendors": vs})
			return mcp.NewToolResultText(string(data)), nil
		},
	)
}
