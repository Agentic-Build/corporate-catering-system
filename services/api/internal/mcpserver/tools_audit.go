// Package mcpserver — audit query tool.
//
// Admin-only read of audit_event. Mirrors the /api/admin/audit HTTP endpoint
// in compliance/http/handlers.go.
package mcpserver

import (
	"context"
	"encoding/json"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"github.com/Agentic-Build/corporate-catering-system/services/api/internal/compliance"
	"github.com/Agentic-Build/corporate-catering-system/services/api/internal/identity"
)

func registerAuditTools(s *server.MCPServer, deps Deps) {
	s.AddTool(
		mcp.NewTool("audit.query",
			mcp.WithDescription("Query audit_event with optional filters (welfare_admin only)"),
			mcp.WithString("target_kind",
				mcp.Description("Optional: order | vendor | payroll_batch | …"),
			),
			mcp.WithString("target_id",
				mcp.Description("Optional UUID of target"),
			),
			mcp.WithString("since",
				mcp.Description("Optional RFC3339 timestamp"),
			),
			mcp.WithNumber("limit",
				mcp.Description("Default 100; service may clamp"),
			),
			annoReadOnly(),
		),
		func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			u, ok := userFromCtx(ctx)
			if !ok {
				return mcp.NewToolResultError("not authenticated"), nil
			}
			if u.Role != identity.RoleWelfareAdmin {
				return mcp.NewToolResultError("only welfare_admin can query audit"), nil
			}
			if deps.Compliance == nil {
				return mcp.NewToolResultError("compliance service not configured"), nil
			}
			filter := compliance.AuditFilter{
				TargetKind: req.GetString("target_kind", ""),
				TargetID:   req.GetString("target_id", ""),
				Limit:      int(req.GetFloat("limit", 100)),
			}
			if since := req.GetString("since", ""); since != "" {
				t, err := time.Parse(time.RFC3339, since)
				if err != nil {
					return mcp.NewToolResultError("since must be RFC3339"), nil
				}
				filter.Since = t
			}
			rows, err := deps.Compliance.QueryAudit(ctx, filter)
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			auditAfter(ctx, deps, "audit.query", "audit_event", "list", nil, u)
			data, _ := json.Marshal(map[string]any{"count": len(rows), "events": rows})
			return mcp.NewToolResultText(string(data)), nil
		},
	)
}
