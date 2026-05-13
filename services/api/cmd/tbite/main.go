package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/spf13/pflag"
	"golang.org/x/sync/errgroup"

	"github.com/takalawang/corporate-catering-system/services/api/internal/config"
	"github.com/takalawang/corporate-catering-system/services/api/internal/httpserver"
	"github.com/takalawang/corporate-catering-system/services/api/internal/identity"
	idhttp "github.com/takalawang/corporate-catering-system/services/api/internal/identity/http"
	"github.com/takalawang/corporate-catering-system/services/api/internal/identity/oidc"
	pgrepo "github.com/takalawang/corporate-catering-system/services/api/internal/identity/postgres"
	idredis "github.com/takalawang/corporate-catering-system/services/api/internal/identity/redis"
	"github.com/takalawang/corporate-catering-system/services/api/internal/menu"
	mhttp "github.com/takalawang/corporate-catering-system/services/api/internal/menu/http"
	mpgrepo "github.com/takalawang/corporate-catering-system/services/api/internal/menu/postgres"
	"github.com/takalawang/corporate-catering-system/services/api/internal/order"
	ohttp "github.com/takalawang/corporate-catering-system/services/api/internal/order/http"
	opgrepo "github.com/takalawang/corporate-catering-system/services/api/internal/order/postgres"
	relay "github.com/takalawang/corporate-catering-system/services/api/internal/order/relay"
	scheduler "github.com/takalawang/corporate-catering-system/services/api/internal/order/scheduler"
	"github.com/takalawang/corporate-catering-system/services/api/internal/platform/cache"
	"github.com/takalawang/corporate-catering-system/services/api/internal/platform/clock"
	"github.com/takalawang/corporate-catering-system/services/api/internal/platform/db"
	messaging "github.com/takalawang/corporate-catering-system/services/api/internal/platform/messaging"
	"github.com/takalawang/corporate-catering-system/services/api/internal/quota"
	qhttp "github.com/takalawang/corporate-catering-system/services/api/internal/quota/http"
	qpgrepo "github.com/takalawang/corporate-catering-system/services/api/internal/quota/postgres"
	vendor "github.com/takalawang/corporate-catering-system/services/api/internal/vendors"
	vhttp "github.com/takalawang/corporate-catering-system/services/api/internal/vendors/http"
	vpgrepo "github.com/takalawang/corporate-catering-system/services/api/internal/vendors/postgres"
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

		// 7b. Vendor service + admin handlers
		vendorService := &vendor.Service{
			Vendors:   vpgrepo.NewVendorRepo(pool),
			Plants:    vpgrepo.NewPlantMappingRepo(pool),
			Invites:   invRepo,
			Clock:     clock.SystemClock{},
			InviteTTL: 7 * 24 * time.Hour,
		}
		vendorAPI := &vhttp.API{Svc: vendorService}

		// 7c. Menu service + merchant/employee handlers
		itemRepo := mpgrepo.NewItemRepo(pool)
		menuService := &menu.Service{
			Categories: mpgrepo.NewCategoryRepo(pool),
			Items:      itemRepo,
			Images:     mpgrepo.NewImageRepo(pool),
		}
		menuAPI := &mhttp.API{Svc: menuService}

		// 7d. Quota service + merchant handlers (vendor capacity management)
		supplyRepo := qpgrepo.NewSupplyRepo(pool)
		quotaService := &quota.Service{
			Supplies: supplyRepo,
			Items:    itemRepo,
		}
		quotaAPI := &qhttp.API{Svc: quotaService}

		// 7e. Order service + employee handlers (place / list / get / cancel)
		orderRepo := opgrepo.NewOrderRepo(pool)
		stateEventRepo := opgrepo.NewStateEventRepo(pool)
		auditRepo := opgrepo.NewAuditRepo(pool)
		outboxRepo := opgrepo.NewOutboxRepo(pool)
		plantRepo := vpgrepo.NewPlantMappingRepo(pool)
		orderService := &order.Service{
			Pool:        pool,
			Orders:      orderRepo,
			OrdersTx:    orderRepo,
			StateEvents: stateEventRepo,
			StateTx:     stateEventRepo,
			Audit:       auditRepo,
			AuditTx:     auditRepo,
			Outbox:      outboxRepo,
			OutboxTx:    outboxRepo,
			QuotaTx:     supplyRepo,
			Items:       itemRepo,
			Plants:      plantRepo,
			Clock:       clock.SystemClock{},
		}
		orderAPI := &ohttp.API{Svc: orderService}

		// 8. HTTP server. When FAKE_OIDC=1, swap the google provider for a
		// deterministic fake and mount its auto-redirect handler. Used for
		// local dev and Playwright e2e — never enable in production.
		var extraRoutes func(chi.Router)
		if os.Getenv("FAKE_OIDC") == "1" {
			logger.Warn("FAKE_OIDC enabled: swapping google provider for FakeProvider (dev/e2e only)")
			fake := &oidc.FakeProvider{
				ProviderName: "google",
				BaseURL:      cfg.OIDCCallbackBaseURL,
				Userinfo: &oidc.Userinfo{
					Provider:        "google",
					ExternalSubject: "fake-google-sub-001",
					Email:           "e2e-employee@tbite.test",
					EmailVerified:   true,
					DisplayName:     "E2E 員工",
					Raw:             map[string]any{"e2e": true},
				},
			}
			svc.Providers["google"] = fake
			callback := cfg.OIDCCallbackBaseURL + "/auth/google/callback"
			extraRoutes = func(r chi.Router) {
				// /test/oidc/google/authorize is the fake "consent screen" — it
				// immediately bounces back to the real OIDC callback with a
				// canned authorization code. The `app` query param is required
				// by completeLogin; the FakeProvider only supports employee.
				r.Get("/test/oidc/google/authorize", func(w http.ResponseWriter, req *http.Request) {
					state := req.URL.Query().Get("state")
					http.Redirect(w, req, fmt.Sprintf("%s?state=%s&code=fake&app=employee", callback, state), http.StatusFound)
				})
			}
		}

		srv := httpserver.New(cfg.HTTPAddr, logger, api, extraRoutes,
			vendorAPI.Register,
			menuAPI.Register,
			quotaAPI.Register,
			orderAPI.Register,
		)
		if err := srv.Run(ctx); err != nil {
			logger.Error("api shutdown", "err", err)
			os.Exit(1)
		}
	case config.RoleWorker:
		pool, err := db.NewPool(ctx, cfg.DatabaseRW)
		if err != nil {
			logger.Error("pg pool", "err", err)
			os.Exit(1)
		}
		defer pool.Close()

		if cfg.NATSURL == "" {
			logger.Error("NATS_URL is required for worker role")
			os.Exit(2)
		}
		natsClient, err := messaging.New(ctx, cfg.NATSURL)
		if err != nil {
			logger.Error("nats connect", "err", err)
			os.Exit(1)
		}
		defer natsClient.Close()
		if err := natsClient.ProvisionStreams(ctx); err != nil {
			logger.Error("provision streams", "err", err)
			os.Exit(1)
		}

		outbox := opgrepo.NewOutboxRepo(pool)
		r := &relay.Relay{
			Outbox: outbox,
			JS:     natsClient.JS,
			Logger: logger.With("component", "outbox-relay"),
			Batch:  100,
			Sleep:  500 * time.Millisecond,
		}
		logger.Info("outbox relay starting")
		if err := r.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
			logger.Error("relay shutdown", "err", err)
			os.Exit(1)
		}
		logger.Info("worker shutdown")
	case config.RoleScheduler:
		// P3 ships a single-replica scheduler — no leader election. If we ever
		// scale to >1 replica, wrap sched.Run() with a K8s coordination.k8s.io
		// Lease so only the holder runs RunOnce. Documented as future work.
		pool, err := db.NewPool(ctx, cfg.DatabaseRW)
		if err != nil {
			logger.Error("pg pool", "err", err)
			os.Exit(1)
		}
		defer pool.Close()

		orderRepo := opgrepo.NewOrderRepo(pool)
		// Service used by NoShowSweep. MarkNoShow calls into all the per-tx repos,
		// so we wire a full Service rather than re-implementing the loop.
		sweepSvc := &order.Service{
			Pool:     pool,
			Orders:   orderRepo,
			OrdersTx: orderRepo,
			StateTx:  opgrepo.NewStateEventRepo(pool),
			AuditTx:  opgrepo.NewAuditRepo(pool),
			OutboxTx: opgrepo.NewOutboxRepo(pool),
			Clock:    clock.SystemClock{},
		}
		cutoff := &scheduler.Cutoff{
			Pool:     pool,
			Orders:   orderRepo,
			OrdersTx: orderRepo,
			StateTx:  opgrepo.NewStateEventRepo(pool),
			AuditTx:  opgrepo.NewAuditRepo(pool),
			OutboxTx: opgrepo.NewOutboxRepo(pool),
			Clock:    clock.SystemClock{},
			Logger:   logger.With("component", "cutoff"),
		}
		noShow := &scheduler.NoShowSweep{
			Svc:      sweepSvc,
			Interval: 5 * time.Minute,
			MaxAge:   2 * time.Hour,
			Logger:   logger.With("component", "no-show"),
		}
		cutoffInterval := 60 * time.Second
		if v := os.Getenv("CUTOFF_INTERVAL"); v != "" {
			if d, err := time.ParseDuration(v); err == nil {
				cutoffInterval = d
			}
		}
		if v := os.Getenv("NO_SHOW_INTERVAL"); v != "" {
			if d, err := time.ParseDuration(v); err == nil {
				noShow.Interval = d
			}
		}
		if v := os.Getenv("NO_SHOW_MAX_AGE"); v != "" {
			if d, err := time.ParseDuration(v); err == nil {
				noShow.MaxAge = d
			}
		}

		logger.Info("scheduler starting", "cutoff_interval", cutoffInterval, "noshow_interval", noShow.Interval, "noshow_max_age", noShow.MaxAge)
		eg, egctx := errgroup.WithContext(ctx)
		eg.Go(func() error { return cutoff.Run(egctx, cutoffInterval) })
		eg.Go(func() error { return noShow.Run(egctx) })
		if err := eg.Wait(); err != nil && !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
			logger.Error("scheduler shutdown", "err", err)
			os.Exit(1)
		}
		logger.Info("scheduler shutdown")
	}
}
