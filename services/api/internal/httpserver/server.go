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

// New constructs the HTTP server. extraRoutes (optional) attaches chi handlers
// after standard routes; apiBuilders register more huma API modules onto the
// shared API. When mcp != nil, the MCP Streamable HTTP transport mounts at
// /mcp; unauthenticated POSTs return 401 + WWW-Authenticate pointing at
// /.well-known/oauth-protected-resource.
func New(addr string, logger *slog.Logger, idAPI *idhttp.API, extraRoutes func(chi.Router), mcp *mcpsrv.MCPServer, mcpOpts MCPOpts, apiBuilders ...func(huma.API)) *Server {
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

	r.Get("/healthz", healthHandler)
	r.Get("/readyz", readyHandler)
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

	if mcp != nil {
		// MCP Streamable HTTP transport at /mcp (POST/GET/DELETE) per spec 2025-03-26.
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
		// OAuth Protected Resource Metadata (RFC 9728) for clients that
		// probe /mcp without a token.
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

		// Mount well-known path on chi without auth — discovery must work
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
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		return s.srv.Shutdown(shutdownCtx)
	case err := <-errCh:
		return err
	}
}
