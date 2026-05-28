// Package mcpserver — compliance tools.
//
// Admin-only document-review and anomaly-governance operations over
// compliance.Service for the MCP transport. Each handler enforces the
// welfare_admin role gate, delegates to the Service so business rules stay
// identical to the HTTP path, then writes an audit_event row tagged with
// request_id="mcp:<tool>".
package mcpserver

import (
	"context"
	"encoding/json"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"github.com/Agentic-Build/corporate-catering-system/services/api/internal/compliance"
	"github.com/Agentic-Build/corporate-catering-system/services/api/internal/identity"
)

func registerComplianceTools(s *server.MCPServer, deps Deps) {
	s.AddTool(
		mcp.NewTool("document.list",
			mcp.WithDescription("List a vendor's compliance documents (welfare_admin only)"),
			mcp.WithString("vendor_id", mcp.Required(), mcp.Description("UUID of the vendor")),
			mcp.WithBoolean("include_all", mcp.Description("Include expired documents")),
			annoReadOnly(),
		),
		documentListHandler(deps),
	)
	s.AddTool(
		mcp.NewTool("document.review",
			mcp.WithDescription("Approve or reject a pending vendor document (welfare_admin only)"),
			mcp.WithString("document_id", mcp.Required(), mcp.Description("UUID of the document")),
			mcp.WithString("status", mcp.Required(), mcp.Description("approved | rejected")),
			mcp.WithString("notes", mcp.Description("Review notes (recorded on the document)")),
			annoStateChange(),
		),
		documentReviewHandler(deps),
	)
	s.AddTool(
		mcp.NewTool("anomaly.list",
			mcp.WithDescription("List anomaly alerts filtered by status/severity (welfare_admin only)"),
			mcp.WithString("status", mcp.Description("Optional: open | triaged | closed")),
			mcp.WithString("severity", mcp.Description("Optional: low | medium | high | critical")),
			annoReadOnly(),
		),
		anomalyListHandler(deps),
	)
	s.AddTool(
		mcp.NewTool("anomaly.triage",
			mcp.WithDescription("Triage an open anomaly, optionally warning or suspending the target vendor (welfare_admin only)"),
			mcp.WithString("anomaly_id", mcp.Required(), mcp.Description("UUID of the anomaly")),
			mcp.WithString("notes", mcp.Description("Triage notes")),
			mcp.WithString("action", mcp.Description("Optional governance action: warn | suspend")),
			annoStateChange(),
		),
		anomalyTriageHandler(deps),
	)
	s.AddTool(
		mcp.NewTool("anomaly.close",
			mcp.WithDescription("Close an open or triaged anomaly (welfare_admin only)"),
			mcp.WithString("anomaly_id", mcp.Required(), mcp.Description("UUID of the anomaly")),
			mcp.WithString("notes", mcp.Description("Closing notes")),
			annoHighRiskAdmin(),
		),
		anomalyCloseHandler(deps),
	)
}

// adminCompliancePrelude validates auth + welfare_admin role + Compliance wired.
func adminCompliancePrelude(ctx context.Context, deps Deps, denyMsg string) (*identity.User, *mcp.CallToolResult) {
	u, ok := userFromCtx(ctx)
	if !ok {
		return nil, mcp.NewToolResultError(errNotAuthenticated)
	}
	if u.Role != identity.RoleWelfareAdmin {
		return nil, mcp.NewToolResultError(denyMsg)
	}
	if deps.Compliance == nil {
		return nil, mcp.NewToolResultError(errComplianceNotConfigured)
	}
	return u, nil
}

func documentListHandler(deps Deps) func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		u, errRes := adminCompliancePrelude(ctx, deps, "only welfare_admin can list documents")
		if errRes != nil {
			return errRes, nil
		}
		vendorID, err := req.RequireString("vendor_id")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		docs, err := deps.Compliance.ListVendorDocuments(ctx, vendorID, req.GetBool("include_all", false))
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		auditAfter(ctx, deps, "document.list", "vendor", vendorID, nil, u)
		data, _ := json.Marshal(map[string]any{"count": len(docs), "documents": docs})
		return mcp.NewToolResultText(string(data)), nil
	}
}

func documentReviewHandler(deps Deps) func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		u, errRes := adminCompliancePrelude(ctx, deps, "only welfare_admin can review documents")
		if errRes != nil {
			return errRes, nil
		}
		docID, err := req.RequireString("document_id")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		status, err := req.RequireString("status")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		notes := req.GetString("notes", "")
		if err := deps.Compliance.ReviewDocument(ctx, docID, u.ID, compliance.DocumentStatus(status), notes); err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		auditAfter(ctx, deps, "document.review", "vendor_document", docID, map[string]any{"status": status}, u)
		return mcp.NewToolResultText(`{"status":"reviewed"}`), nil
	}
}

func anomalyListHandler(deps Deps) func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		u, errRes := adminCompliancePrelude(ctx, deps, "only welfare_admin can list anomalies")
		if errRes != nil {
			return errRes, nil
		}
		var statuses []compliance.AnomalyStatus
		if v := req.GetString("status", ""); v != "" {
			statuses = []compliance.AnomalyStatus{compliance.AnomalyStatus(v)}
		}
		var severities []compliance.AnomalySeverity
		if v := req.GetString("severity", ""); v != "" {
			severities = []compliance.AnomalySeverity{compliance.AnomalySeverity(v)}
		}
		items, err := deps.Compliance.ListAnomalies(ctx, statuses, severities)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		auditAfter(ctx, deps, "anomaly.list", "anomaly_alert", "list", nil, u)
		data, _ := json.Marshal(map[string]any{"count": len(items), "anomalies": items})
		return mcp.NewToolResultText(string(data)), nil
	}
}

func anomalyTriageHandler(deps Deps) func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		u, errRes := adminCompliancePrelude(ctx, deps, "only welfare_admin can triage anomalies")
		if errRes != nil {
			return errRes, nil
		}
		id, err := req.RequireString("anomaly_id")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		action := req.GetString("action", "")
		if err := deps.Compliance.TriageAnomaly(ctx, id, u.ID, req.GetString("notes", ""), action); err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		auditAfter(ctx, deps, "anomaly.triage", "anomaly_alert", id, map[string]any{"action": action}, u)
		return mcp.NewToolResultText(`{"status":"triaged"}`), nil
	}
}

func anomalyCloseHandler(deps Deps) func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		u, errRes := adminCompliancePrelude(ctx, deps, "only welfare_admin can close anomalies")
		if errRes != nil {
			return errRes, nil
		}
		id, err := req.RequireString("anomaly_id")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		if err := deps.Compliance.CloseAnomaly(ctx, id, u.ID, req.GetString("notes", "")); err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		auditAfter(ctx, deps, "anomaly.close", "anomaly_alert", id, nil, u)
		return mcp.NewToolResultText(`{"status":"closed"}`), nil
	}
}
