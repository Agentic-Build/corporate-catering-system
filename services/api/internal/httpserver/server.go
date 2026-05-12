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
func New(addr string, logger *slog.Logger, idAPI *idhttp.API, extraRoutes func(chi.Router)) *Server {
	r := chi.NewRouter()
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
