package hydra_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Agentic-Build/corporate-catering-system/services/api/internal/identity/hydra"
)

// TestDiscoveryShim_InjectsRegistrationEndpoint pins the fix for Hydra
// v2.2/v2.3's missing registration_endpoint in the OIDC discovery doc.
// Without this patch, MCP clients fail at the OAuth-discovery step and
// fall back to manual configuration — defeating DCR.
func TestDiscoveryShim_InjectsRegistrationEndpoint(t *testing.T) {
	// Fake "Hydra" that returns a discovery document missing the
	// registration_endpoint, mimicking the real bug.
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/.well-known/openid-configuration" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(`{
			"issuer": "http://localhost:8080/",
			"authorization_endpoint": "http://localhost:8080/oauth2/auth",
			"token_endpoint": "http://localhost:8080/oauth2/token",
			"jwks_uri": "http://localhost:8080/.well-known/jwks.json"
		}`))
	}))
	defer upstream.Close()

	shim := hydra.NewDiscoveryShim(upstream.URL)
	doc, err := shim.Doc(context.Background())
	require.NoError(t, err)

	var parsed map[string]any
	require.NoError(t, json.Unmarshal(doc, &parsed))

	regEndpoint, _ := parsed["registration_endpoint"].(string)
	assert.Equal(t, upstream.URL+"/oauth2/register", regEndpoint,
		"discovery shim must inject the DCR endpoint")

	// Upstream fields must survive unchanged.
	assert.Equal(t, "http://localhost:8080/", parsed["issuer"])
	assert.Equal(t, "http://localhost:8080/oauth2/auth", parsed["authorization_endpoint"])
}

// TestDiscoveryShim_ServesAsHandler covers the http.Handler shape used by
// the chi router: the shim must emit application/json and return the same
// patched body via ServeHTTP.
func TestDiscoveryShim_ServesAsHandler(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"issuer":"http://x/"}`))
	}))
	defer upstream.Close()

	shim := hydra.NewDiscoveryShim(upstream.URL)
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/.well-known/openid-configuration", nil)
	shim.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)
	assert.Equal(t, "application/json", rr.Header().Get("Content-Type"))

	var parsed map[string]any
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &parsed))
	assert.Contains(t, parsed, "registration_endpoint")
}

// TestReverseProxy_PreservesHostHeader ensures the proxy rewrites Host
// before forwarding so Hydra's internal URL resolver doesn't echo back
// the original request host as the issuer.
func TestReverseProxy_PreservesHostHeader(t *testing.T) {
	var seenHost string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenHost = r.Host
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	proxy, err := hydra.ReverseProxy(upstream.URL)
	require.NoError(t, err)

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/oauth2/auth", nil)
	req.Host = "api.example.com"
	proxy.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)
	assert.NotEqual(t, "api.example.com", seenHost,
		"proxy must rewrite Host to upstream so Hydra's issuer logic is stable")
}
