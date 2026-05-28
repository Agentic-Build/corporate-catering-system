// Tool annotation presets (mcp-go defaults are pessimistic and trigger a
// confirmation prompt on every tool — including read-only ones). The presets
// below classify each tool to its actual side-effects.
package mcpserver

import "github.com/mark3labs/mcp-go/mcp"

// annoReadOnly: list/search/get/query — no state change.
func annoReadOnly() mcp.ToolOption {
	return mcp.WithToolAnnotation(mcp.ToolAnnotation{
		ReadOnlyHint:    boolPtr(true),
		IdempotentHint:  boolPtr(true),
		DestructiveHint: boolPtr(false),
		OpenWorldHint:   boolPtr(false),
	})
}

// annoCreate: inserts new rows only (not idempotent, not destructive).
// order.place, feedback.rate_order, feedback.file_complaint.
func annoCreate() mcp.ToolOption {
	return mcp.WithToolAnnotation(mcp.ToolAnnotation{
		ReadOnlyHint:    boolPtr(false),
		IdempotentHint:  boolPtr(false),
		DestructiveHint: boolPtr(false),
		OpenWorldHint:   boolPtr(false),
	})
}

// annoStateChange: flips an existing row's state (idempotent, destructive in
// the MCP sense). order.modify, payroll.dispute_resolve, document.review,
// anomaly.triage.
func annoStateChange() mcp.ToolOption {
	return mcp.WithToolAnnotation(mcp.ToolAnnotation{
		ReadOnlyHint:    boolPtr(false),
		IdempotentHint:  boolPtr(true),
		DestructiveHint: boolPtr(true),
		OpenWorldHint:   boolPtr(false),
	})
}

// annoHighRiskAdmin: irreversible-ish admin actions (vendor.suspend, payroll.lock_batch,
// settlement.close_period, anomaly.close). Shape matches annoStateChange — distinct
// preset so a future "require human confirmation" gate is a one-grep change.
func annoHighRiskAdmin() mcp.ToolOption {
	return mcp.WithToolAnnotation(mcp.ToolAnnotation{
		ReadOnlyHint:    boolPtr(false),
		IdempotentHint:  boolPtr(true),
		DestructiveHint: boolPtr(true),
		OpenWorldHint:   boolPtr(false),
	})
}

// annoReversible: undoable via an inverse tool (vendor.reinstate, order.cancel).
// Classified non-destructive — no permanent data loss.
func annoReversible() mcp.ToolOption {
	return mcp.WithToolAnnotation(mcp.ToolAnnotation{
		ReadOnlyHint:    boolPtr(false),
		IdempotentHint:  boolPtr(true),
		DestructiveHint: boolPtr(false),
		OpenWorldHint:   boolPtr(false),
	})
}

func boolPtr(b bool) *bool { return &b }
