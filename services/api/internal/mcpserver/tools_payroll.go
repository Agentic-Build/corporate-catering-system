// Package mcpserver — payroll tools.
//
// Admin-only operations over payroll.Service. Each handler enforces the
// welfare_admin role gate, delegates to the Service, then writes an
// audit_event row tagged with request_id="mcp:<tool>".
package mcpserver

import (
	"context"
	"encoding/json"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"github.com/Agentic-Build/corporate-catering-system/services/api/internal/identity"
	"github.com/Agentic-Build/corporate-catering-system/services/api/internal/payroll"
)

func registerPayrollTools(s *server.MCPServer, deps Deps) {
	s.AddTool(
		mcp.NewTool("payroll.list_batches",
			mcp.WithDescription("List payroll batches (welfare_admin only)"),
			mcp.WithString("status",
				mcp.Description("Optional: draft | locked | exported | closed"),
			),
			annoReadOnly(),
		),
		payrollListBatchesHandler(deps),
	)
	s.AddTool(
		mcp.NewTool("payroll.lock_batch",
			mcp.WithDescription("Lock a draft payroll batch (welfare_admin only)"),
			mcp.WithString("batch_id",
				mcp.Required(),
				mcp.Description("UUID of the batch to lock"),
			),
			annoHighRiskAdmin(),
		),
		payrollLockBatchHandler(deps),
	)
	s.AddTool(
		mcp.NewTool("payroll.resolve_dispute",
			mcp.WithDescription("Resolve a payroll dispute (welfare_admin only; high-risk on refund)"),
			mcp.WithString("dispute_id",
				mcp.Required(),
				mcp.Description("UUID of the open dispute"),
			),
			mcp.WithString("status",
				mcp.Required(),
				mcp.Description("resolved_refund | resolved_reject"),
			),
			mcp.WithString("resolution",
				mcp.Description("Free-text resolution notes"),
			),
			mcp.WithNumber("refund_minor",
				mcp.Description("Refund amount in minor units; 0 for reject"),
			),
			annoStateChange(),
		),
		payrollResolveDisputeHandler(deps),
	)
}

// adminPayrollPrelude validates auth + welfare_admin role + Payroll wired.
func adminPayrollPrelude(ctx context.Context, deps Deps, denyMsg string) (*identity.User, *mcp.CallToolResult) {
	u, ok := userFromCtx(ctx)
	if !ok {
		return nil, mcp.NewToolResultError(errNotAuthenticated)
	}
	if u.Role != identity.RoleWelfareAdmin {
		return nil, mcp.NewToolResultError(denyMsg)
	}
	if deps.Payroll == nil {
		return nil, mcp.NewToolResultError(errPayrollNotConfigured)
	}
	return u, nil
}

func payrollListBatchesHandler(deps Deps) func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		u, errRes := adminPayrollPrelude(ctx, deps, "only welfare_admin can list batches")
		if errRes != nil {
			return errRes, nil
		}
		var statuses []payroll.BatchStatus
		if statusStr := req.GetString("status", ""); statusStr != "" {
			statuses = []payroll.BatchStatus{payroll.BatchStatus(statusStr)}
		}
		bs, err := deps.Payroll.ListBatches(ctx, statuses)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		auditAfter(ctx, deps, "payroll.list_batches", "payroll_batch", "list", nil, u)
		data, _ := json.Marshal(map[string]any{"count": len(bs), "batches": bs})
		return mcp.NewToolResultText(string(data)), nil
	}
}

func payrollLockBatchHandler(deps Deps) func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		u, errRes := adminPayrollPrelude(ctx, deps, "only welfare_admin can lock batches")
		if errRes != nil {
			return errRes, nil
		}
		batchID, err := req.RequireString("batch_id")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		if err := deps.Payroll.Lock(ctx, batchID, u.ID); err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		auditAfter(ctx, deps, "payroll.lock_batch", "payroll_batch", batchID, nil, u)
		return mcp.NewToolResultText(`{"status":"locked"}`), nil
	}
}

func payrollResolveDisputeHandler(deps Deps) func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		u, errRes := adminPayrollPrelude(ctx, deps, "only welfare_admin can resolve disputes")
		if errRes != nil {
			return errRes, nil
		}
		disputeID, err := req.RequireString("dispute_id")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		statusStr, err := req.RequireString("status")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		if statusStr != string(payroll.DisputeStatusResolvedRefund) &&
			statusStr != string(payroll.DisputeStatusResolvedReject) {
			return mcp.NewToolResultError("status must be resolved_refund | resolved_reject"), nil
		}
		resolution := req.GetString("resolution", "")
		refundMinor := int64(req.GetFloat("refund_minor", 0))
		if err := deps.Payroll.ResolveDispute(ctx, payroll.ResolveDisputeInput{
			DisputeID:   disputeID,
			ResolvedBy:  u.ID,
			Status:      payroll.DisputeStatus(statusStr),
			Resolution:  resolution,
			RefundMinor: refundMinor,
		}); err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		auditAfter(ctx, deps, "payroll.resolve_dispute", "payroll_dispute", disputeID, map[string]any{
			"status":       statusStr,
			"refund_minor": refundMinor,
		}, u)
		return mcp.NewToolResultText(`{"status":"resolved"}`), nil
	}
}
