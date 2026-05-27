package httpserver

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humachi"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	mcpsrv "github.com/mark3labs/mcp-go/server"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel/attribute"

	idhttp "github.com/takalawang/corporate-catering-system/services/api/internal/identity/http"
	"github.com/takalawang/corporate-catering-system/services/api/internal/platform/observability"
)

type Server struct {
	addr   string
	logger *slog.Logger
	srv    *http.Server
}

// MCPOpts carries configuration for the MCP transport mount. Empty fields
// degrade gracefully: a zero MCPOpts mounts MCP with Bearer auth only, no
// OAuth Protected Resource Metadata discovery.
type MCPOpts struct {
	// PublicBaseURL is the externally-reachable base URL of the API
	// (e.g. "https://api.tbite.com" or "http://localhost:8080"). Used to
	// compute the absolute Resource identifier required by RFC 9728.
	PublicBaseURL string
	// AuthorizationServers lists the OAuth 2.0 issuer URLs that can mint
	// access tokens for this MCP endpoint. Typically one entry — the
	// Authentik issuer URL. When empty, no OAuth metadata is published.
	AuthorizationServers []string
}

// New constructs the HTTP server. The optional extraRoutes callback runs after
// the standard routes are registered, letting callers attach additional chi
// handlers. Pass nil when no local-only routes are needed.
// Additional huma API modules can be registered via apiBuilders; each is
// invoked with the shared huma.API so their operations land on the same
// router and OpenAPI document.
//
// mcp is an optional *mcpsrv.MCPServer. When non-nil, the MCP Streamable HTTP
// transport is mounted at /mcp (POST/GET/DELETE). The /mcp route returns 401
// + WWW-Authenticate when called without a valid Bearer token, pointing clients
// at the RFC 9728 resource metadata at
// /.well-known/oauth-protected-resource (when mcpOpts.AuthorizationServers
// is configured).
func New(addr string, logger *slog.Logger, idAPI *idhttp.API, extraRoutes func(chi.Router), mcp *mcpsrv.MCPServer, mcpOpts MCPOpts, apiBuilders ...func(huma.API)) *Server {
	r := chi.NewRouter()
	r.Use(func(next http.Handler) http.Handler {
		return otelhttp.NewHandler(next, "tbite.http")
	})
	// Add the chi-resolved route pattern as an OTel attribute so metrics +
	// spans carry `http.route` (otelhttp can't infer it before chi routes).
	// Runs as a wrapper so labeler attrs land before otelhttp records.
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			next.ServeHTTP(w, r)
			if rctx := chi.RouteContext(r.Context()); rctx != nil {
				if pattern := rctx.RoutePattern(); pattern != "" {
					labeler, _ := otelhttp.LabelerFromContext(r.Context())
					labeler.Add(attribute.String("http.route", pattern))
				}
			}
		})
	})
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Recoverer)
	r.Use(middleware.Timeout(30 * time.Second))
	// Temporary HTTP access log so we can trace MCP client probes (Claude
	// Code, mcp-inspector) end-to-end during DCR/OAuth verification.
	// Filter out /healthz to avoid liveness-probe noise.
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/healthz" && r.URL.Path != "/readyz" {
				logger.Info("http",
					"method", r.Method,
					"path", r.URL.Path,
					"query", r.URL.RawQuery,
					"ua", r.Header.Get("User-Agent"),
					"auth", r.Header.Get("Authorization") != "",
				)
			}
			next.ServeHTTP(w, r)
		})
	})
	r.Use(idAPI.AuthMiddleware)

	// native chi health endpoints
	r.Get("/healthz", healthHandler)
	r.Get("/readyz", readyHandler)
	r.NotFound(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, `{"error":"not_found"}`, http.StatusNotFound)
	})

	// huma API mounted on same chi router
	api := humachi.New(r, huma.DefaultConfig("T-Bite API", "0.1.0"))
	api.OpenAPI().Components.SecuritySchemes = map[string]*huma.SecurityScheme{
		"bearer": {Type: "http", Scheme: "bearer"},
	}
	idAPI.Register(api)

	for _, build := range apiBuilders {
		build(api)
	}

	if mcp != nil {
		// Streamable HTTP transport (MCP spec 2025-03-26) at exact path
		// /mcp — POST for JSON-RPC, GET for the server-initiated stream,
		// DELETE for session teardown. This is what ChatGPT, Claude.ai
		// remote MCP, and the official MCP SDKs target by default.
		streamOpts := []mcpsrv.StreamableHTTPOption{
			mcpsrv.WithEndpointPath("/mcp"),
			mcpsrv.WithStateLess(true),
			// CORS so browser-based MCP playgrounds (and the OpenAI Apps SDK
			// connector page) can hit the endpoint directly. Wildcard origin
			// is safe here because every request still has to carry a valid
			// Bearer session token to do anything.
			mcpsrv.WithStreamableHTTPCORS(
				mcpsrv.WithCORSAllowedOrigins("*"),
				mcpsrv.WithCORSAllowedHeaders("Content-Type", "Authorization", "Mcp-Session-Id", "Mcp-Protocol-Version"),
				mcpsrv.WithCORSExposedHeaders("Mcp-Session-Id"),
			),
		}
		// OAuth 2.0 Protected Resource Metadata (RFC 9728) for clients
		// that probe /mcp without a token — they pick up the authorization
		// server URL from /.well-known/oauth-protected-resource and run an
		// OAuth flow against Authentik before retrying.
		if mcpOpts.PublicBaseURL != "" && len(mcpOpts.AuthorizationServers) > 0 {
			streamOpts = append(streamOpts, mcpsrv.WithProtectedResourceMetadata(mcpsrv.ProtectedResourceMetadataConfig{
				Resource:               strings.TrimRight(mcpOpts.PublicBaseURL, "/") + "/mcp",
				AuthorizationServers:   mcpOpts.AuthorizationServers,
				BearerMethodsSupported: []string{"header"},
				ResourceName:           "T-Bite MCP",
				ScopesSupported:        []string{"openid", "profile", "email"},
			}))
		}
		stream := mcpsrv.NewStreamableHTTPServer(mcp, streamOpts...)
		r.Handle("/mcp", mcpAuthEnforce(stream, mcpOpts, "/mcp"))

		// Mount the RFC 9728 metadata at the well-known path on the router
		// directly so chi serves it without auth — discovery must work
		// before the client has a token.
		if mcpOpts.PublicBaseURL != "" && len(mcpOpts.AuthorizationServers) > 0 {
			meta := mcpsrv.NewProtectedResourceMetadataHandler(mcpsrv.ProtectedResourceMetadataConfig{
				Resource:               strings.TrimRight(mcpOpts.PublicBaseURL, "/") + "/mcp",
				AuthorizationServers:   mcpOpts.AuthorizationServers,
				BearerMethodsSupported: []string{"header"},
				ResourceName:           "T-Bite MCP",
				ScopesSupported:        []string{"openid", "profile", "email"},
			})
			r.Handle("/.well-known/oauth-protected-resource", meta)
			r.Handle("/.well-known/oauth-protected-resource/mcp", meta)
		}

		logger.Info("mcp server mounted",
			"streamable_http", "/mcp",
			"oauth_metadata", mcpOpts.PublicBaseURL != "" && len(mcpOpts.AuthorizationServers) > 0,
		)
	}

	if extraRoutes != nil {
		extraRoutes(r)
	}

	srv := &http.Server{
		Addr:              addr,
		Handler:           r,
		ReadHeaderTimeout: 5 * time.Second,
	}
	return &Server{addr: addr, logger: logger, srv: srv}
}

// mcpAuthEnforce wraps the MCP transport so unauthenticated requests get a
// proper 401 with a WWW-Authenticate header pointing at the resource metadata
// — the discovery handshake Claude.ai and ChatGPT use to find the OAuth
// issuer. Preflight (OPTIONS) and GET (server-stream / metadata bookkeeping)
// pass through without auth so the CORS layer and server stream can initialize
// before the client has a token. POST/DELETE are gated.
func mcpAuthEnforce(next http.Handler, opts MCPOpts, endpoint string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Let CORS preflight and stream GETs through — the user must be
		// authenticated to *do* anything (POST a tool call), and the
		// inner StreamableHTTPServer rejects unauthenticated POSTs by
		// returning the MCP error from the tool handlers. But we want a
		// real HTTP 401 specifically for tool-call POSTs so clients can
		// discover OAuth metadata.
		if r.Method == http.MethodPost {
			if _, ok := idhttp.UserFromContext(r.Context()); !ok {
				// AuthMiddleware falls through to anonymous on a missing,
				// expired, or invalid Bearer token, so this 401 is the real
				// MCP authentication-failure gate that backs the
				// AuthFailureSurge alert.
				observability.RecordMCPAuthFailure(r.Context(), "missing_or_invalid_token")
				if opts.PublicBaseURL != "" && len(opts.AuthorizationServers) > 0 {
					resource := strings.TrimRight(opts.PublicBaseURL, "/") + endpoint
					metaURL := strings.TrimRight(opts.PublicBaseURL, "/") + "/.well-known/oauth-protected-resource"
					w.Header().Set("WWW-Authenticate",
						`Bearer realm="`+resource+`", resource_metadata="`+metaURL+`"`)
				} else {
					w.Header().Set("WWW-Authenticate", `Bearer realm="mcp"`)
				}
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusUnauthorized)
				_, _ = w.Write([]byte(`{"jsonrpc":"2.0","error":{"code":-32001,"message":"unauthorized"},"id":null}`))
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

// Handler returns the underlying http.Handler. Intended for in-process tests
// (httptest.NewServer); production callers should use Run.
func (s *Server) Handler() http.Handler {
	return s.srv.Handler
}

func (s *Server) Run(ctx context.Context) error {
	errCh := make(chan error, 1)
	go func() {
		s.logger.Info("http listening", "addr", s.addr)
		if err := s.srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()
	select {
	case <-ctx.Done():
		s.logger.Info("http shutting down")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		return s.srv.Shutdown(shutdownCtx)
	case err := <-errCh:
		return err
	}
}
