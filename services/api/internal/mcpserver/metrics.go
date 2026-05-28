// Package mcpserver — observability hooks.
//
// Every tool invocation goes through mcp-go's BeforeCallTool / AfterCallTool
// hooks so we can record latency + outcome + side-effect classification
// uniformly across the 21 tools. The hooks key per-request start times by
// the request id (which is unique within a session) so concurrent calls don't
// mix up timings.
//
// Side-effect classification is derived from the tool name. Each name follows
// the `<domain>.<verb>` convention and the verb (list / get / search vs.
// place / cancel / ...) maps cleanly to read_only / write / state_change.
package mcpserver

import (
	"context"
	"strings"
	"sync"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	idhttp "github.com/Agentic-Build/corporate-catering-system/services/api/internal/identity/http"
	"github.com/Agentic-Build/corporate-catering-system/services/api/internal/platform/observability"
)

// startTimes carries the BeforeCallTool timestamp keyed by request id so
// AfterCallTool can compute the elapsed time. Bounded by concurrency of
// in-flight MCP tool calls per process (a few hundred at most under any
// realistic load), so a sync.Map without explicit eviction is fine.
var startTimes sync.Map

// elapsedForID returns the seconds since BeforeCallTool stored id's start time.
func elapsedForID(id any) float64 {
	if id == nil {
		return 0
	}
	v, ok := startTimes.LoadAndDelete(id)
	if !ok {
		return 0
	}
	t, ok := v.(time.Time)
	if !ok {
		return 0
	}
	return time.Since(t).Seconds()
}

// toolCallOutcome classifies the tool call result for the metric label.
func toolCallOutcome(result any) string {
	if r, ok := result.(*mcp.CallToolResult); ok && r != nil && r.IsError {
		return "tool_error"
	}
	return "success"
}

func buildMetricsHooks() *server.Hooks {
	h := &server.Hooks{}
	h.AddBeforeCallTool(func(_ context.Context, id any, _ *mcp.CallToolRequest) {
		if id == nil {
			return
		}
		startTimes.Store(id, time.Now())
	})
	h.AddAfterCallTool(func(ctx context.Context, id any, req *mcp.CallToolRequest, result any) {
		toolName := ""
		if req != nil {
			toolName = req.Params.Name
		}
		observability.MCPToolCall(ctx, toolName, clientFromCtx(ctx), toolCallOutcome(result), sideEffectFor(toolName), elapsedForID(id), nil)
	})
	return h
}

// clientFromCtx returns a stable identifier for the calling actor. We prefer
// the authenticated user role+id (so dashboards can spot a specific user that
// hammers a tool); when no auth context is attached (early in the request
// lifecycle, or for protocol-level calls that aren't wrapped in our HTTP
// middleware) we return "anonymous".
func clientFromCtx(ctx context.Context) string {
	if u, ok := idhttp.UserFromContext(ctx); ok && u != nil {
		return string(u.Role) + ":" + u.ID
	}
	return "anonymous"
}

// sideEffectFor classifies a tool by name. The names follow strict
// `<domain>.<verb>[_qualifier]` so a prefix match on the verb suffices.
func sideEffectFor(toolName string) string {
	if toolName == "" {
		return "unknown"
	}
	dot := strings.IndexByte(toolName, '.')
	if dot < 0 {
		return "unknown"
	}
	verb := toolName[dot+1:]
	switch {
	case strings.HasPrefix(verb, "list"),
		strings.HasPrefix(verb, "get"),
		strings.HasPrefix(verb, "search"),
		strings.HasPrefix(verb, "query"),
		strings.HasPrefix(verb, "show"),
		strings.HasPrefix(verb, "recent"),
		strings.HasPrefix(verb, "today"),
		strings.HasPrefix(verb, "current"):
		return "read_only"
	case strings.HasPrefix(verb, "cancel"),
		strings.HasPrefix(verb, "reject"),
		strings.HasPrefix(verb, "suspend"),
		strings.HasPrefix(verb, "void"),
		strings.HasPrefix(verb, "close"),
		strings.HasPrefix(verb, "lock"),
		strings.HasPrefix(verb, "reverse"),
		strings.HasPrefix(verb, "resolve"),
		strings.HasPrefix(verb, "delete"),
		strings.HasPrefix(verb, "triage"):
		return "state_change"
	default:
		return "write"
	}
}
