package mcpserver

import (
	"context"
	"testing"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/stretchr/testify/assert"
)

// elapsedForID has three early-return guards that the framework-driven happy
// path never reaches (id is always a non-nil time.Time there): a nil id, an id
// with no stored start time, and a stored value of the wrong type.
func TestElapsedForID_Guards(t *testing.T) {
	assert.Equal(t, 0.0, elapsedForID(nil), "nil id → 0")

	assert.Equal(t, 0.0, elapsedForID("never-stored"), "unknown id → 0")

	// Wrong type stored under the key → the type assertion fails → 0.
	startTimes.Store("wrong-type", "not-a-time")
	assert.Equal(t, 0.0, elapsedForID("wrong-type"), "non-time value → 0")

	// Happy path: a stored time yields a non-negative elapsed and consumes the key.
	startTimes.Store("real", time.Now().Add(-10*time.Millisecond))
	got := elapsedForID("real")
	assert.Greater(t, got, 0.0)
	_, stillThere := startTimes.Load("real")
	assert.False(t, stillThere, "LoadAndDelete must remove the key")
}

// buildMetricsHooks' BeforeCallTool returns early when id is nil — exercise it
// directly since the framework never invokes the hook with a nil id.
func TestBuildMetricsHooks_NilIDGuards(t *testing.T) {
	h := buildMetricsHooks()
	// BeforeCallTool with nil id must be a no-op (no panic, nothing stored).
	for _, cb := range h.OnBeforeCallTool {
		cb(context.Background(), nil, &mcp.CallToolRequest{})
	}
	// AfterCallTool with nil id + nil req must not panic and reports a metric.
	for _, cb := range h.OnAfterCallTool {
		cb(context.Background(), nil, nil, nil)
	}
}

// toolCallOutcome maps an error result to "tool_error" and everything else to
// "success", including non-CallToolResult values.
func TestToolCallOutcome(t *testing.T) {
	assert.Equal(t, "tool_error", toolCallOutcome(&mcp.CallToolResult{IsError: true}))
	assert.Equal(t, "success", toolCallOutcome(&mcp.CallToolResult{IsError: false}))
	assert.Equal(t, "success", toolCallOutcome(nil))
	assert.Equal(t, "success", toolCallOutcome("not-a-result"))
}

// sideEffectFor classifies by the verb after the dot; names without a dot are
// "unknown", and the empty name is "unknown".
func TestSideEffectFor(t *testing.T) {
	cases := map[string]string{
		"":                   "unknown",
		"nodot":              "unknown",
		"order.list_mine":    "read_only",
		"audit.query":        "read_only",
		"order.place":        "write",
		"order.cancel":       "state_change",
		"payroll.lock_batch": "state_change",
		"anomaly.triage":     "state_change",
	}
	for name, want := range cases {
		assert.Equalf(t, want, sideEffectFor(name), "sideEffectFor(%q)", name)
	}
}

// clientFromCtx returns "anonymous" without an auth context.
func TestClientFromCtx_Anonymous(t *testing.T) {
	assert.Equal(t, "anonymous", clientFromCtx(context.Background()))
}
