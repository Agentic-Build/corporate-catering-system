package mcpserver_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/takalawang/corporate-catering-system/services/api/internal/identity"
	idhttp "github.com/takalawang/corporate-catering-system/services/api/internal/identity/http"
	"github.com/takalawang/corporate-catering-system/services/api/internal/mcpserver"
)

// callTool exercises the same code path the HTTP / stdio transports use:
// hands the MCP server a raw JSON-RPC tools/call message and returns the
// parsed response. ctx carries the authenticated user, mirroring what
// AuthMiddleware (HTTP) or StdioServer.SetContextFunc (stdio) inject.
func callTool(t *testing.T, ctx context.Context, srv any, name string, args map[string]any) mcp.JSONRPCMessage {
	t.Helper()
	payload := map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "tools/call",
		"params": map[string]any{
			"name":      name,
			"arguments": args,
		},
	}
	raw, err := json.Marshal(payload)
	require.NoError(t, err)
	type handler interface {
		HandleMessage(ctx context.Context, message json.RawMessage) mcp.JSONRPCMessage
	}
	return srv.(handler).HandleMessage(ctx, raw)
}

func toolResult(t *testing.T, msg mcp.JSONRPCMessage) *mcp.CallToolResult {
	t.Helper()
	jr, ok := msg.(mcp.JSONRPCResponse)
	require.Truef(t, ok, "expected JSON-RPC response, got %T", msg)
	result, ok := jr.Result.(*mcp.CallToolResult)
	require.Truef(t, ok, "expected *mcp.CallToolResult, got %T", jr.Result)
	return result
}

func toolText(t *testing.T, msg mcp.JSONRPCMessage) string {
	t.Helper()
	res := toolResult(t, msg)
	require.NotEmpty(t, res.Content)
	tc, ok := res.Content[0].(mcp.TextContent)
	require.True(t, ok)
	return tc.Text
}

func newEmployeeCtx() context.Context {
	plant := "F12B-3F"
	u := &identity.User{
		ID:           "user-1",
		PrimaryEmail: "e@tbite.test",
		Role:         identity.RoleEmployee,
		Status:       identity.StatusActive,
		Plant:        &plant,
	}
	return idhttp.ContextWithUser(context.Background(), u)
}

// TestSearch_RequiresQuery ensures the ChatGPT `search` tool validates that
// the query argument is present — ChatGPT will sometimes call with an empty
// payload during connector probing and we want a clean error, not a crash.
func TestSearch_RequiresQuery(t *testing.T) {
	srv := mcpserver.New(mcpserver.Deps{})
	resp := callTool(t, newEmployeeCtx(), srv, "search", map[string]any{})
	res := toolResult(t, resp)
	assert.True(t, res.IsError)
}

// TestSearch_AnonymousRejected ensures anonymous callers see an "not
// authenticated" tool error (the HTTP layer already returns 401 separately;
// at the tool layer we surface the error so stdio clients see it too).
func TestSearch_AnonymousRejected(t *testing.T) {
	srv := mcpserver.New(mcpserver.Deps{})
	resp := callTool(t, context.Background(), srv, "search", map[string]any{"query": "lunch"})
	res := toolResult(t, resp)
	assert.True(t, res.IsError)
	assert.Contains(t, toolText(t, resp), "not authenticated")
}

// TestSearch_EmptyResultsWhenNoServices verifies that with nil Deps (no
// menu, no order service) the search tool returns an empty results array
// rather than panicking — the ChatGPT connector contract requires a valid
// JSON object on every call.
func TestSearch_EmptyResultsWhenNoServices(t *testing.T) {
	srv := mcpserver.New(mcpserver.Deps{})
	resp := callTool(t, newEmployeeCtx(), srv, "search", map[string]any{"query": "lunch"})
	text := toolText(t, resp)
	var out map[string]any
	require.NoError(t, json.Unmarshal([]byte(text), &out))
	results, _ := out["results"].([]any)
	assert.Empty(t, results)
}

// TestFetch_UnknownPrefix verifies the fetch tool rejects IDs that don't
// have one of the recognised prefixes (menu:, order:, vendor:). ChatGPT
// sometimes passes back URL-encoded or otherwise mangled IDs; surfacing a
// clear error helps the model self-correct.
func TestFetch_UnknownPrefix(t *testing.T) {
	srv := mcpserver.New(mcpserver.Deps{})
	resp := callTool(t, newEmployeeCtx(), srv, "fetch", map[string]any{"id": "garbage-id"})
	res := toolResult(t, resp)
	assert.True(t, res.IsError)
	assert.Contains(t, toolText(t, resp), "unknown id prefix")
}

// TestMenuListForDay_RequiresPlant guards against the regression where a
// user without a home plant configured can still call menu.list_for_day —
// without plant we have no way to scope results so the tool must refuse.
func TestMenuListForDay_RequiresPlant(t *testing.T) {
	srv := mcpserver.New(mcpserver.Deps{})
	u := &identity.User{
		ID:     "user-2",
		Role:   identity.RoleEmployee,
		Status: identity.StatusActive,
		// Plant intentionally nil.
	}
	ctx := idhttp.ContextWithUser(context.Background(), u)
	resp := callTool(t, ctx, srv, "menu.list_for_day", map[string]any{})
	res := toolResult(t, resp)
	assert.True(t, res.IsError)
	assert.Contains(t, toolText(t, resp), "plant required")
}

// TestMenuSearch_RoleGate confirms that callers without the employee or
// welfare_admin role get a clean role-rejection error — vendor operators
// must not be able to enumerate the global menu via MCP.
func TestMenuSearch_RoleGate(t *testing.T) {
	srv := mcpserver.New(mcpserver.Deps{})
	u := &identity.User{
		ID:     "vendor-op-1",
		Role:   identity.RoleVendorOperator,
		Status: identity.StatusActive,
	}
	ctx := idhttp.ContextWithUser(context.Background(), u)
	resp := callTool(t, ctx, srv, "menu.search", map[string]any{"query": "sushi"})
	res := toolResult(t, resp)
	assert.True(t, res.IsError)
	assert.Contains(t, toolText(t, resp), "cannot read menu")
}
