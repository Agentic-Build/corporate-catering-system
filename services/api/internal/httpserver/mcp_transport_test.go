package httpserver_test

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Agentic-Build/corporate-catering-system/services/api/internal/httpserver"
	idhttp "github.com/Agentic-Build/corporate-catering-system/services/api/internal/identity/http"
	"github.com/Agentic-Build/corporate-catering-system/services/api/internal/mcpserver"
)

// boot constructs the production httpserver wiring with empty Deps + idAPI
// so we can exercise the MCP transport (auth, CORS, OAuth discovery) without
// a database. Returns the chi router behind an httptest.Server.
func boot(t *testing.T, opts httpserver.MCPOpts) *httptest.Server {
	t.Helper()
	idAPI := &idhttp.API{}
	mcp := mcpserver.New(mcpserver.Deps{})
	hs := httpserver.New(":0",
		slog.New(slog.NewTextHandler(io.Discard, nil)),
		idAPI,
		func(_ chi.Router) {},
		httpserver.MCP{Server: mcp, Opts: opts},
	)
	require.NotNil(t, hs)
	return httptest.NewServer(hs.Handler())
}

// TestMCP_UnauthorizedReturns401 calls /mcp without an Authorization header
// and asserts the response is 401 with a WWW-Authenticate header that
// includes the resource_metadata URL. This is the discovery handshake
// Claude.ai and ChatGPT remote MCP rely on.
func TestMCP_UnauthorizedReturns401(t *testing.T) {
	ts := boot(t, httpserver.MCPOpts{
		PublicBaseURL:        "http://test.example",
		AuthorizationServers: []string{"http://idp.example/issuer"},
	})
	defer ts.Close()

	body := bytes.NewReader([]byte(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`))
	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/mcp", body)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
	wwwAuth := resp.Header.Get("WWW-Authenticate")
	assert.Contains(t, wwwAuth, "Bearer")
	assert.Contains(t, wwwAuth, "resource_metadata=")
	assert.Contains(t, wwwAuth, "/.well-known/oauth-protected-resource")
}

// TestMCP_OAuthMetadataIsServed verifies the RFC 9728 endpoint returns the
// configured issuer URL, so MCP clients can discover where to authenticate
// without first hitting a 401.
func TestMCP_OAuthMetadataIsServed(t *testing.T) {
	const issuer = "http://idp.example/application/o/tbite/"
	ts := boot(t, httpserver.MCPOpts{
		PublicBaseURL:        "http://test.example",
		AuthorizationServers: []string{issuer},
	})
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/.well-known/oauth-protected-resource")
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Contains(t, resp.Header.Get("Content-Type"), "application/json")

	var meta map[string]any
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&meta))
	assert.Equal(t, "http://test.example/mcp", meta["resource"])
	auths, ok := meta["authorization_servers"].([]any)
	require.True(t, ok)
	require.Len(t, auths, 1)
	assert.Equal(t, issuer, auths[0])
	methods, ok := meta["bearer_methods_supported"].([]any)
	require.True(t, ok)
	assert.Contains(t, methods, "header")
}

// TestMCP_CORSPreflight verifies an OPTIONS preflight on /mcp gets a 200
// with proper Access-Control-* headers, so browser-based MCP playgrounds
// (and OpenAI's connector UI) can connect without being blocked.
func TestMCP_CORSPreflight(t *testing.T) {
	ts := boot(t, httpserver.MCPOpts{})
	defer ts.Close()

	req, _ := http.NewRequest(http.MethodOptions, ts.URL+"/mcp", nil)
	req.Header.Set("Origin", "https://chat.openai.com")
	req.Header.Set("Access-Control-Request-Method", "POST")
	req.Header.Set("Access-Control-Request-Headers", "content-type,authorization")

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Less(t, resp.StatusCode, 300, "preflight must succeed")
	assert.Equal(t, "*", resp.Header.Get("Access-Control-Allow-Origin"))
	allowHeaders := strings.ToLower(resp.Header.Get("Access-Control-Allow-Headers"))
	assert.Contains(t, allowHeaders, "authorization")
}

// TestMCP_SSETransportNotMounted locks the HTTP MCP surface to the
// Streamable HTTP endpoint only.
func TestMCP_SSETransportNotMounted(t *testing.T) {
	ts := boot(t, httpserver.MCPOpts{})
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/mcp/sse")
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusNotFound, resp.StatusCode)
}
