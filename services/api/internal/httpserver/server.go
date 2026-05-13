package httpserver

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humachi"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	mcpsrv "github.com/mark3labs/mcp-go/server"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"

	idhttp "github.com/takalawang/corporate-catering-system/services/api/internal/identity/http"
)

type Server struct {
	addr   string
	logger *slog.Logger
	srv    *http.Server
}

// New constructs the HTTP server. The optional extraRoutes callback runs after
// the standard routes are registered, letting callers attach additional chi
// handlers (e.g. the FAKE_OIDC auto-redirect endpoint). Pass nil in production.
// Additional huma API modules can be registered via apiBuilders; each is
// invoked with the shared huma.API so their operations land on the same
// router and OpenAPI document.
//
// mcp is an optional *mcpsrv.MCPServer. When non-nil, an SSE-based MCP
// transport is mounted under /mcp (handshake at /mcp/sse, JSON-RPC POSTs at
// /mcp/message). Auth reuses the global Bearer middleware applied via r.Use,
// so MCP tool handlers can read the authenticated user via
// idhttp.UserFromContext just like REST handlers.
func New(addr string, logger *slog.Logger, idAPI *idhttp.API, extraRoutes func(chi.Router), mcp *mcpsrv.MCPServer, apiBuilders ...func(huma.API)) *Server {
	r := chi.NewRouter()
	r.Use(func(next http.Handler) http.Handler {
		return otelhttp.NewHandler(next, "tbite.http")
	})
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Recoverer)
	r.Use(middleware.Timeout(30 * time.Second))
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
		// SSEServer.ServeHTTP matches exact paths (/mcp/sse, /mcp/message)
		// when constructed with WithBasePath("/mcp"); chi forwards /mcp/* to
		// it with the original r.URL.Path intact, which is what SSEServer
		// expects.
		sse := mcpsrv.NewSSEServer(mcp, mcpsrv.WithBasePath("/mcp"))
		r.Handle("/mcp/*", sse)
		logger.Info("mcp server mounted", "path", "/mcp", "sse", "/mcp/sse", "message", "/mcp/message")
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
