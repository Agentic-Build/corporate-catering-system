package mcpserver_test

import (
	"sort"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/takalawang/corporate-catering-system/services/api/internal/mcpserver"
)

// TestMCPServer_RegistersExpectedTools is a structural smoke test: with empty
// Deps (no DB / no services), the constructor must still register all 8 MCP
// tools without nil-deref panics. Each handler defends against nil deps at
// call time, so registration itself is safe even when services aren't wired.
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
		"audit.query",
		"order.cancel",
		"order.get",
		"order.get_pickup_code",
		"order.list_mine",
		"order.place",
		"payroll.list_batches",
		"vendor.list",
	}
	assert.Equal(t, want, got, "MCP server must register exactly the 8 P7 tools")
}
