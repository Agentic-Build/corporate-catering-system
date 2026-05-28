// Package mcpserver — settlement tools.
//
// Admin-only operations over settlement.Service for the MCP transport. The
// close handler enforces the welfare_admin role gate, delegates to the
// Service so business rules stay identical to the HTTP path, then writes an
// audit_event row tagged with request_id="mcp:<tool>".
package mcpserver

import (
	"context"
	"encoding/json"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"github.com/Agentic-Build/corporate-catering-system/services/api/internal/identity"
	"github.com/Agentic-Build/corporate-catering-system/services/api/internal/settlement"
)

func registerSettlementTools(s *server.MCPServer, deps Deps) {
	s.AddTool(
		mcp.NewTool("settlement.close_period",
			mcp.WithDescription("Close a vendor settlement period: cut one settlement per vendor with orders (welfare_admin only)"),
			mcp.WithString("period_start",
				mcp.Required(),
				mcp.Description("Period start date in YYYY-MM-DD"),
			),
			mcp.WithString("period_end",
				mcp.Required(),
				mcp.Description("Period end date in YYYY-MM-DD"),
			),
			annoHighRiskAdmin(),
		),
		settlementClosePeriodHandler(deps),
	)
}

func settlementClosePeriodHandler(deps Deps) func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		u, ok := userFromCtx(ctx)
		if !ok {
			return mcp.NewToolResultError(errNotAuthenticated), nil
		}
		if u.Role != identity.RoleWelfareAdmin {
			return mcp.NewToolResultError("only welfare_admin can close settlement periods"), nil
		}
		if deps.Settlement == nil {
			return mcp.NewToolResultError("settlement service not configured"), nil
		}
		startStr, err := req.RequireString("period_start")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		endStr, err := req.RequireString("period_end")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		start, err := time.Parse(dateLayoutISO, startStr)
		if err != nil {
			return mcp.NewToolResultError("invalid period_start (YYYY-MM-DD)"), nil
		}
		end, err := time.Parse(dateLayoutISO, endStr)
		if err != nil {
			return mcp.NewToolResultError("invalid period_end (YYYY-MM-DD)"), nil
		}
		settlements, err := deps.Settlement.CloseSettlement(ctx, settlement.CloseSettlementInput{
			PeriodStart: start,
			PeriodEnd:   end,
			ClosedBy:    u.ID,
		})
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		auditAfter(ctx, deps, "settlement.close_period", "vendor_settlement_period",
			startStr+"/"+endStr, map[string]any{
				"period_start": startStr,
				"period_end":   endStr,
				"vendor_count": len(settlements),
			}, u)
		data, _ := json.Marshal(map[string]any{
			"count":       len(settlements),
			"settlements": settlements,
		})
		return mcp.NewToolResultText(string(data)), nil
	}
}
