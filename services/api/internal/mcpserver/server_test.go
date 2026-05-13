package mcpserver_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/takalawang/corporate-catering-system/services/api/internal/mcpserver"
)

func TestMCPServer_BootEmpty(t *testing.T) {
	s := mcpserver.New(mcpserver.Deps{})
	require.NotNil(t, s)
}
