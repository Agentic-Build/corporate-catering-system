package hydra_test

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Agentic-Build/corporate-catering-system/services/api/internal/identity/hydra"
)

// errReadCloser yields one byte then a read error, so io.ReadAll fails.
type errReadCloser struct{ read bool }

func (e *errReadCloser) Read(p []byte) (int, error) {
	if e.read {
		return 0, io.ErrUnexpectedEOF
	}
	e.read = true
	if len(p) > 0 {
		p[0] = 'x'
	}
	return 1, nil
}
func (e *errReadCloser) Close() error { return nil }

// rtFunc adapts a function to http.RoundTripper.
type rtFunc func(*http.Request) (*http.Response, error)

func (f rtFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// TestSanitizingDCRProxy_ResponseReadError drives writeSanitizedResponse's
// io.ReadAll failure (and ServeHTTP's resulting 502) via a body that errors.
func TestSanitizingDCRProxy_ResponseReadError(t *testing.T) {
	proxy := &hydra.SanitizingDCRProxy{
		HydraURL: "http://hydra.local",
		HTTP: &http.Client{Transport: rtFunc(func(r *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": {"application/json"}},
				Body:       &errReadCloser{},
			}, nil
		})},
	}
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/oauth2/register", strings.NewReader(`{}`))
	proxy.ServeHTTP(rr, req)
	assert.Equal(t, http.StatusBadGateway, rr.Code)
}

// TestSanitizingDCRProxy_BadMethod hits buildUpstreamRequest's
// http.NewRequestWithContext error via an invalid HTTP method.
func TestSanitizingDCRProxy_BadMethod(t *testing.T) {
	proxy := &hydra.SanitizingDCRProxy{HydraURL: "http://hydra.local"}
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/oauth2/register", nil)
	req.Method = "BAD METHOD" // space is illegal in a method token
	proxy.ServeHTTP(rr, req)
	assert.Equal(t, http.StatusBadGateway, rr.Code)
}

// TestSanitizingDCRProxy_SkipsHostHeader ensures a "Host" entry in the header
// map is not forwarded (the EqualFold continue branch).
func TestSanitizingDCRProxy_SkipsHostHeader(t *testing.T) {
	var forwardedHostHeader string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		forwardedHostHeader = r.Header.Get("Host")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{}`))
	}))
	defer upstream.Close()

	proxy := &hydra.SanitizingDCRProxy{HydraURL: upstream.URL, HTTP: upstream.Client()}
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/oauth2/register", strings.NewReader(`{}`))
	req.Header["Host"] = []string{"evil.example.com"}
	proxy.ServeHTTP(rr, req)
	assert.Empty(t, forwardedHostHeader, "Host header must be skipped, not forwarded")
}

func TestSanitizingDCRProxy_ScrubsResponse(t *testing.T) {
	var seenBody map[string]any
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/oauth2/register", r.URL.Path)
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &seenBody)
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Custom", "passthrough")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{
			"client_id":"abc",
			"policy_uri":"",
			"tos_uri":"",
			"client_uri":"https://keep.me",
			"contacts":null,
			"audience":[],
			"allowed_cors_origins":[],
			"registration_access_token":"rat"
		}`))
	}))
	defer upstream.Close()

	proxy := &hydra.SanitizingDCRProxy{HydraURL: upstream.URL, HTTP: upstream.Client()}
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/oauth2/register",
		strings.NewReader(`{"client_name":"x","scope":"openid"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Host = "public.example.com"
	proxy.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusCreated, rr.Code)
	// Host header must not be forwarded as-is to upstream.
	var out map[string]any
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &out))
	assert.NotContains(t, out, "policy_uri")
	assert.NotContains(t, out, "tos_uri")
	assert.NotContains(t, out, "contacts")
	assert.NotContains(t, out, "audience")
	assert.NotContains(t, out, "allowed_cors_origins")
	assert.Equal(t, "https://keep.me", out["client_uri"])
	assert.Equal(t, "rat", out["registration_access_token"])
	// Non-Content-Length headers pass through; Content-Length is recomputed.
	assert.Equal(t, "passthrough", rr.Header().Get("X-Custom"))
	assert.Equal(t, itoa(len(rr.Body.Bytes())), rr.Header().Get("Content-Length"))

	// scope expansion added offline_access (request only had openid).
	scope, _ := seenBody["scope"].(string)
	assert.Contains(t, scope, "openid")
	assert.Contains(t, scope, "offline_access")
}

func TestSanitizingDCRProxy_NonJSONResponsePassthrough(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`policy_uri=""`))
	}))
	defer upstream.Close()

	proxy := &hydra.SanitizingDCRProxy{HydraURL: upstream.URL, HTTP: upstream.Client()}
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/oauth2/register/abc", nil)
	proxy.ServeHTTP(rr, req)
	assert.Equal(t, http.StatusOK, rr.Code)
	assert.Equal(t, `policy_uri=""`, rr.Body.String())
}

func TestSanitizingDCRProxy_ErrorStatusNotScrubbed(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"invalid","policy_uri":""}`))
	}))
	defer upstream.Close()

	proxy := &hydra.SanitizingDCRProxy{HydraURL: upstream.URL, HTTP: upstream.Client()}
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/oauth2/register", strings.NewReader(``))
	proxy.ServeHTTP(rr, req)
	assert.Equal(t, http.StatusBadRequest, rr.Code)
	// Error body passes through unscrubbed.
	assert.Contains(t, rr.Body.String(), `"policy_uri":""`)
}

func TestSanitizingDCRProxy_BadHydraURL(t *testing.T) {
	proxy := &hydra.SanitizingDCRProxy{HydraURL: "://bad-url"}
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/oauth2/register", strings.NewReader(`{}`))
	proxy.ServeHTTP(rr, req)
	assert.Equal(t, http.StatusBadGateway, rr.Code)
}

func TestSanitizingDCRProxy_UpstreamUnreachable(t *testing.T) {
	// nil HTTP forces http.DefaultClient; unreachable host → Do error.
	proxy := &hydra.SanitizingDCRProxy{HydraURL: "http://127.0.0.1:1"}
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/oauth2/register", strings.NewReader(`{}`))
	proxy.ServeHTTP(rr, req)
	assert.Equal(t, http.StatusBadGateway, rr.Code)
	assert.Contains(t, rr.Body.String(), "hydra DCR upstream")
}

func TestSanitizingDCRProxy_NilBody(t *testing.T) {
	var gotBody string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		gotBody = string(raw)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer upstream.Close()

	proxy := &hydra.SanitizingDCRProxy{HydraURL: upstream.URL, HTTP: upstream.Client()}
	rr := httptest.NewRecorder()
	// GET with explicit nil body to hit the r.Body == nil branch.
	req := httptest.NewRequest(http.MethodGet, "/oauth2/register/x", nil)
	req.Body = nil
	proxy.ServeHTTP(rr, req)
	assert.Equal(t, http.StatusOK, rr.Code)
	assert.Empty(t, gotBody)
}

func TestSanitizingDCRProxy_ScopeAlreadyComplete(t *testing.T) {
	var seenBody string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		seenBody = string(raw)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{}`))
	}))
	defer upstream.Close()

	proxy := &hydra.SanitizingDCRProxy{HydraURL: upstream.URL, HTTP: upstream.Client()}
	rr := httptest.NewRecorder()
	// Already has both required scopes → body forwarded unchanged.
	in := `{"scope":"openid offline_access"}`
	req := httptest.NewRequest(http.MethodPost, "/oauth2/register", strings.NewReader(in))
	proxy.ServeHTTP(rr, req)
	assert.JSONEq(t, in, seenBody)
}

func TestSanitizingDCRProxy_InvalidJSONBodyForwardedRaw(t *testing.T) {
	var seenBody string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		seenBody = string(raw)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{}`))
	}))
	defer upstream.Close()

	proxy := &hydra.SanitizingDCRProxy{HydraURL: upstream.URL, HTTP: upstream.Client()}
	rr := httptest.NewRecorder()
	// Non-JSON request body → expandRegistrationScope leaves it untouched.
	req := httptest.NewRequest(http.MethodPost, "/oauth2/register", strings.NewReader(`not json`))
	proxy.ServeHTTP(rr, req)
	assert.Equal(t, `not json`, seenBody)
}

func TestSanitizingDCRProxy_Invalid2xxJSONKeepsRaw(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		// 2xx + json content-type but malformed → sanitize fails, raw kept.
		_, _ = w.Write([]byte(`{bad json`))
	}))
	defer upstream.Close()

	proxy := &hydra.SanitizingDCRProxy{HydraURL: upstream.URL, HTTP: upstream.Client()}
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/oauth2/register", strings.NewReader(`{}`))
	proxy.ServeHTTP(rr, req)
	assert.Equal(t, http.StatusOK, rr.Code)
	assert.Equal(t, `{bad json`, rr.Body.String())
}

func TestSanitizingDCRProxy_TrailingSlashHydraURL(t *testing.T) {
	var seenPath string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{}`))
	}))
	defer upstream.Close()

	proxy := &hydra.SanitizingDCRProxy{HydraURL: upstream.URL + "/", HTTP: upstream.Client()}
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/oauth2/register", strings.NewReader(`{}`))
	proxy.ServeHTTP(rr, req)
	assert.Equal(t, "/oauth2/register", seenPath)
}
