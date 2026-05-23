package mcpserver_test

import (
	"sort"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/takalawang/corporate-catering-system/services/api/internal/mcpserver"
)

// TestMCPServer_RegistersExpectedTools is a structural smoke test: with empty
// Deps (no DB / no services), the constructor must still register every MCP
// tool without nil-deref panics. Each handler defends against nil deps at
// call time, so registration itself is safe even when services aren't wired.
//
// Tool inventory (26 total):
//   - 5 order · 3 menu (list_for_day, search, get_item) · 1 vendor discovery
//   - 2 ChatGPT-convention (search, fetch)
//   - 3 vendor admin · 3 payroll · 1 audit · 2 feedback
//   - 1 settlement · 5 compliance
func TestMCPServer_RegistersExpectedTools(t *testing.T) {
	s := mcpserver.New(mcpserver.Deps{})
	require.NotNil(t, s)

	tools := s.ListTools()
	got := make([]string, 0, len(tools))
	for name := range tools {
		got = append(got, name)
	}
	sort.Strings(got)

	want := []string{
		"anomaly.close",
		"anomaly.list",
		"anomaly.triage",
		"audit.query",
		"document.list",
		"document.review",
		"feedback.file_complaint",
		"feedback.rate_order",
		"fetch",
		"menu.get_item",
		"menu.list_for_day",
		"menu.search",
		"order.cancel",
		"order.get",
		"order.list_mine",
		"order.modify",
		"order.place",
		"payroll.list_batches",
		"payroll.lock_batch",
		"payroll.resolve_dispute",
		"search",
		"settlement.close_period",
		"vendor.list",
		"vendor.list_open",
		"vendor.reinstate",
		"vendor.suspend",
	}
	assert.Equal(t, want, got, "MCP server must register every MCP tool")
}
