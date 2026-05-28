package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"
	// Embed the IANA tz database: the cluster image is distroless (no tzdata on
	// disk) with CGO disabled, so without this LoadLocation("Asia/Taipei")
	// fails and time.Local degrades to UTC — breaking cutoff math.
	_ "time/tzdata"

	"github.com/go-chi/chi/v5"
	mcpgo "github.com/mark3labs/mcp-go/server"
	"github.com/spf13/pflag"

	chttp "github.com/Agentic-Build/corporate-catering-system/services/api/internal/compliance/http"
	"github.com/Agentic-Build/corporate-catering-system/services/api/internal/config"
	dlqhttp "github.com/Agentic-Build/corporate-catering-system/services/api/internal/dlq/http"
	dlqpgrepo "github.com/Agentic-Build/corporate-catering-system/services/api/internal/dlq/postgres"
	feedbackhttp "github.com/Agentic-Build/corporate-catering-system/services/api/internal/feedback/http"
	"github.com/Agentic-Build/corporate-catering-system/services/api/internal/httpserver"
	"github.com/Agentic-Build/corporate-catering-system/services/api/internal/identity"
	idauthentik "github.com/Agentic-Build/corporate-catering-system/services/api/internal/identity/authentik"
	idhttp "github.com/Agentic-Build/corporate-catering-system/services/api/internal/identity/http"
	"github.com/Agentic-Build/corporate-catering-system/services/api/internal/identity/hydra"
	"github.com/Agentic-Build/corporate-catering-system/services/api/internal/identity/oidc"
	pgrepo "github.com/Agentic-Build/corporate-catering-system/services/api/internal/identity/postgres"
	"github.com/Agentic-Build/corporate-catering-system/services/api/internal/mcpserver"
	"github.com/Agentic-Build/corporate-catering-system/services/api/internal/menu"
	mhttp "github.com/Agentic-Build/corporate-catering-system/services/api/internal/menu/http"
	mpgrepo "github.com/Agentic-Build/corporate-catering-system/services/api/internal/menu/postgres"
	"github.com/Agentic-Build/corporate-catering-system/services/api/internal/menu/readmodel"
	"github.com/Agentic-Build/corporate-catering-system/services/api/internal/order"
	ohttp "github.com/Agentic-Build/corporate-catering-system/services/api/internal/order/http"
	opgrepo "github.com/Agentic-Build/corporate-catering-system/services/api/internal/order/postgres"
	payrollhttp "github.com/Agentic-Build/corporate-catering-system/services/api/internal/payroll/http"
	"github.com/Agentic-Build/corporate-catering-system/services/api/internal/plants"
	phttp "github.com/Agentic-Build/corporate-catering-system/services/api/internal/plants/http"
	ppgrepo "github.com/Agentic-Build/corporate-catering-system/services/api/internal/plants/postgres"
	"github.com/Agentic-Build/corporate-catering-system/services/api/internal/platform/cache"
	"github.com/Agentic-Build/corporate-catering-system/services/api/internal/platform/clock"
	"github.com/Agentic-Build/corporate-catering-system/services/api/internal/platform/db"
	messaging "github.com/Agentic-Build/corporate-catering-system/services/api/internal/platform/messaging"
	"github.com/Agentic-Build/corporate-catering-system/services/api/internal/platform/observability"
	"github.com/Agentic-Build/corporate-catering-system/services/api/internal/platform/storage"
	"github.com/Agentic-Build/corporate-catering-system/services/api/internal/quota"
	qhttp "github.com/Agentic-Build/corporate-catering-system/services/api/internal/quota/http"
	qpgrepo "github.com/Agentic-Build/corporate-catering-system/services/api/internal/quota/postgres"
	settlementhttp "github.com/Agentic-Build/corporate-catering-system/services/api/internal/settlement/http"
	vhttp "github.com/Agentic-Build/corporate-catering-system/services/api/internal/vendors/http"
)

func main() {
	var roleStr string
	pflag.StringVar(&roleStr, "role", "api",
		"binary role: api | mcp-stdio | "+
			"outbox-relay | payroll-settler | on-time-evaluator | "+
			"cutoff-sweeper | no-show-sweeper | document-expiry-scanner | feedback-scanner | "+
			"realtime-gateway | provision-streams")
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

	shutdownTracer, err := observability.Init(ctx, "tbite-"+string(role), "0.1.0")
	if err != nil {
		logger.Warn("otel init failed; continuing without tracing", "err", err)
	} else {
		defer func() {
			sd, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			_ = shutdownTracer(sd)
		}()
	}
	shutdownMeter, err := observability.InitMeter(ctx, "tbite-"+string(role), "0.1.0")
	if err != nil {
		logger.Warn("otel meter init failed; continuing without metrics", "err", err)
	} else {
		defer func() {
			sd, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			_ = shutdownMeter(sd)
		}()
	}
	observability.MustInitMetrics()

	// Dispatch table for the cloud-native split roles (architecture
	// issues #56, #58, #62). These short-circuit before the api and
	// mcp-stdio switch so the per-role bodies stay in roles.go.
	switch role {
	case config.RoleOutboxRelay:
		if err := runOutboxRelay(ctx, logger, cfg); err != nil {
			logger.Error("outbox-relay", "err", err)
			os.Exit(1)
		}
		return
	case config.RolePayrollSettler:
		if err := runPayrollSettler(ctx, logger, cfg); err != nil {
			logger.Error("payroll-settler", "err", err)
			os.Exit(1)
		}
		return
	case config.RoleOnTimeEvaluator:
		if err := runOnTimeEvaluator(ctx, logger, cfg); err != nil {
			logger.Error("on-time-evaluator", "err", err)
			os.Exit(1)
		}
		return
	case config.RoleCutoffSweeper:
		if err := runCutoffSweeper(ctx, logger, cfg); err != nil {
			logger.Error("cutoff-sweeper", "err", err)
			os.Exit(1)
		}
		return
	case config.RoleNoShowSweeper:
		if err := runNoShowSweeper(ctx, logger, cfg); err != nil {
			logger.Error("no-show-sweeper", "err", err)
			os.Exit(1)
		}
		return
	case config.RoleDocExpiryScanner:
		if err := runDocExpiryScanner(ctx, logger, cfg); err != nil {
			logger.Error("document-expiry-scanner", "err", err)
			os.Exit(1)
		}
		return
	case config.RoleFeedbackScanner:
		if err := runFeedbackScanner(ctx, logger, cfg); err != nil {
			logger.Error("feedback-scanner", "err", err)
			os.Exit(1)
		}
		return
	case config.RoleRealtimeGateway:
		if err := runRealtimeGateway(ctx, logger, cfg); err != nil {
			logger.Error("realtime-gateway", "err", err)
			os.Exit(1)
		}
		return
	case config.RoleProvisionStreams:
		if err := runProvisionStreams(ctx, logger, cfg); err != nil {
			logger.Error("provision-streams", "err", err)
			os.Exit(1)
		}
		return
	}

	switch role {
	case config.RoleAPI:
		// 1. Postgres pool
		pool, err := db.NewPoolWithConfig(ctx, cfg.DatabaseRW, db.PoolConfig{MaxConns: cfg.DBMaxConns, MinConns: cfg.DBMinConns})
		if err != nil {
			logger.Error("pg pool", "err", err)
			os.Exit(1)
		}
		defer pool.Close()
		_ = db.RegisterPoolMetrics(pool, "rw")

		// Read-only pool for read-model paths (ADR-0007); falls back to primary
		// when no replica is configured.
		roPool, err := newROPool(ctx, cfg)
		if err != nil {
			logger.Error("pg ro pool", "err", err)
			os.Exit(1)
		}
		defer roPool.Close()

		// 2. Redis client
		rdb, err := cache.NewClient(ctx, cfg.RedisURL)
		if err != nil {
			logger.Error("redis", "err", err)
			os.Exit(1)
		}
		defer rdb.Close()

		// 3. S3 / object storage. Constructed before buildCoreServices so the
		// compliance service is wired to a non-nil Storage; the api role hard-
		// depends on object storage so we fail fast on misconfiguration.
		s3API, err := storage.NewS3(ctx, storage.S3Config{
			Endpoint:        cfg.S3Endpoint,
			Region:          cfg.S3Region,
			AccessKeyID:     cfg.S3AccessKeyID,
			SecretAccessKey: cfg.S3SecretAccessKey,
			Bucket:          cfg.S3Bucket,
			UsePathStyle:    cfg.S3UsePathStyle,
		})
		if err != nil {
			logger.Error("s3", "err", err)
			os.Exit(1)
		}
		if err := s3API.EnsureBucket(ctx); err != nil {
			logger.Warn("ensure bucket failed; uploads will fail until storage is reachable", "err", err)
		}

		// 4. Shared service graph (same wiring the mcp-stdio role uses).
		cs, err := buildCoreServices(ctx, pool, rdb, cfg, s3API, logger)
		if err != nil {
			logger.Error("build services", "err", err)
			os.Exit(1)
		}
		if err := qpgrepo.RegisterSupplyGauges(pool); err != nil {
			logger.Warn("register supply gauges", "err", err)
		}

		// 5. OIDC providers
		providers, err := buildOIDCProviders(ctx, cfg)
		if err != nil {
			logger.Error("oidc providers", "err", err)
			os.Exit(1)
		}

		// 6. Identity-specific repos + state store (API-only)
		idRepo := pgrepo.NewUserIdentityRepo(pool)
		stateStore := oidc.NewRedisStateStore(rdb, 5*time.Minute)

		// 7. Identity service + HTTP API
		svc := &identity.Service{
			Users:      cs.UserRepo,
			Identities: idRepo,
			Sessions:   cs.SessStore,
			Providers:  providers,
			States:     stateStore,
		}
		api := &idhttp.API{
			Svc:      svc,
			Sessions: cs.SessStore,
			Users:    cs.UserRepo,
			Handoff:  cs.SessStore,
			AppURLs: idhttp.AppBaseURLs{
				"employee": cfg.AppBaseURLEmployee,
				"merchant": cfg.AppBaseURLMerchant,
				"admin":    cfg.AppBaseURLAdmin,
			},
		}

		// Hydra OAuth bridge (only when HYDRA_PUBLIC_URL is set): we reverse-
		// proxy Hydra under our host so MCP clients see one issuer (matches
		// Hydra's URLS_SELF_ISSUER) and a CORS-clean origin.
		var (
			hydraBridge    *hydra.Bridge
			hydraProxy     http.Handler
			hydraDiscovery *hydra.DiscoveryShim
		)
		if cfg.HydraPublicURL != "" {
			publicIssuer := strings.TrimRight(cfg.OIDCCallbackBaseURL, "/") + "/"
			mcpResource := strings.TrimRight(cfg.OIDCCallbackBaseURL, "/") + "/mcp"
			tokVerifier, err := hydra.NewAccessTokenVerifier(ctx, cfg.HydraPublicURL, publicIssuer, mcpResource)
			if err != nil {
				logger.Error("hydra access token verifier", "err", err)
				os.Exit(1)
			}
			api.JWT = hydra.SubjectVerifier{V: tokVerifier}
			// Bridge-only OIDC client uses /oauth/callback (web uses /auth/{slug}/callback).
			var bridgeOIDC *oidc.OIDCProvider
			var bridgeOIDCSlug string
			if len(cfg.AuthProviders) > 0 {
				p := cfg.AuthProviders[0]
				bp, err := oidc.New(ctx, oidc.Config{
					Slug:         p.Slug,
					DisplayName:  p.DisplayName,
					IssuerURL:    p.IssuerURL,
					ClientID:     p.ClientID,
					ClientSecret: p.ClientSecret,
					RedirectURL:  strings.TrimRight(cfg.OIDCCallbackBaseURL, "/") + "/oauth/callback",
					Scopes:       p.Scopes,
				})
				if err != nil {
					logger.Error("mcp oidc provider", "err", err)
					os.Exit(1)
				}
				bridgeOIDC = bp
				bridgeOIDCSlug = p.Slug
			}
			hydraBridge = &hydra.Bridge{
				Hydra:            hydra.NewAdminClient(cfg.HydraAdminURL),
				Sessions:         cs.SessStore,
				Users:            cs.UserRepo,
				Identities:       idRepo,
				OIDCProvider:     bridgeOIDC,
				OIDCProviderName: bridgeOIDCSlug,
				States:           stateStore,
				PublicBaseURL:    cfg.OIDCCallbackBaseURL,
			}
			hydraDiscovery = hydra.NewDiscoveryShim(cfg.HydraPublicURL)
			hydraDiscovery.PublicBaseURL = strings.TrimRight(cfg.OIDCCallbackBaseURL, "/")
			proxy, err := hydra.ReverseProxy(cfg.HydraPublicURL)
			if err != nil {
				logger.Error("hydra reverse proxy", "err", err)
				os.Exit(1)
			}
			hydraProxy = proxy
			logger.Info("hydra bridge wired",
				"public_url", cfg.HydraPublicURL,
				"admin_url", cfg.HydraAdminURL,
				"issuer", publicIssuer,
			)
		}

		// 8. HTTP handlers — one per bounded context, all sharing the services
		// constructed in buildCoreServices above.
		vendorAPI := &vhttp.API{Svc: cs.Vendor}

		// Plant registry (vendors pick from this list during onboarding).
		plantRegistrySvc := &plants.Service{Repo: ppgrepo.NewPlantRepo(pool)}
		plantAPI := &phttp.API{Svc: plantRegistrySvc, VendorSvc: cs.Vendor}

		// Menu — direct multipart + presigned uploads go to object storage
		// (Storage is set at construction since s3API is already built).
		menuAPI := &mhttp.API{
			Svc:                  cs.Menu,
			Storage:              s3API,
			StoragePublicBaseURL: cfg.S3PublicBaseURL,
			StorageBucket:        cfg.S3Bucket,
		}

		// Quota (merchant capacity management).
		quotaAPI := &qhttp.API{Svc: &quota.Service{Supplies: cs.SupplyRepo, Items: cs.ItemRepo}}

		// Order — BoardHub / MenuHub fan live events to the merchant prep board
		// over SSE; wired to NATS below when NATS_URL is configured.
		boardHub := order.NewBoardHub()
		menuHub := order.NewMenuHub()
		orderAPI := &ohttp.API{Svc: cs.Order, Board: boardHub, MenuHub: menuHub}

		payrollAPI := &payrollhttp.API{Svc: cs.Payroll}
		complianceAPI := &chttp.API{Svc: cs.Compliance}

		// 7h. DLQ admin handlers (list/replay/resolve). NATS is optional: when
		// NATS_URL is set we wire JetStream so /replay can re-publish; otherwise
		// /replay returns 503 and only list/resolve work.
		dlqRepo := dlqpgrepo.NewDLQRepo(pool)
		dlqAPI := &dlqhttp.API{Repo: dlqRepo}
		if err := dlqpgrepo.RegisterDLQGauges(pool); err != nil {
			logger.Warn("register dlq gauges", "err", err)
		}
		if cfg.NATSURL != "" {
			if natsClient, err := messaging.New(ctx, cfg.NATSURL); err == nil {
				dlqAPI.JS = natsClient.JS
				defer natsClient.Close()
				// Tap ORDERS_V1 for the merchant board SSE; non-fatal.
				go func() {
					if err := order.RunBoardConsumer(ctx, natsClient.JS, boardHub, menuHub, logger); err != nil {
						logger.Warn("board consumer stopped", "err", err)
					}
				}()
			} else {
				logger.Warn("nats unavailable for dlq replay; /replay will return 503", "err", err)
			}
		}

		// Personalisation: favorites + reorder + home aggregate.
		favoriteRepo := mpgrepo.NewFavoriteRepo(pool)
		favoritesSvc := menu.NewFavoritesService(favoriteRepo)
		favoritesAPI := &mhttp.FavoritesAPI{Svc: favoritesSvc}

		reorderSvc := order.NewReorderService(
			pool,
			cs.OrderRepo,
			p9SupplyRepoAdapter{inner: cs.SupplyRepo},
			p9ItemRepoAdapter{inner: cs.ItemRepo},
			cs.VendorRepo,
			cs.PlantRepo,
			cs.StateEventRepo,
			cs.AuditRepo,
			cs.OutboxRepo,
			clock.SystemClock{},
			appLocation(),
		)
		reorderAPI := &ohttp.ReorderAPI{Svc: reorderSvc}

		alpha := 1.0
		if raw := os.Getenv("RECOMMENDATION_ALPHA"); raw != "" {
			if v, err := strconv.ParseFloat(raw, 64); err == nil {
				alpha = v
			} else {
				logger.Warn("invalid RECOMMENDATION_ALPHA; defaulting to 1.0", "raw", raw)
			}
		}
		// Read-model aggregates run off the RO pool (ADR-0007); Redis-cached
		// with NATS-driven invalidation + TTL safety net.
		popularityRepo := mpgrepo.NewPopularityRepo(roPool)
		affinityRepo := mpgrepo.NewAffinityRepo(roPool)
		recentOrdersRepo := opgrepo.NewRecentOrdersRepo(roPool)
		homeCache := &readmodel.RedisCache{C: rdb, Prefix: "tbite:rm:"}
		homeMetrics := readmodel.NewMetrics()
		cachedPopularity := cachedPopularityAdapter{
			cached: &readmodel.CachedPopularity{Inner: popularityRepo, Cache: homeCache, Metrics: homeMetrics},
			repo:   popularityRepo,
		}
		cachedAffinity := &readmodel.CachedAffinity{Inner: affinityRepo, Cache: homeCache, Metrics: homeMetrics}
		homeSvc := &menu.HomeService{
			Clock:         clock.SystemClock{},
			ServerTZ:      appLocation(),
			RecentOrders:  recentOrdersRepo,
			Popularity:    cachedPopularity,
			Affinity:      cachedAffinity,
			FavoritesRepo: favoriteRepo,
			Alpha:         alpha,
			VendorNames: func(ctx context.Context, ids []string) (map[string]string, error) {
				out := map[string]string{}
				if len(ids) == 0 {
					return out, nil
				}
				rows, err := roPool.Query(ctx, `SELECT id, display_name FROM vendor WHERE id = ANY($1)`, ids)
				if err != nil {
					return nil, err
				}
				defer rows.Close()
				for rows.Next() {
					var id, name string
					if err := rows.Scan(&id, &name); err != nil {
						return nil, err
					}
					out[id] = name
				}
				return out, rows.Err()
			},
		}
		homeAPI := &mhttp.HomeAPI{
			Home:        homeSvc,
			MenuSvc:     cs.Menu,
			Cache:       homeCache,
			CacheTTL:    30 * time.Second,
			CacheMetric: homeMetrics,
		}
		// Read-model invalidator on ORDERS_V1; non-fatal (TTL is the fallback).
		if cfg.NATSURL != "" {
			go func() {
				natsClient, err := messaging.New(ctx, cfg.NATSURL)
				if err != nil {
					logger.Warn("readmodel invalidator: nats unavailable", "err", err)
					return
				}
				defer natsClient.Close()
				if err := readmodel.RunOrderInvalidator(ctx, natsClient.JS, homeCache, logger.With("component", "readmodel-invalidator")); err != nil {
					logger.Warn("readmodel invalidator stopped", "err", err)
				}
			}()
		}

		feedbackAPI := &feedbackhttp.API{Svc: cs.Feedback}
		settlementAPI := &settlementhttp.API{Svc: cs.Settlement}

		// MCP server — mounted at /mcp by httpserver.New below.
		mcpSrv := mcpserver.New(mcpserver.Deps{
			Pool:       pool,
			Audit:      cs.AuditRepo,
			Order:      cs.Order,
			Vendor:     cs.Vendor,
			Menu:       cs.Menu,
			Payroll:    cs.Payroll,
			Compliance: cs.Compliance,
			Feedback:   cs.Feedback,
			Settlement: cs.Settlement,
			Users:      cs.UserRepo,
			Sessions:   cs.SessStore,
		})

		// MCP OAuth metadata: advertise our host when Hydra is wired (DCR
		// needs one origin); fall back to Authentik's issuer otherwise.
		var mcpAuthServers []string
		if cfg.HydraPublicURL != "" {
			mcpAuthServers = []string{strings.TrimRight(cfg.OIDCCallbackBaseURL, "/") + "/"}
		} else {
			mcpAuthServers = make([]string, 0, len(cfg.AuthProviders))
			for _, p := range cfg.AuthProviders {
				mcpAuthServers = append(mcpAuthServers, p.IssuerURL)
			}
		}
		mcpOpts := httpserver.MCPOpts{
			PublicBaseURL:        cfg.OIDCCallbackBaseURL,
			AuthorizationServers: mcpAuthServers,
		}

		srv := httpserver.New(cfg.HTTPAddr, logger, api, func(r chi.Router) {
			// Direct multipart upload — vendor-scoped, returns public MinIO URL.
			r.Post("/api/merchant/uploads", menuAPI.HandleDirectUpload)

			// Hydra OAuth bridge endpoints (anonymous; only when sidecar wired).
			if hydraBridge != nil {
				r.Get("/oauth/login", hydraBridge.LoginHandler)
				r.Get("/oauth/callback", hydraBridge.CallbackHandler)
				r.Get("/oauth/consent", hydraBridge.ConsentHandler)
				// Discovery shim injects registration_endpoint (Hydra v2.2-2.3 omits it).
				r.Get("/.well-known/openid-configuration", hydraDiscovery.ServeHTTP)
				r.Get("/.well-known/oauth-authorization-server", hydraDiscovery.ServeHTTP)
				// DCR (RFC 7591) sanitizing proxy: strips empty optional URI
				// fields strict OAuth clients (Claude/ChatGPT) reject.
				dcrProxy := &hydra.SanitizingDCRProxy{HydraURL: cfg.HydraPublicURL}
				r.Handle("/oauth2/register", dcrProxy)
				r.Handle("/oauth2/register/*", dcrProxy)
				// Reverse-proxy Hydra under our host so iss / discovery URLs line up.
				r.Handle("/oauth2/*", hydraProxy)
				r.Get("/.well-known/jwks.json", hydraProxy.ServeHTTP)
				r.Get("/userinfo", hydraProxy.ServeHTTP)
			}
		}, mcpSrv, mcpOpts,
			vendorAPI.Register,
			plantAPI.Register,
			menuAPI.Register,
			menuAPI.RegisterPresigned,
			quotaAPI.Register,
			orderAPI.Register,
			payrollAPI.Register,
			complianceAPI.Register,
			dlqAPI.Register,
			favoritesAPI.Register,
			reorderAPI.Register,
			homeAPI.Register,
			feedbackAPI.Register,
			settlementAPI.Register,
		)
		if err := srv.Run(ctx); err != nil {
			logger.Error("api shutdown", "err", err)
			os.Exit(1)
		}
	case config.RoleMCPStdio:
		// Same MCPServer wiring as the api role's /mcp, but over stdin/stdout
		// for local clients. Single-client → resolve the bearer user once at boot.
		token := os.Getenv("MCP_BEARER_TOKEN")
		if token == "" {
			logger.Error("MCP_BEARER_TOKEN required for mcp-stdio role")
			os.Exit(2)
		}

		pool, err := db.NewPoolWithConfig(ctx, cfg.DatabaseRW, db.PoolConfig{MaxConns: cfg.DBMaxConns, MinConns: cfg.DBMinConns})
		if err != nil {
			logger.Error("pg pool", "err", err)
			os.Exit(1)
		}
		defer pool.Close()
		rdb, err := cache.NewClient(ctx, cfg.RedisURL)
		if err != nil {
			logger.Error("redis", "err", err)
			os.Exit(1)
		}
		defer rdb.Close()

		// Construct the shared service graph (same wiring the api role uses).
		// Storage is nil here: no MCP tool currently exercises document upload
		// (only audit.query, which uses AuditQry).
		cs, err := buildCoreServices(ctx, pool, rdb, cfg, nil, logger)
		if err != nil {
			logger.Error("build services", "err", err)
			os.Exit(1)
		}

		// Resolve the user that owns the bearer token once at boot — stdio is
		// single-client so we don't re-check per request.
		sess, err := cs.SessStore.Get(ctx, token)
		if err != nil {
			logger.Error("invalid MCP_BEARER_TOKEN", "err", err)
			os.Exit(1)
		}
		user, err := cs.UserRepo.GetByID(ctx, sess.UserID)
		if err != nil {
			logger.Error("user lookup", "err", err)
			os.Exit(1)
		}

		mcpSrv := mcpserver.New(mcpserver.Deps{
			Pool:       pool,
			Audit:      cs.AuditRepo,
			Order:      cs.Order,
			Vendor:     cs.Vendor,
			Menu:       cs.Menu,
			Payroll:    cs.Payroll,
			Compliance: cs.Compliance,
			Feedback:   cs.Feedback,
			Settlement: cs.Settlement,
			Users:      cs.UserRepo,
			Sessions:   cs.SessStore,
		})

		stdioSrv := mcpgo.NewStdioServer(mcpSrv)
		stdioSrv.SetContextFunc(func(c context.Context) context.Context {
			return idhttp.ContextWithUser(c, user)
		})

		logger.Info("mcp-stdio starting", "user_email", user.PrimaryEmail, "user_role", string(user.Role))
		if err := stdioSrv.Listen(ctx, os.Stdin, os.Stdout); err != nil && !errors.Is(err, context.Canceled) {
			logger.Error("mcp stdio", "err", err)
			os.Exit(1)
		}
		logger.Info("mcp stdio shutdown")
	}
}

func buildOIDCProviders(ctx context.Context, cfg config.Config) (map[string]oidc.Provider, error) {
	if len(cfg.AuthProviders) == 0 {
		return nil, errors.New("AUTH_PROVIDER_SLUGS must configure at least one OIDC provider")
	}
	providers := make(map[string]oidc.Provider, len(cfg.AuthProviders))
	for _, p := range cfg.AuthProviders {
		if !validProviderSlug(p.Slug) {
			return nil, fmt.Errorf("invalid auth provider slug %q", p.Slug)
		}
		redirectURL := cfg.OIDCCallbackBaseURL + "/auth/" + p.Slug + "/callback"
		provider, err := oidc.New(ctx, oidc.Config{
			Slug:         p.Slug,
			DisplayName:  p.DisplayName,
			IssuerURL:    p.IssuerURL,
			ClientID:     p.ClientID,
			ClientSecret: p.ClientSecret,
			RedirectURL:  redirectURL,
			Scopes:       p.Scopes,
		})
		if err != nil {
			return nil, err
		}
		providers[p.Slug] = provider
	}
	return providers, nil
}

func newAuthentikProvisioner(cfg config.Config) (identity.VendorOperatorProvisioner, error) {
	return idauthentik.New(idauthentik.Config{
		BaseURL:             cfg.AuthentikBaseURL,
		APIToken:            cfg.AuthentikAPIToken,
		Provider:            "authentik",
		VendorOperatorGroup: cfg.AuthentikVendorOperatorGroup,
	})
}

func validProviderSlug(slug string) bool {
	if slug == "" {
		return false
	}
	for i, r := range slug {
		ok := r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '_' || r == '-' || r == '.'
		if !ok {
			return false
		}
		if i == 0 && !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9') {
			return false
		}
	}
	return true
}

// getenv returns the value of the named env var, or def when unset/empty.
func getenv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

// appLocation is the business timezone used for order-cutoff math and the
// "today" derivation. Defaults to Asia/Taipei; override with APP_TIMEZONE.
// Falls back to UTC if the zone can't be loaded.
func appLocation() *time.Location {
	name := getenv("APP_TIMEZONE", "Asia/Taipei")
	loc, err := time.LoadLocation(name)
	if err != nil {
		slog.Warn("invalid APP_TIMEZONE; falling back to UTC", "value", name, "err", err)
		return time.UTC
	}
	return loc
}
