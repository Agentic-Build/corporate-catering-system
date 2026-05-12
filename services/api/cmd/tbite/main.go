package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/spf13/pflag"

	"github.com/takalawang/corporate-catering-system/services/api/internal/config"
	"github.com/takalawang/corporate-catering-system/services/api/internal/httpserver"
	"github.com/takalawang/corporate-catering-system/services/api/internal/identity"
	idhttp "github.com/takalawang/corporate-catering-system/services/api/internal/identity/http"
	"github.com/takalawang/corporate-catering-system/services/api/internal/identity/oidc"
	pgrepo "github.com/takalawang/corporate-catering-system/services/api/internal/identity/postgres"
	idredis "github.com/takalawang/corporate-catering-system/services/api/internal/identity/redis"
	"github.com/takalawang/corporate-catering-system/services/api/internal/platform/cache"
	"github.com/takalawang/corporate-catering-system/services/api/internal/platform/clock"
	"github.com/takalawang/corporate-catering-system/services/api/internal/platform/db"
)

func main() {
	var roleStr string
	pflag.StringVar(&roleStr, "role", "api", "binary role: api|worker|scheduler")
	pflag.Parse()

	role, err := config.ParseRole(roleStr)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}

	cfg, err := config.FromEnv()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	cfg.Role = role

	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	logger = logger.With("role", string(role))

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	switch role {
	case config.RoleAPI:
		// 1. Postgres pool
		pool, err := db.NewPool(ctx, cfg.DatabaseRW)
		if err != nil {
			logger.Error("pg pool", "err", err)
			os.Exit(1)
		}
		defer pool.Close()

		// 2. Redis client
		rdb, err := cache.NewClient(ctx, cfg.RedisURL)
		if err != nil {
			logger.Error("redis", "err", err)
			os.Exit(1)
		}
		defer rdb.Close()

		// 3. OIDC providers
		googleRedirect := cfg.OIDCCallbackBaseURL + "/auth/google/callback"
		googleProv, err := oidc.NewGoogle(ctx, cfg.GoogleClientID, cfg.GoogleClientSecret, googleRedirect)
		if err != nil {
			logger.Error("google oidc discovery", "err", err)
			os.Exit(1)
		}
		githubRedirect := cfg.OIDCCallbackBaseURL + "/auth/github/callback"
		githubProv := oidc.NewGitHub(cfg.GitHubClientID, cfg.GitHubClientSecret, githubRedirect)

		// 4. Repositories
		userRepo := pgrepo.NewUserRepo(pool)
		idRepo := pgrepo.NewUserIdentityRepo(pool)
		dirRepo := pgrepo.NewDirectoryRepo(pool)
		invRepo := pgrepo.NewVendorInviteRepo(pool)
		awRepo := pgrepo.NewAdminWhitelistRepo(pool)

		// 5. Session store + OIDC state store
		sessStore := idredis.NewSessionStore(rdb, 7*24*time.Hour)
		stateStore := oidc.NewRedisStateStore(rdb, 5*time.Minute)

		// 6. Identity service
		svc := &identity.Service{
			Users:      userRepo,
			Identities: idRepo,
			Directory:  dirRepo,
			Invites:    invRepo,
			AdminWL:    awRepo,
			Sessions:   sessStore,
			Providers: map[string]oidc.Provider{
				"google": googleProv,
				"github": githubProv,
			},
			States: stateStore,
			Clock:  clock.SystemClock{},
		}

		// 7. HTTP API
		api := &idhttp.API{
			Svc:      svc,
			Sessions: sessStore,
			Users:    userRepo,
			AppURLs: idhttp.AppBaseURLs{
				"employee": cfg.AppBaseURLEmployee,
				"merchant": cfg.AppBaseURLMerchant,
				"admin":    cfg.AppBaseURLAdmin,
			},
		}

		// 8. HTTP server
		srv := httpserver.New(cfg.HTTPAddr, logger, api)
		if err := srv.Run(ctx); err != nil {
			logger.Error("api shutdown", "err", err)
			os.Exit(1)
		}
	case config.RoleWorker:
		logger.Info("worker placeholder, waiting for shutdown signal (P0)")
		<-ctx.Done()
		logger.Info("worker shutting down")
	case config.RoleScheduler:
		logger.Info("scheduler placeholder, waiting for shutdown signal (P0)")
		<-ctx.Done()
		logger.Info("scheduler shutting down")
	}
}
