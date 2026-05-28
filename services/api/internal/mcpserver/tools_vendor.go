// Package mcpserver — vendor tools.
//
// Admin-only operations over vendor.Service for the MCP transport. Each
// handler enforces the welfare_admin role gate, delegates to the Service so
// business rules stay identical to the HTTP path, then writes an audit_event
// row tagged with request_id="mcp:<tool>".
package mcpserver

import (
	"context"
	"encoding/json"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"github.com/Agentic-Build/corporate-catering-system/services/api/internal/identity"
	vendor "github.com/Agentic-Build/corporate-catering-system/services/api/internal/vendors"
)

func registerVendorTools(s *server.MCPServer, deps Deps) {
	s.AddTool(
		mcp.NewTool("vendor.list",
			mcp.WithDescription("List vendors with optional status filter (welfare_admin only)"),
			mcp.WithString("status",
				mcp.Description("Optional: pending | approved | suspended | terminated"),
			),
			annoReadOnly(),
		),
		vendorListHandler(deps),
	)
	s.AddTool(
		mcp.NewTool("vendor.suspend",
			mcp.WithDescription("Suspend an approved vendor (welfare_admin only, high-risk)"),
			mcp.WithString("vendor_id",
				mcp.Required(),
				mcp.Description("UUID of the vendor to suspend"),
			),
			mcp.WithString("reason",
				mcp.Description("Optional reason (recorded in audit_event payload)"),
			),
			annoHighRiskAdmin(),
		),
		vendorSuspendHandler(deps),
	)
	s.AddTool(
		mcp.NewTool("vendor.reinstate",
			mcp.WithDescription("Reinstate a suspended vendor (welfare_admin only)"),
			mcp.WithString("vendor_id",
				mcp.Required(),
				mcp.Description("UUID of the suspended vendor"),
			),
			annoReversible(),
		),
		vendorReinstateHandler(deps),
	)
}

// adminVendorPrelude validates auth + welfare_admin role + Vendor wired.
func adminVendorPrelude(ctx context.Context, deps Deps, denyMsg string) (*identity.User, *mcp.CallToolResult) {
	u, ok := userFromCtx(ctx)
	if !ok {
		return nil, mcp.NewToolResultError(errNotAuthenticated)
	}
	if u.Role != identity.RoleWelfareAdmin {
		return nil, mcp.NewToolResultError(denyMsg)
	}
	if deps.Vendor == nil {
		return nil, mcp.NewToolResultError(errVendorNotConfigured)
	}
	return u, nil
}

func vendorListHandler(deps Deps) func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		u, errRes := adminVendorPrelude(ctx, deps, "only welfare_admin can list vendors")
		if errRes != nil {
			return errRes, nil
		}
		var statuses []vendor.Status
		if statusStr := req.GetString("status", ""); statusStr != "" {
			statuses = []vendor.Status{vendor.Status(statusStr)}
		}
		vs, err := deps.Vendor.List(ctx, statuses)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		auditAfter(ctx, deps, "vendor.list", "vendor", "list", nil, u)
		data, _ := json.Marshal(map[string]any{"count": len(vs), "vendors": vs})
		return mcp.NewToolResultText(string(data)), nil
	}
}

func vendorSuspendHandler(deps Deps) func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		u, errRes := adminVendorPrelude(ctx, deps, "only welfare_admin can suspend vendors")
		if errRes != nil {
			return errRes, nil
		}
		vendorID, err := req.RequireString("vendor_id")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		reason := req.GetString("reason", "")
		if err := deps.Vendor.Suspend(ctx, vendorID, u.ID); err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		payload := map[string]any{}
		if reason != "" {
			payload["reason"] = reason
		}
		auditAfter(ctx, deps, "vendor.suspend", "vendor", vendorID, payload, u)
		return mcp.NewToolResultText(`{"status":"suspended"}`), nil
	}
}

func vendorReinstateHandler(deps Deps) func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		u, errRes := adminVendorPrelude(ctx, deps, "only welfare_admin can reinstate vendors")
		if errRes != nil {
			return errRes, nil
		}
		vendorID, err := req.RequireString("vendor_id")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		if err := deps.Vendor.Reinstate(ctx, vendorID, u.ID); err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		auditAfter(ctx, deps, "vendor.reinstate", "vendor", vendorID, nil, u)
		return mcp.NewToolResultText(`{"status":"reinstated"}`), nil
	}
}
