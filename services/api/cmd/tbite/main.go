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
	"github.com/jackc/pgx/v5/pgxpool"
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

	appRunners := map[config.Role]func(context.Context, *slog.Logger, config.Config) error{
		config.RoleAPI:      runAPI,
		config.RoleMCPStdio: runMCPStdio,
	}
	runner, ok := appRunners[role]
	if !ok {
		logger.Error("unknown role", "role", string(role))
		os.Exit(2)
	}
	if err := runner(ctx, logger, cfg); err != nil {
		logger.Error(string(role), "err", err)
		os.Exit(1)
	}
}

// noopCleanup is the no-op cleanup returned by setup helpers that haven't
// acquired any resource yet (early-error paths and the NATS-off branch).
// Callers can `defer cleanup()` unconditionally.
func noopCleanup() {}

type apiInfra struct {
	pool   *pgxpool.Pool
	roPool *pgxpool.Pool
	rdb    *cache.Client
	s3     *storage.S3Client
}

type hydraBits struct {
	bridge    *hydra.Bridge
	proxy     http.Handler
	discovery *hydra.DiscoveryShim
}

func runAPI(ctx context.Context, logger *slog.Logger, cfg config.Config) error {
	inf, cleanup, err := initAPIInfra(ctx, cfg, logger)
	if err != nil {
		return err
	}
	defer cleanup()

	cs, err := buildCoreServices(ctx, inf.pool, inf.rdb, cfg, inf.s3, logger)
	if err != nil {
		return fmt.Errorf("build services: %w", err)
	}
	if err := qpgrepo.RegisterSupplyGauges(inf.pool); err != nil {
		logger.Warn("register supply gauges", "err", err)
	}

	providers, err := buildOIDCProviders(ctx, cfg)
	if err != nil {
		return fmt.Errorf("oidc providers: %w", err)
	}
	idRepo := pgrepo.NewUserIdentityRepo(inf.pool)
	stateStore := oidc.NewRedisStateStore(inf.rdb, 5*time.Minute)
	idAPI := newIdentityAPI(cs, providers, idRepo, stateStore, cfg)

	hb, err := setupHydraBridge(ctx, cfg, cs, idRepo, stateStore, idAPI, logger)
	if err != nil {
		return err
	}

	boardHub := order.NewBoardHub()
	menuHub := order.NewMenuHub()
	dlqAPI, natsCleanup := setupBoardSSEAndDLQ(ctx, cfg, inf.pool, boardHub, menuHub, logger)
	defer natsCleanup()

	menuAPI := &mhttp.API{
		Svc:                  cs.Menu,
		Storage:              inf.s3,
		StoragePublicBaseURL: cfg.S3PublicBaseURL,
		StorageBucket:        cfg.S3Bucket,
	}
	homeAPI, favoritesAPI, reorderAPI := setupHomeAndReadModel(ctx, cfg, cs, inf, logger)

	mcpSrv := mcpserver.New(mcpserver.Deps{
		Pool: inf.pool, Audit: cs.AuditRepo, Order: cs.Order, Vendor: cs.Vendor,
		Menu: cs.Menu, Payroll: cs.Payroll, Compliance: cs.Compliance,
		Feedback: cs.Feedback, Settlement: cs.Settlement, Users: cs.UserRepo,
		Sessions: cs.SessStore,
	})

	srv := httpserver.New(cfg.HTTPAddr, logger, idAPI,
		hydraRouter(hb, menuAPI, cfg.HydraPublicURL),
		mcpSrv,
		httpserver.MCPOpts{PublicBaseURL: cfg.OIDCCallbackBaseURL, AuthorizationServers: mcpAuthServers(cfg)},
		(&vhttp.API{Svc: cs.Vendor}).Register,
		(&phttp.API{Svc: &plants.Service{Repo: ppgrepo.NewPlantRepo(inf.pool)}, VendorSvc: cs.Vendor}).Register,
		menuAPI.Register,
		menuAPI.RegisterPresigned,
		(&qhttp.API{Svc: &quota.Service{Supplies: cs.SupplyRepo, Items: cs.ItemRepo}}).Register,
		(&ohttp.API{Svc: cs.Order, Board: boardHub, MenuHub: menuHub}).Register,
		(&payrollhttp.API{Svc: cs.Payroll}).Register,
		(&chttp.API{Svc: cs.Compliance}).Register,
		dlqAPI.Register,
		favoritesAPI.Register,
		reorderAPI.Register,
		homeAPI.Register,
		(&feedbackhttp.API{Svc: cs.Feedback}).Register,
		(&settlementhttp.API{Svc: cs.Settlement}).Register,
	)
	if err := srv.Run(ctx); err != nil {
		return fmt.Errorf("api shutdown: %w", err)
	}
	return nil
}

func initAPIInfra(ctx context.Context, cfg config.Config, logger *slog.Logger) (*apiInfra, func(), error) {
	pool, err := db.NewPoolWithConfig(ctx, cfg.DatabaseRW, db.PoolConfig{MaxConns: cfg.DBMaxConns, MinConns: cfg.DBMinConns})
	if err != nil {
		return nil, noopCleanup, fmt.Errorf("pg pool: %w", err)
	}
	_ = db.RegisterPoolMetrics(pool, "rw")

	roPool, err := newROPool(ctx, cfg)
	if err != nil {
		pool.Close()
		return nil, noopCleanup, fmt.Errorf("pg ro pool: %w", err)
	}

	rdb, err := cache.NewClient(ctx, cfg.RedisURL)
	if err != nil {
		roPool.Close()
		pool.Close()
		return nil, noopCleanup, fmt.Errorf("redis: %w", err)
	}

	s3API, err := storage.NewS3(ctx, storage.S3Config{
		Endpoint:        cfg.S3Endpoint,
		Region:          cfg.S3Region,
		AccessKeyID:     cfg.S3AccessKeyID,
		SecretAccessKey: cfg.S3SecretAccessKey,
		Bucket:          cfg.S3Bucket,
		UsePathStyle:    cfg.S3UsePathStyle,
	})
	if err != nil {
		rdb.Close()
		roPool.Close()
		pool.Close()
		return nil, noopCleanup, fmt.Errorf("s3: %w", err)
	}
	if err := s3API.EnsureBucket(ctx); err != nil {
		logger.Warn("ensure bucket failed; uploads will fail until storage is reachable", "err", err)
	}

	cleanup := func() {
		rdb.Close()
		roPool.Close()
		pool.Close()
	}
	return &apiInfra{pool: pool, roPool: roPool, rdb: rdb, s3: s3API}, cleanup, nil
}

func newIdentityAPI(cs *coreServices, providers map[string]oidc.Provider, idRepo identity.UserIdentityRepository, stateStore oidc.StateStore, cfg config.Config) *idhttp.API {
	return &idhttp.API{
		Svc: &identity.Service{
			Users:      cs.UserRepo,
			Identities: idRepo,
			Sessions:   cs.SessStore,
			Providers:  providers,
			States:     stateStore,
		},
		Sessions: cs.SessStore,
		Users:    cs.UserRepo,
		Handoff:  cs.SessStore,
		AppURLs: idhttp.AppBaseURLs{
			"employee": cfg.AppBaseURLEmployee,
			"merchant": cfg.AppBaseURLMerchant,
			"admin":    cfg.AppBaseURLAdmin,
		},
	}
}

// setupHydraBridge wires the Hydra OAuth bridge (only when HYDRA_PUBLIC_URL
// is set). We reverse-proxy Hydra under our host so MCP clients see one
// issuer (matches Hydra's URLS_SELF_ISSUER) and a CORS-clean origin.
func setupHydraBridge(ctx context.Context, cfg config.Config, cs *coreServices, idRepo identity.UserIdentityRepository, stateStore oidc.StateStore, api *idhttp.API, logger *slog.Logger) (*hydraBits, error) {
	if cfg.HydraPublicURL == "" {
		return &hydraBits{}, nil
	}
	publicIssuer := strings.TrimRight(cfg.OIDCCallbackBaseURL, "/") + "/"
	mcpResource := strings.TrimRight(cfg.OIDCCallbackBaseURL, "/") + "/mcp"
	tokVerifier, err := hydra.NewAccessTokenVerifier(ctx, cfg.HydraPublicURL, publicIssuer, mcpResource)
	if err != nil {
		return nil, fmt.Errorf("hydra access token verifier: %w", err)
	}
	api.JWT = hydra.SubjectVerifier{V: tokVerifier}

	bridgeOIDC, bridgeOIDCSlug, err := newBridgeOIDC(ctx, cfg)
	if err != nil {
		return nil, err
	}

	bridge := &hydra.Bridge{
		Hydra:            hydra.NewAdminClient(cfg.HydraAdminURL),
		Sessions:         cs.SessStore,
		Users:            cs.UserRepo,
		Identities:       idRepo,
		OIDCProvider:     bridgeOIDC,
		OIDCProviderName: bridgeOIDCSlug,
		States:           stateStore,
		PublicBaseURL:    cfg.OIDCCallbackBaseURL,
	}
	discovery := hydra.NewDiscoveryShim(cfg.HydraPublicURL)
	discovery.PublicBaseURL = strings.TrimRight(cfg.OIDCCallbackBaseURL, "/")
	proxy, err := hydra.ReverseProxy(cfg.HydraPublicURL)
	if err != nil {
		return nil, fmt.Errorf("hydra reverse proxy: %w", err)
	}
	logger.Info("hydra bridge wired", "public_url", cfg.HydraPublicURL, "admin_url", cfg.HydraAdminURL, "issuer", publicIssuer)
	return &hydraBits{bridge: bridge, proxy: proxy, discovery: discovery}, nil
}

// newBridgeOIDC builds the OIDC provider the MCP /oauth/callback uses (web's
// /auth/{slug}/callback runs through its own provider map).
func newBridgeOIDC(ctx context.Context, cfg config.Config) (*oidc.OIDCProvider, string, error) {
	if len(cfg.AuthProviders) == 0 {
		return nil, "", nil
	}
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
		return nil, "", fmt.Errorf("mcp oidc provider: %w", err)
	}
	return bp, p.Slug, nil
}

// setupBoardSSEAndDLQ wires the DLQ admin handlers and (if NATS is up) the
// merchant prep-board SSE consumer. Returns a no-op cleanup when NATS is off.
func setupBoardSSEAndDLQ(ctx context.Context, cfg config.Config, pool *pgxpool.Pool, boardHub *order.BoardHub, menuHub *order.MenuHub, logger *slog.Logger) (*dlqhttp.API, func()) {
	dlqAPI := &dlqhttp.API{Repo: dlqpgrepo.NewDLQRepo(pool)}
	if err := dlqpgrepo.RegisterDLQGauges(pool); err != nil {
		logger.Warn("register dlq gauges", "err", err)
	}
	if cfg.NATSURL == "" {
		return dlqAPI, noopCleanup
	}
	natsClient, err := messaging.New(ctx, cfg.NATSURL)
	if err != nil {
		logger.Warn("nats unavailable for dlq replay; /replay will return 503", "err", err)
		return dlqAPI, noopCleanup
	}
	dlqAPI.JS = natsClient.JS
	go func() {
		if err := order.RunBoardConsumer(ctx, natsClient.JS, boardHub, menuHub, logger); err != nil {
			logger.Warn("board consumer stopped", "err", err)
		}
	}()
	return dlqAPI, natsClient.Close
}

// setupHomeAndReadModel constructs the personalisation handlers (favorites,
// reorder, home) plus the cached read-model adapters and starts the
// invalidator goroutine.
func setupHomeAndReadModel(ctx context.Context, cfg config.Config, cs *coreServices, inf *apiInfra, logger *slog.Logger) (*mhttp.HomeAPI, *mhttp.FavoritesAPI, *ohttp.ReorderAPI) {
	favoriteRepo := mpgrepo.NewFavoriteRepo(inf.pool)
	favoritesAPI := &mhttp.FavoritesAPI{Svc: menu.NewFavoritesService(favoriteRepo)}

	reorderAPI := &ohttp.ReorderAPI{Svc: order.NewReorderService(
		inf.pool, cs.OrderRepo,
		p9SupplyRepoAdapter{inner: cs.SupplyRepo},
		p9ItemRepoAdapter{inner: cs.ItemRepo},
		cs.VendorRepo, cs.PlantRepo, cs.StateEventRepo, cs.AuditRepo, cs.OutboxRepo,
		clock.SystemClock{}, appLocation(),
	)}

	homeCache := &readmodel.RedisCache{C: inf.rdb, Prefix: "tbite:rm:"}
	homeMetrics := readmodel.NewMetrics()
	popularityRepo := mpgrepo.NewPopularityRepo(inf.roPool)
	homeSvc := &menu.HomeService{
		Clock:         clock.SystemClock{},
		ServerTZ:      appLocation(),
		RecentOrders:  opgrepo.NewRecentOrdersRepo(inf.roPool),
		Popularity:    cachedPopularityAdapter{cached: &readmodel.CachedPopularity{Inner: popularityRepo, Cache: homeCache, Metrics: homeMetrics}, repo: popularityRepo},
		Affinity:      &readmodel.CachedAffinity{Inner: mpgrepo.NewAffinityRepo(inf.roPool), Cache: homeCache, Metrics: homeMetrics},
		FavoritesRepo: favoriteRepo,
		Alpha:         recommendationAlpha(logger),
		VendorNames:   vendorNamesLookup(inf.roPool),
	}
	homeAPI := &mhttp.HomeAPI{Home: homeSvc, MenuSvc: cs.Menu, Cache: homeCache, CacheTTL: 30 * time.Second, CacheMetric: homeMetrics}

	if cfg.NATSURL != "" {
		go runReadModelInvalidator(ctx, cfg.NATSURL, homeCache, logger)
	}
	return homeAPI, favoritesAPI, reorderAPI
}

func recommendationAlpha(logger *slog.Logger) float64 {
	raw := os.Getenv("RECOMMENDATION_ALPHA")
	if raw == "" {
		return 1.0
	}
	v, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		logger.Warn("invalid RECOMMENDATION_ALPHA; defaulting to 1.0", "raw", raw)
		return 1.0
	}
	return v
}

func vendorNamesLookup(roPool *pgxpool.Pool) func(context.Context, []string) (map[string]string, error) {
	return func(ctx context.Context, ids []string) (map[string]string, error) {
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
	}
}

func runReadModelInvalidator(ctx context.Context, natsURL string, homeCache *readmodel.RedisCache, logger *slog.Logger) {
	natsClient, err := messaging.New(ctx, natsURL)
	if err != nil {
		logger.Warn("readmodel invalidator: nats unavailable", "err", err)
		return
	}
	defer natsClient.Close()
	if err := readmodel.RunOrderInvalidator(ctx, natsClient.JS, homeCache, logger.With("component", "readmodel-invalidator")); err != nil {
		logger.Warn("readmodel invalidator stopped", "err", err)
	}
}

func mcpAuthServers(cfg config.Config) []string {
	if cfg.HydraPublicURL != "" {
		return []string{strings.TrimRight(cfg.OIDCCallbackBaseURL, "/") + "/"}
	}
	servers := make([]string, 0, len(cfg.AuthProviders))
	for _, p := range cfg.AuthProviders {
		servers = append(servers, p.IssuerURL)
	}
	return servers
}

func hydraRouter(hb *hydraBits, menuAPI *mhttp.API, hydraPublicURL string) func(chi.Router) {
	return func(r chi.Router) {
		r.Post("/api/merchant/uploads", menuAPI.HandleDirectUpload)
		if hb.bridge == nil {
			return
		}
		r.Get("/oauth/login", hb.bridge.LoginHandler)
		r.Get("/oauth/callback", hb.bridge.CallbackHandler)
		r.Get("/oauth/consent", hb.bridge.ConsentHandler)
		r.Get("/.well-known/openid-configuration", hb.discovery.ServeHTTP)
		r.Get("/.well-known/oauth-authorization-server", hb.discovery.ServeHTTP)
		dcrProxy := &hydra.SanitizingDCRProxy{HydraURL: hydraPublicURL}
		r.Handle("/oauth2/register", dcrProxy)
		r.Handle("/oauth2/register/*", dcrProxy)
		r.Handle("/oauth2/*", hb.proxy)
		r.Get("/.well-known/jwks.json", hb.proxy.ServeHTTP)
		r.Get("/userinfo", hb.proxy.ServeHTTP)
	}
}

func runMCPStdio(ctx context.Context, logger *slog.Logger, cfg config.Config) error {
	// Same MCPServer wiring as the api role's /mcp, but over stdin/stdout
	// for local clients. Single-client → resolve the bearer user once at boot.
	token := os.Getenv("MCP_BEARER_TOKEN")
	if token == "" {
		return errors.New("MCP_BEARER_TOKEN required for mcp-stdio role")
	}

	pool, err := db.NewPoolWithConfig(ctx, cfg.DatabaseRW, db.PoolConfig{MaxConns: cfg.DBMaxConns, MinConns: cfg.DBMinConns})
	if err != nil {
		return fmt.Errorf("pg pool: %w", err)
	}
	defer pool.Close()
	rdb, err := cache.NewClient(ctx, cfg.RedisURL)
	if err != nil {
		return fmt.Errorf("redis: %w", err)
	}
	defer rdb.Close()

	// Construct the shared service graph (same wiring the api role uses).
	// Storage is nil here: no MCP tool currently exercises document upload
	// (only audit.query, which uses AuditQry).
	cs, err := buildCoreServices(ctx, pool, rdb, cfg, nil, logger)
	if err != nil {
		return fmt.Errorf("build services: %w", err)
	}

	// Resolve the user that owns the bearer token once at boot — stdio is
	// single-client so we don't re-check per request.
	sess, err := cs.SessStore.Get(ctx, token)
	if err != nil {
		return fmt.Errorf("invalid MCP_BEARER_TOKEN: %w", err)
	}
	user, err := cs.UserRepo.GetByID(ctx, sess.UserID)
	if err != nil {
		return fmt.Errorf("user lookup: %w", err)
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
		return fmt.Errorf("mcp stdio: %w", err)
	}
	logger.Info("mcp stdio shutdown")
	return nil
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
