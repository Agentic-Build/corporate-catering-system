package hydra_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
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

// TestDiscoveryShim_PublicBaseURL pins the registration_endpoint under the
// public host (not Hydra) and the S256 PKCE default injection.
func TestDiscoveryShim_PublicBaseURL(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"issuer":"http://hydra/"}`))
	}))
	defer upstream.Close()

	shim := hydra.NewDiscoveryShim(upstream.URL)
	shim.PublicBaseURL = "https://public.example.com"
	doc, err := shim.Doc(context.Background())
	require.NoError(t, err)

	var parsed map[string]any
	require.NoError(t, json.Unmarshal(doc, &parsed))
	assert.Equal(t, "https://public.example.com/oauth2/register", parsed["registration_endpoint"])
	assert.Equal(t, []any{"S256"}, parsed["code_challenge_methods_supported"])
}

// Upstream already advertising PKCE methods → shim must not overwrite them.
func TestDiscoveryShim_PreservesExistingPKCE(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"issuer":"http://x/","code_challenge_methods_supported":["plain"]}`))
	}))
	defer upstream.Close()

	shim := hydra.NewDiscoveryShim(upstream.URL)
	doc, err := shim.Doc(context.Background())
	require.NoError(t, err)
	var parsed map[string]any
	require.NoError(t, json.Unmarshal(doc, &parsed))
	assert.Equal(t, []any{"plain"}, parsed["code_challenge_methods_supported"])
}

// Second call within the TTL must hit the cache (no second upstream request).
func TestDiscoveryShim_CacheHit(t *testing.T) {
	var hits int
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits++
		_, _ = w.Write([]byte(`{"issuer":"http://x/"}`))
	}))
	defer upstream.Close()

	shim := hydra.NewDiscoveryShim(upstream.URL)
	_, err := shim.Doc(context.Background())
	require.NoError(t, err)
	_, err = shim.Doc(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 1, hits, "second Doc call must be served from cache")
}

func TestDiscoveryShim_UpstreamErrorStatus(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer upstream.Close()

	shim := hydra.NewDiscoveryShim(upstream.URL)
	_, err := shim.Doc(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "503")
}

func TestDiscoveryShim_UpstreamUnreachable(t *testing.T) {
	shim := hydra.NewDiscoveryShim("http://127.0.0.1:1")
	_, err := shim.Doc(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "discovery fetch")
}

func TestDiscoveryShim_InvalidJSON(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{not json`))
	}))
	defer upstream.Close()

	shim := hydra.NewDiscoveryShim(upstream.URL)
	_, err := shim.Doc(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "decode")
}

func TestDiscoveryShim_BadRequestBuild(t *testing.T) {
	shim := hydra.NewDiscoveryShim("http://\x7f/")
	_, err := shim.Doc(context.Background())
	require.Error(t, err)
}

// ServeHTTP error leg: upstream down → 502 + stub body.
func TestDiscoveryShim_ServeHTTP_BadGateway(t *testing.T) {
	shim := hydra.NewDiscoveryShim("http://127.0.0.1:1")
	rr := httptest.NewRecorder()
	shim.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/.well-known/openid-configuration", nil))
	assert.Equal(t, http.StatusBadGateway, rr.Code)
	assert.Equal(t, "application/json", rr.Header().Get("Content-Type"))
	assert.Contains(t, rr.Body.String(), "hydra unavailable")
}

// discErrBody yields a read error so Doc's io.ReadAll fails.
type discErrBody struct{}

func (discErrBody) Read([]byte) (int, error) { return 0, io.ErrUnexpectedEOF }
func (discErrBody) Close() error             { return nil }

type discRT func(*http.Request) (*http.Response, error)

func (f discRT) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// TestDiscoveryShim_Concurrent exercises Doc under concurrent callers; the
// RWMutex + cache must serialise correctly and only fetch upstream once.
func TestDiscoveryShim_Concurrent(t *testing.T) {
	var hits int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&hits, 1)
		_, _ = w.Write([]byte(`{"issuer":"http://x/"}`))
	}))
	defer upstream.Close()

	shim := hydra.NewDiscoveryShim(upstream.URL)
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := shim.Doc(context.Background())
			assert.NoError(t, err)
		}()
	}
	wg.Wait()
	assert.Equal(t, int32(1), atomic.LoadInt32(&hits),
		"concurrent Doc callers must share a single upstream fetch")
}

func TestDiscoveryShim_BodyReadError(t *testing.T) {
	shim := hydra.NewDiscoveryShim("http://hydra.local")
	shim.HTTP = &http.Client{Transport: discRT(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Body: discErrBody{}, Header: http.Header{}}, nil
	})}
	_, err := shim.Doc(context.Background())
	require.Error(t, err)
}

func TestReverseProxy_BadURL(t *testing.T) {
	_, err := hydra.ReverseProxy("://not-a-url")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "parse hydra url")
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
