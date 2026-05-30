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

	idhttp "github.com/Agentic-Build/corporate-catering-system/services/api/internal/identity/http"
	"github.com/Agentic-Build/corporate-catering-system/services/api/internal/platform/observability"
)

type Server struct {
	addr   string
	logger *slog.Logger
	srv    *http.Server
}

// MCPOpts carries configuration for the MCP transport mount. Empty fields
// degrade gracefully: a zero MCPOpts mounts MCP with Bearer auth only.
type MCPOpts struct {
	// PublicBaseURL is the externally-reachable base URL; used to compute the
	// absolute Resource identifier required by RFC 9728.
	PublicBaseURL string
	// AuthorizationServers lists the OAuth 2.0 issuer URLs (typically just the
	// Authentik issuer). Empty → no OAuth metadata published.
	AuthorizationServers []string
}

// MCP bundles the optional MCP transport: the server to mount and its
// transport options. A nil Server skips the /mcp mount entirely.
type MCP struct {
	Server *mcpsrv.MCPServer
	Opts   MCPOpts
}

// New constructs the HTTP server. extraRoutes (optional) attaches chi handlers
// after standard routes; apiBuilders register more huma API modules onto the
// shared API. When mcp.Server != nil, the MCP Streamable HTTP transport mounts at
// /mcp; unauthenticated POSTs return 401 + WWW-Authenticate pointing at
// /.well-known/oauth-protected-resource.
func New(addr string, logger *slog.Logger, idAPI *idhttp.API, extraRoutes func(chi.Router), mcp MCP, apiBuilders ...func(huma.API)) *Server {
	return newServer(addr, logger, idAPI, nil, extraRoutes, mcp, apiBuilders...)
}

// NewWithHealth constructs the HTTP server with dependency-aware health
// endpoints. API roles that own hard runtime dependencies should use this so
// /readyz reflects whether those dependencies can serve requests.
func NewWithHealth(addr string, logger *slog.Logger, idAPI *idhttp.API, health *Health, extraRoutes func(chi.Router), mcp MCP, apiBuilders ...func(huma.API)) *Server {
	return newServer(addr, logger, idAPI, health, extraRoutes, mcp, apiBuilders...)
}

func newServer(addr string, logger *slog.Logger, idAPI *idhttp.API, health *Health, extraRoutes func(chi.Router), mcp MCP, apiBuilders ...func(huma.API)) *Server {
	r := chi.NewRouter()
	r.Use(func(next http.Handler) http.Handler {
		return otelhttp.NewHandler(next, "tbite.http")
	})
	// Add chi route pattern as an OTel attribute so metrics+spans carry
	// http.route (otelhttp can't infer it before chi routes).
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
	r.Use(idAPI.AuthMiddleware)

	if health != nil {
		r.Get("/healthz", health.LivenessHandler())
		r.Get("/readyz", health.ReadinessHandler())
	} else {
		r.Get("/healthz", healthHandler)
		r.Get("/readyz", readyHandler)
	}
	r.NotFound(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, `{"error":"not_found"}`, http.StatusNotFound)
	})

	api := humachi.New(r, huma.DefaultConfig("T-Bite API", "0.1.0"))
	api.OpenAPI().Components.SecuritySchemes = map[string]*huma.SecurityScheme{
		"bearer": {Type: "http", Scheme: "bearer"},
	}
	idAPI.Register(api)

	for _, build := range apiBuilders {
		build(api)
	}

	if mcp.Server != nil {
		mountMCP(r, logger, mcp.Server, mcp.Opts)
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

// mcpResourceConfig builds the shared RFC 9728 metadata config so the stream
// option and the well-known handler stay in sync.
func mcpResourceConfig(mcpOpts MCPOpts) mcpsrv.ProtectedResourceMetadataConfig {
	return mcpsrv.ProtectedResourceMetadataConfig{
		Resource:               strings.TrimRight(mcpOpts.PublicBaseURL, "/") + "/mcp",
		AuthorizationServers:   mcpOpts.AuthorizationServers,
		BearerMethodsSupported: []string{"header"},
		ResourceName:           "T-Bite MCP",
		ScopesSupported:        []string{"openid", "profile", "email"},
	}
}

// mountMCP wires the Streamable HTTP transport at /mcp plus the well-known
// OAuth protected-resource metadata when configured.
func mountMCP(r chi.Router, logger *slog.Logger, mcp *mcpsrv.MCPServer, mcpOpts MCPOpts) {
	hasMetadata := mcpOpts.PublicBaseURL != "" && len(mcpOpts.AuthorizationServers) > 0
	streamOpts := []mcpsrv.StreamableHTTPOption{
		mcpsrv.WithEndpointPath("/mcp"),
		mcpsrv.WithStateLess(true),
		// Wildcard CORS is safe — Bearer auth still required to do anything.
		mcpsrv.WithStreamableHTTPCORS(
			mcpsrv.WithCORSAllowedOrigins("*"),
			mcpsrv.WithCORSAllowedHeaders("Content-Type", "Authorization", "Mcp-Session-Id", "Mcp-Protocol-Version"),
			mcpsrv.WithCORSExposedHeaders("Mcp-Session-Id"),
		),
	}
	if hasMetadata {
		streamOpts = append(streamOpts, mcpsrv.WithProtectedResourceMetadata(mcpResourceConfig(mcpOpts)))
	}
	stream := mcpsrv.NewStreamableHTTPServer(mcp, streamOpts...)
	r.Handle("/mcp", mcpAuthEnforce(stream, mcpOpts, "/mcp"))

	if hasMetadata {
		meta := mcpsrv.NewProtectedResourceMetadataHandler(mcpResourceConfig(mcpOpts))
		r.Handle("/.well-known/oauth-protected-resource", meta)
		r.Handle("/.well-known/oauth-protected-resource/mcp", meta)
	}

	logger.Info("mcp server mounted",
		"streamable_http", "/mcp",
		"oauth_metadata", hasMetadata,
	)
}

// mcpAuthEnforce wraps the MCP transport so unauthenticated POSTs return a
// real HTTP 401 + WWW-Authenticate (the discovery handshake Claude.ai/ChatGPT
// use). OPTIONS and GET pass through so CORS and the server stream can init
// before the client has a token.
func mcpAuthEnforce(next http.Handler, opts MCPOpts, endpoint string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			if _, ok := idhttp.UserFromContext(r.Context()); !ok {
				// AuthMiddleware falls through to anonymous on missing/expired/invalid
				// tokens — this 401 is the gate backing the AuthFailureSurge alert.
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

// Handler returns the underlying http.Handler (for in-process tests). Production uses Run.
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
		shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
		defer cancel()
		return s.srv.Shutdown(shutdownCtx)
	case err := <-errCh:
		return err
	}
}
