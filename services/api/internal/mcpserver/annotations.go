// Package mcpserver — tool annotation presets.
//
// MCP `tools/list` includes per-tool annotations the host model uses to
// decide whether to surface a confirmation prompt before invoking the
// tool (see modelcontextprotocol.io spec — ToolAnnotations).
//
// mcp-go defaults to the most pessimistic combination
// (readOnly=false, destructive=true, idempotent=false, openWorld=true).
// Claude treats `destructive=true` as "ask the user first", so without
// these presets every menu.search and order.get prompts for confirmation —
// pure friction with no safety benefit. The classifications below are
// chosen per-tool to match the actual side-effects of each operation.
package mcpserver

import "github.com/mark3labs/mcp-go/mcp"

// annoReadOnly is for tools that don't change any state: list, search,
// get, query. Idempotent by definition, no destructive risk, scope is
// limited to the T-Bite domain (openWorld=false).
func annoReadOnly() mcp.ToolOption {
	return mcp.WithToolAnnotation(mcp.ToolAnnotation{
		ReadOnlyHint:    boolPtr(true),
		IdempotentHint:  boolPtr(true),
		DestructiveHint: boolPtr(false),
		OpenWorldHint:   boolPtr(false),
	})
}

// annoCreate is for tools that add new rows but never modify or delete
// existing ones: order.place, feedback.rate_order, feedback.file_complaint.
// Not idempotent (each call inserts a fresh row), not destructive.
func annoCreate() mcp.ToolOption {
	return mcp.WithToolAnnotation(mcp.ToolAnnotation{
		ReadOnlyHint:    boolPtr(false),
		IdempotentHint:  boolPtr(false),
		DestructiveHint: boolPtr(false),
		OpenWorldHint:   boolPtr(false),
	})
}

// annoStateChange is for tools that flip an existing row's state — order
// modify, payroll dispute resolve, document.review, anomaly.triage. The
// final state is reached idempotently (same args twice ends in the same
// place) and the change is destructive in the MCP sense (overwrites
// existing data).
func annoStateChange() mcp.ToolOption {
	return mcp.WithToolAnnotation(mcp.ToolAnnotation{
		ReadOnlyHint:    boolPtr(false),
		IdempotentHint:  boolPtr(true),
		DestructiveHint: boolPtr(true),
		OpenWorldHint:   boolPtr(false),
	})
}

// annoHighRiskAdmin is for irreversible-ish admin actions: vendor.suspend,
// payroll.lock_batch, settlement.close_period, anomaly.close. Same shape
// as annoStateChange — the distinct preset documents intent so adding
// a future "require human confirmation" gate is a one-grep change.
func annoHighRiskAdmin() mcp.ToolOption {
	return mcp.WithToolAnnotation(mcp.ToolAnnotation{
		ReadOnlyHint:    boolPtr(false),
		IdempotentHint:  boolPtr(true),
		DestructiveHint: boolPtr(true),
		OpenWorldHint:   boolPtr(false),
	})
}

// annoReversible is for tools whose effect can be undone by calling the
// inverse tool — vendor.reinstate (inverse of suspend), order.cancel
// (employees can re-place if before cutoff). MCP doesn't have a "reversible"
// hint per se, but classifying these as non-destructive matches the spec's
// definition: they don't permanently destroy data.
func annoReversible() mcp.ToolOption {
	return mcp.WithToolAnnotation(mcp.ToolAnnotation{
		ReadOnlyHint:    boolPtr(false),
		IdempotentHint:  boolPtr(true),
		DestructiveHint: boolPtr(false),
		OpenWorldHint:   boolPtr(false),
	})
}

func boolPtr(b bool) *bool { return &b }
