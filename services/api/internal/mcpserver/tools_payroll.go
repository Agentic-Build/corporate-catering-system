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

	"github.com/takalawang/corporate-catering-system/services/api/internal/identity"
	"github.com/takalawang/corporate-catering-system/services/api/internal/payroll"
)

func registerPayrollTools(s *server.MCPServer, deps Deps) {
	// -------- payroll.list_batches --------
	s.AddTool(
		mcp.NewTool("payroll.list_batches",
			mcp.WithDescription("List payroll batches (welfare_admin only)"),
			mcp.WithString("status",
				mcp.Description("Optional: draft | locked | exported | closed"),
			),
			annoReadOnly(),
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
			auditAfter(ctx, deps, "payroll.list_batches", "payroll_batch", "list", nil, u)
			data, _ := json.Marshal(map[string]any{"count": len(bs), "batches": bs})
			return mcp.NewToolResultText(string(data)), nil
		},
	)

	// -------- payroll.lock_batch --------
	s.AddTool(
		mcp.NewTool("payroll.lock_batch",
			mcp.WithDescription("Lock a draft payroll batch (welfare_admin only)"),
			mcp.WithString("batch_id",
				mcp.Required(),
				mcp.Description("UUID of the batch to lock"),
			),
			annoHighRiskAdmin(),
		),
		func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			u, ok := userFromCtx(ctx)
			if !ok {
				return mcp.NewToolResultError("not authenticated"), nil
			}
			if u.Role != identity.RoleWelfareAdmin {
				return mcp.NewToolResultError("only welfare_admin can lock batches"), nil
			}
			if deps.Payroll == nil {
				return mcp.NewToolResultError("payroll service not configured"), nil
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
		},
	)

	// -------- payroll.resolve_dispute (high-risk on refund path) --------
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
		func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			u, ok := userFromCtx(ctx)
			if !ok {
				return mcp.NewToolResultError("not authenticated"), nil
			}
			if u.Role != identity.RoleWelfareAdmin {
				return mcp.NewToolResultError("only welfare_admin can resolve disputes"), nil
			}
			if deps.Payroll == nil {
				return mcp.NewToolResultError("payroll service not configured"), nil
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
		},
	)
}
