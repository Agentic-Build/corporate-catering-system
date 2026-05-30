package httpserver

import (
	"context"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	idhttp "github.com/Agentic-Build/corporate-catering-system/services/api/internal/identity/http"
)

func newListener(t *testing.T) (net.Listener, error) {
	t.Helper()
	return net.Listen("tcp", "127.0.0.1:0")
}

func TestLivenessHandler(t *testing.T) {
	h := NewHealth()

	t.Run("live returns ok", func(t *testing.T) {
		rr := httptest.NewRecorder()
		h.LivenessHandler()(rr, httptest.NewRequest(http.MethodGet, "/healthz", nil))
		assert.Equal(t, http.StatusOK, rr.Code)
		assert.Equal(t, "application/json", rr.Header().Get("content-type"))
		assert.JSONEq(t, `{"status":"ok"}`, rr.Body.String())
	})

	t.Run("not live returns 503 draining", func(t *testing.T) {
		h.SetLive(false)
		rr := httptest.NewRecorder()
		h.LivenessHandler()(rr, httptest.NewRequest(http.MethodGet, "/healthz", nil))
		assert.Equal(t, http.StatusServiceUnavailable, rr.Code)
		assert.JSONEq(t, `{"status":"draining"}`, rr.Body.String())
	})
}

func TestDrainHandlerDelayPaths(t *testing.T) {
	t.Run("delay elapses then responds", func(t *testing.T) {
		h := NewHealth()
		rr := httptest.NewRecorder()
		h.DrainHandler(10*time.Millisecond)(rr, httptest.NewRequest(http.MethodGet, "/drainz", nil))
		assert.Equal(t, http.StatusOK, rr.Code)
		assert.JSONEq(t, `{"status":"draining"}`, rr.Body.String())
		assert.False(t, h.ready.Load())
	})

	t.Run("context cancelled aborts before write", func(t *testing.T) {
		h := NewHealth()
		ctx, cancel := context.WithCancel(context.Background())
		cancel() // already done → select takes ctx.Done() branch immediately
		req := httptest.NewRequest(http.MethodGet, "/drainz", nil).WithContext(ctx)
		rr := httptest.NewRecorder()
		h.DrainHandler(time.Hour)(rr, req)
		// Body not written because the handler returned on ctx.Done().
		assert.Empty(t, rr.Body.String())
		assert.False(t, h.ready.Load())
	})
}

// TestMCPAuthEnforce_EmptyOptsRealm exercises the fallback WWW-Authenticate
// branch when no OAuth metadata is configured (PublicBaseURL empty).
func TestMCPAuthEnforce_EmptyOptsRealm(t *testing.T) {
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	h := mcpAuthEnforce(next, MCPOpts{}, "/mcp")

	req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(`{}`))
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusUnauthorized, rr.Code)
	assert.Equal(t, `Bearer realm="mcp"`, rr.Header().Get("WWW-Authenticate"))
	assert.Contains(t, rr.Body.String(), "unauthorized")
}

func TestServerRunShutsDownOnContextCancel(t *testing.T) {
	srv := New(":0",
		slog.New(slog.NewTextHandler(io.Discard, nil)),
		&idhttp.API{}, nil, nil, MCPOpts{})

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- srv.Run(ctx) }()

	// Give the goroutine a moment to enter ListenAndServe, then cancel.
	cancel()
	select {
	case err := <-done:
		assert.NoError(t, err)
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not return after context cancel")
	}
}

func TestServerRunReturnsListenError(t *testing.T) {
	// Bind a port, then start a second server on the same addr so
	// ListenAndServe fails and Run returns via the errCh branch.
	ln, err := newListener(t)
	require.NoError(t, err)
	defer ln.Close()

	srv := New(ln.Addr().String(),
		slog.New(slog.NewTextHandler(io.Discard, nil)),
		&idhttp.API{}, nil, nil, MCPOpts{})

	err = srv.Run(context.Background())
	assert.Error(t, err)
}

// extraRoutesAndNilHealthCovered ensures the chi extraRoutes branch in
// newServer runs (covering the `extraRoutes != nil` true path) alongside the
// default health handlers.
func TestNewWithExtraRoutesAndDefaultHealth(t *testing.T) {
	hit := false
	srv := New(":0",
		slog.New(slog.NewTextHandler(io.Discard, nil)),
		&idhttp.API{},
		func(r chi.Router) {
			hit = true
			r.Get("/custom", func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusTeapot)
			})
		},
		nil, MCPOpts{})
	assert.True(t, hit)

	rr := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/custom", nil))
	assert.Equal(t, http.StatusTeapot, rr.Code)

	// Default healthz handler (health==nil branch).
	rr = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	assert.Equal(t, http.StatusOK, rr.Code)
}

// TestNewWithAPIBuilder exercises the apiBuilders loop in newServer: the
// builder must be invoked against the shared huma.API and its registered
// operation must be routable.
func TestNewWithAPIBuilder(t *testing.T) {
	built := false
	srv := New(":0",
		slog.New(slog.NewTextHandler(io.Discard, nil)),
		&idhttp.API{}, nil, nil, MCPOpts{},
		func(api huma.API) {
			built = true
			huma.Get(api, "/ping", func(_ context.Context, _ *struct{}) (*struct {
				Body struct {
					Pong bool `json:"pong"`
				}
			}, error) {
				resp := &struct {
					Body struct {
						Pong bool `json:"pong"`
					}
				}{}
				resp.Body.Pong = true
				return resp, nil
			})
		})
	assert.True(t, built)

	rr := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/ping", nil))
	assert.Equal(t, http.StatusOK, rr.Code)
	assert.Contains(t, rr.Body.String(), "pong")
}
