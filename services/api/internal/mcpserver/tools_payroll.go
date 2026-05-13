// Package mcpserver — payroll tools.
//
// Admin-only read-side over payroll.Service.
package mcpserver

import (
	"context"
	"encoding/json"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"github.com/takalawang/corporate-catering-system/services/api/internal/identity"
	"github.com/takalawang/corporate-catering-system/services/api/internal/payroll"
)

func registerPayrollTools(s *server.MCPServer, deps Deps) {
	s.AddTool(
		mcp.NewTool("payroll.list_batches",
			mcp.WithDescription("List payroll batches (welfare_admin only)"),
			mcp.WithString("status",
				mcp.Description("Optional: draft | locked | exported | closed"),
			),
		),
		func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			u, ok := userFromCtx(ctx)
			if !ok {
				return mcp.NewToolResultError("not authenticated"), nil
			}
			if u.Role != identity.RoleWelfareAdmin {
				return mcp.NewToolResultError("only welfare_admin can list batches"), nil
			}
			if deps.Payroll == nil {
				return mcp.NewToolResultError("payroll service not configured"), nil
			}
			var statuses []payroll.BatchStatus
			if statusStr := req.GetString("status", ""); statusStr != "" {
				statuses = []payroll.BatchStatus{payroll.BatchStatus(statusStr)}
			}
			bs, err := deps.Payroll.ListBatches(ctx, statuses)
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			data, _ := json.Marshal(map[string]any{"count": len(bs), "batches": bs})
			return mcp.NewToolResultText(string(data)), nil
		},
	)
}
