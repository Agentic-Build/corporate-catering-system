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

	"github.com/takalawang/corporate-catering-system/services/api/internal/compliance"
	chttp "github.com/takalawang/corporate-catering-system/services/api/internal/compliance/http"
	cpgrepo "github.com/takalawang/corporate-catering-system/services/api/internal/compliance/postgres"
	"github.com/takalawang/corporate-catering-system/services/api/internal/config"
	dlqhttp "github.com/takalawang/corporate-catering-system/services/api/internal/dlq/http"
	dlqpgrepo "github.com/takalawang/corporate-catering-system/services/api/internal/dlq/postgres"
	"github.com/takalawang/corporate-catering-system/services/api/internal/feedback"
	feedbackhttp "github.com/takalawang/corporate-catering-system/services/api/internal/feedback/http"
	fpg "github.com/takalawang/corporate-catering-system/services/api/internal/feedback/postgres"
	"github.com/takalawang/corporate-catering-system/services/api/internal/httpserver"
	"github.com/takalawang/corporate-catering-system/services/api/internal/identity"
	idauthentik "github.com/takalawang/corporate-catering-system/services/api/internal/identity/authentik"
	idhttp "github.com/takalawang/corporate-catering-system/services/api/internal/identity/http"
	"github.com/takalawang/corporate-catering-system/services/api/internal/identity/hydra"
	"github.com/takalawang/corporate-catering-system/services/api/internal/identity/oidc"
	pgrepo "github.com/takalawang/corporate-catering-system/services/api/internal/identity/postgres"
	idredis "github.com/takalawang/corporate-catering-system/services/api/internal/identity/redis"
	"github.com/takalawang/corporate-catering-system/services/api/internal/mcpserver"
	"github.com/takalawang/corporate-catering-system/services/api/internal/menu"
	mhttp "github.com/takalawang/corporate-catering-system/services/api/internal/menu/http"
	mpgrepo "github.com/takalawang/corporate-catering-system/services/api/internal/menu/postgres"
	"github.com/takalawang/corporate-catering-system/services/api/internal/menu/readmodel"
	"github.com/takalawang/corporate-catering-system/services/api/internal/order"
	ohttp "github.com/takalawang/corporate-catering-system/services/api/internal/order/http"
	opgrepo "github.com/takalawang/corporate-catering-system/services/api/internal/order/postgres"
	"github.com/takalawang/corporate-catering-system/services/api/internal/payroll"
	payrollhttp "github.com/takalawang/corporate-catering-system/services/api/internal/payroll/http"
	payrollpgrepo "github.com/takalawang/corporate-catering-system/services/api/internal/payroll/postgres"
	"github.com/takalawang/corporate-catering-system/services/api/internal/plants"
	phttp "github.com/takalawang/corporate-catering-system/services/api/internal/plants/http"
	ppgrepo "github.com/takalawang/corporate-catering-system/services/api/internal/plants/postgres"
	"github.com/takalawang/corporate-catering-system/services/api/internal/platform/cache"
	"github.com/takalawang/corporate-catering-system/services/api/internal/platform/clock"
	"github.com/takalawang/corporate-catering-system/services/api/internal/platform/db"
	messaging "github.com/takalawang/corporate-catering-system/services/api/internal/platform/messaging"
	"github.com/takalawang/corporate-catering-system/services/api/internal/platform/observability"
	"github.com/takalawang/corporate-catering-system/services/api/internal/platform/storage"
	"github.com/takalawang/corporate-catering-system/services/api/internal/quota"
	qhttp "github.com/takalawang/corporate-catering-system/services/api/internal/quota/http"
	qpgrepo "github.com/takalawang/corporate-catering-system/services/api/internal/quota/postgres"
	"github.com/takalawang/corporate-catering-system/services/api/internal/settlement"
	settlementhttp "github.com/takalawang/corporate-catering-system/services/api/internal/settlement/http"
	settlementpg "github.com/takalawang/corporate-catering-system/services/api/internal/settlement/postgres"
	vendor "github.com/takalawang/corporate-catering-system/services/api/internal/vendors"
	vhttp "github.com/takalawang/corporate-catering-system/services/api/internal/vendors/http"
	vpgrepo "github.com/takalawang/corporate-catering-system/services/api/internal/vendors/postgres"
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

		// 1b. Read-only pool for the home/recommendation read-model paths
		// (ADR-0007). newROPool targets DATABASE_RO_URL and falls back to the
		// primary when no replica is configured, so single-DB deployments are
		// unaffected while production offloads these eventual-consistency reads
		// to a replica.
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

		// 3. OIDC providers
		providers, err := buildOIDCProviders(ctx, cfg)
		if err != nil {
			logger.Error("oidc providers", "err", err)
			os.Exit(1)
		}

		// 4. Repositories
		userRepo := pgrepo.NewUserRepo(pool)
		idRepo := pgrepo.NewUserIdentityRepo(pool)

		// 5. Session store + OIDC state store
		sessStore := idredis.NewSessionStore(rdb, 7*24*time.Hour)
		stateStore := oidc.NewRedisStateStore(rdb, 5*time.Minute)

		// 6. Identity service
		svc := &identity.Service{
			Users:      userRepo,
			Identities: idRepo,
			Sessions:   sessStore,
			Providers:  providers,
			States:     stateStore,
		}

		// 7. HTTP API
		api := &idhttp.API{
			Svc:      svc,
			Sessions: sessStore,
			Users:    userRepo,
			Handoff:  sessStore,
			AppURLs: idhttp.AppBaseURLs{
				"employee": cfg.AppBaseURLEmployee,
				"merchant": cfg.AppBaseURLMerchant,
				"admin":    cfg.AppBaseURLAdmin,
			},
		}

		// 7a-bis. Hydra OAuth bridge — only wired when HYDRA_PUBLIC_URL is
		// set. We reverse-proxy Hydra's OAuth surface under our own host
		// so MCP clients see a single issuer and CORS-clean origin. Hydra
		// is configured with URLS_SELF_ISSUER=our_host so its discovery
		// doc and the iss claim it signs into tokens line up with what
		// clients fetch from our /.well-known/openid-configuration.
		var (
			hydraBridge    *hydra.Bridge
			hydraProxy     http.Handler
			hydraDiscovery *hydra.DiscoveryShim
		)
		if cfg.HydraPublicURL != "" {
			// JWT iss claim is OIDCCallbackBaseURL (our advertised host),
			// but the verifier needs a reachable URL to fetch the
			// discovery doc at boot — that's the Hydra container's
			// directly-reachable URL.
			publicIssuer := strings.TrimRight(cfg.OIDCCallbackBaseURL, "/") + "/"
			mcpResource := strings.TrimRight(cfg.OIDCCallbackBaseURL, "/") + "/mcp"
			tokVerifier, err := hydra.NewAccessTokenVerifier(ctx, cfg.HydraPublicURL, publicIssuer, mcpResource)
			if err != nil {
				logger.Error("hydra access token verifier", "err", err)
				os.Exit(1)
			}
			api.JWT = hydra.SubjectVerifier{V: tokVerifier}
			// Build a dedicated OIDC client for the MCP bridge with its own
			// redirect_uri (/oauth/callback). The web-frontend OIDC client
			// uses /auth/{slug}/callback; both clients are the same logical
			// Authentik application but they need separate redirect URI
			// registrations.
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
				Sessions:         sessStore,
				Users:            userRepo,
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

		// 7b. Vendor service + admin handlers
		authentikProvisioner, err := newAuthentikProvisioner(cfg)
		if err != nil {
			logger.Error("authentik provisioner", "err", err)
			os.Exit(1)
		}
		vendorService := &vendor.Service{
			Vendors:     vpgrepo.NewVendorRepo(pool),
			Plants:      vpgrepo.NewPlantMappingRepo(pool),
			Operators:   vpgrepo.NewOperatorRepo(pool),
			Provisioner: authentikProvisioner,
			Users:       userRepo,
			Sessions:    sessStore,
			Audit:       opgrepo.NewAuditRepo(pool),
		}
		vendorAPI := &vhttp.API{Svc: vendorService}

		// 7b-bis. Plant registry service + endpoints.
		plantRegistrySvc := &plants.Service{Repo: ppgrepo.NewPlantRepo(pool)}
		plantAPI := &phttp.API{Svc: plantRegistrySvc, VendorSvc: vendorService}

		// 7c. Menu service + merchant/employee handlers
		itemRepo := mpgrepo.NewItemRepo(pool)
		menuService := &menu.Service{
			Categories: mpgrepo.NewCategoryRepo(pool),
			Items:      itemRepo,
			Images:     mpgrepo.NewImageRepo(pool),
		}
		menuAPI := &mhttp.API{
			Svc:                  menuService,
			StoragePublicBaseURL: cfg.S3PublicBaseURL,
			StorageBucket:        cfg.S3Bucket,
		}

		// 7d. Quota service + merchant handlers (vendor capacity management)
		supplyRepo := qpgrepo.NewSupplyRepo(pool)
		if err := qpgrepo.RegisterSupplyGauges(pool); err != nil {
			logger.Warn("register supply gauges", "err", err)
		}
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
			Vendors:     vpgrepo.NewVendorRepo(pool),
			Clock:       clock.SystemClock{},
			Location:    appLocation(),
		}
		// BoardHub fans live order events to the merchant prep board over SSE.
		// It is wired to NATS below when NATS_URL is configured.
		boardHub := order.NewBoardHub()
		menuHub := order.NewMenuHub()
		orderAPI := &ohttp.API{Svc: orderService, Board: boardHub, MenuHub: menuHub}

		// 7f. Payroll service + admin/employee handlers
		payrollService := &payroll.Service{
			Pool:       pool,
			Batches:    payrollpgrepo.NewBatchRepo(pool),
			Entries:    payrollpgrepo.NewEntryRepo(pool),
			Disputes:   payrollpgrepo.NewDisputeRepo(pool),
			Exceptions: payrollpgrepo.NewExceptionRepo(pool),
			Orders:     orderRepo,
			OrderTx:    orderRepo,
			Audit:      auditRepo,
			Outbox:     outboxRepo,
			Clock:      clock.SystemClock{},
		}
		payrollAPI := &payrollhttp.API{Svc: payrollService}

		// 7g. Compliance service + admin handlers (vendor docs / anomalies /
		// audit query / dlq stub). S3 is constructed here as well as in worker
		// so document uploads have somewhere to land. If S3 is misconfigured we
		// fail fast — the api role now hard-depends on object storage.
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
		// Menu-item images travel via presigned PUT/GET or direct upload to
		// object storage. The API authorises both paths.
		menuAPI.Storage = s3API
		complianceService := &compliance.Service{
			Pool:      pool,
			Docs:      cpgrepo.NewDocumentRepo(pool),
			Anomaly:   cpgrepo.NewAnomalyRepo(pool),
			Storage:   s3API,
			Audit:     auditRepo,
			Outbox:    outboxRepo,
			AuditQry:  auditRepo,
			Vendors:   vpgrepo.NewVendorRepo(pool),
			VendorGov: vendorService,
			Clock:     clock.SystemClock{},
		}
		complianceAPI := &chttp.API{Svc: complianceService}

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
				// Tap ORDERS_V1 so the merchant prep board SSE endpoint can
				// push live updates. Failure here is non-fatal: the board
				// still works, just without push.
				go func() {
					if err := order.RunBoardConsumer(ctx, natsClient.JS, boardHub, menuHub, logger); err != nil {
						logger.Warn("board consumer stopped", "err", err)
					}
				}()
			} else {
				logger.Warn("nats unavailable for dlq replay; /replay will return 503", "err", err)
			}
		}

		// 7i. P9 personalisation: favorites, reorder, home aggregate. All three
		// share the existing repos; only favorites needs a new table (000008).
		favoriteRepo := mpgrepo.NewFavoriteRepo(pool)
		favoritesSvc := menu.NewFavoritesService(favoriteRepo)
		favoritesAPI := &mhttp.FavoritesAPI{Svc: favoritesSvc}

		reorderSvc := order.NewReorderService(
			pool,
			orderRepo,
			p9SupplyRepoAdapter{inner: supplyRepo},
			p9ItemRepoAdapter{inner: itemRepo},
			vpgrepo.NewVendorRepo(pool),
			plantRepo,
			stateEventRepo,
			auditRepo,
			outboxRepo,
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
		// Read-model repos read off the RO pool (ADR-0007): these aggregates
		// are already eventual-consistency (Redis-cached, NATS-invalidated), so
		// replica lag is within tolerance.
		popularityRepo := mpgrepo.NewPopularityRepo(roPool)
		affinityRepo := mpgrepo.NewAffinityRepo(roPool)
		recentOrdersRepo := opgrepo.NewRecentOrdersRepo(roPool)
		// Read-model cache (Valkey) shared by the employee home aggregate and
		// the recommendation aggregates. The namespace prefix scopes keys so
		// SCAN-based invalidation stays bounded.
		homeCache := &readmodel.RedisCache{C: rdb, Prefix: "tbite:rm:"}
		homeMetrics := readmodel.NewMetrics()
		// Popularity + affinity are recomputed from raw orders per request;
		// cache them so AC3 holds. Popularity is plant/date keyed and shared;
		// affinity is user keyed over a 30-day window. The outbox invalidator
		// drops both on order events; TTL is the safety net.
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
			MenuSvc:     menuService,
			Cache:       homeCache,
			CacheTTL:    30 * time.Second,
			CacheMetric: homeMetrics,
		}
		// The outbox-driven invalidator subscribes to ORDERS_V1 and
		// clears entries scoped to the affected plant/date. Failure
		// to start is non-fatal — the TTL is the safety net.
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

		// 7j. Feedback (F1): employee meal ratings + complaint workflow.
		// Reverser wires admin "resolve with compensation" to ReverseOrder so a
		// complaint resolved with `compensate=true` reverses the salary deduction.
		feedbackService := &feedback.Service{
			Pool:       pool,
			Ratings:    fpg.NewRatingRepo(pool),
			Complaints: fpg.NewComplaintRepo(pool),
			Orders:     fpg.NewOrderReader(pool),
			Audit:      auditRepo,
			Clock:      clock.SystemClock{},
			Reverser:   payrollService,
		}
		feedbackAPI := &feedbackhttp.API{Svc: feedbackService}

		// 7k. Vendor settlement (F2): monthly reconciliation + admin close.
		settlementRepo := settlementpg.NewSettlementRepo(pool)
		settlementService := &settlement.Service{
			Pool:        pool,
			Settlements: settlementRepo,
			Orders:      settlementRepo,
			Audit:       auditRepo,
		}
		settlementAPI := &settlementhttp.API{Svc: settlementService}

		// 8. HTTP server. Local and e2e auth now run through Authentik; there is
		// no fake OIDC route mounted by the API.

		// 9. MCP server (P7). Reuses the same service instances and the same
		// Bearer auth middleware as the REST API. Tools are registered in
		// subsequent P7 tasks; Task 1 mounts the skeleton at /mcp.
		mcpSrv := mcpserver.New(mcpserver.Deps{
			Pool:       pool,
			Audit:      auditRepo,
			Order:      orderService,
			Vendor:     vendorService,
			Menu:       menuService,
			Payroll:    payrollService,
			Compliance: complianceService,
			Feedback:   feedbackService,
			Settlement: settlementService,
			Users:      userRepo,
			Sessions:   sessStore,
		})

		// MCP OAuth metadata. When the Hydra sidecar is wired, advertise
		// OUR host as the authorization server: Hydra is configured with
		// URLS_SELF_ISSUER pointing at us, and the /.well-known/openid-
		// configuration discovery + DCR + /oauth2/* endpoints are reverse-
		// proxied at our host — so the JWT iss claim, discovery doc, and
		// PRM all line up on one origin. This is what makes Claude.ai /
		// ChatGPT's strict OAuth client trust Hydra-issued tokens via DCR.
		// When Hydra isn't wired, fall back to Authentik's issuer (bearer-
		// paste only, no DCR).
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

			// Hydra OAuth bridge — only mounted when the sidecar is wired.
			// These endpoints are anonymous: they're the URLs Hydra
			// redirects the user's browser to during the login/consent
			// dance, before any session exists.
			if hydraBridge != nil {
				r.Get("/oauth/login", hydraBridge.LoginHandler)
				r.Get("/oauth/callback", hydraBridge.CallbackHandler)
				r.Get("/oauth/consent", hydraBridge.ConsentHandler)
				// Discovery shim — wraps Hydra's /.well-known/openid-
				// configuration and injects registration_endpoint
				// (Hydra v2.2-2.3 omits it from the published doc).
				r.Get("/.well-known/openid-configuration", hydraDiscovery.ServeHTTP)
				r.Get("/.well-known/oauth-authorization-server", hydraDiscovery.ServeHTTP)
				// DCR (RFC 7591) — sanitizing proxy strips empty-string
				// optional URI fields from Hydra's response. Strict OAuth
				// clients (Claude Code, Claude.ai web, ChatGPT) reject
				// empty policy_uri/tos_uri/etc and would otherwise mark
				// the connection as failed during the registration step.
				dcrProxy := &hydra.SanitizingDCRProxy{HydraURL: cfg.HydraPublicURL}
				r.Handle("/oauth2/register", dcrProxy)
				r.Handle("/oauth2/register/*", dcrProxy)
				// Reverse proxy — Hydra's OAuth surface served under our
				// own host so URLs in the discovery doc and the iss
				// claim in JWTs line up.
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
		// mcp-stdio role: same MCPServer + tool wiring as the api role's /mcp
		// mount, but transported over stdin/stdout for local MCP clients
		// (Claude Code, Cursor, etc.). The user is resolved once at boot from
		// MCP_BEARER_TOKEN and attached to every request's context via
		// StdioServer.SetContextFunc — stdio is single-client so this is safe.
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

		userRepo := pgrepo.NewUserRepo(pool)
		sessStore := idredis.NewSessionStore(rdb, 7*24*time.Hour)

		// Resolve the user that owns the bearer token. We do this once at boot
		// rather than per-request because stdio is single-client.
		sess, err := sessStore.Get(ctx, token)
		if err != nil {
			logger.Error("invalid MCP_BEARER_TOKEN", "err", err)
			os.Exit(1)
		}
		user, err := userRepo.GetByID(ctx, sess.UserID)
		if err != nil {
			logger.Error("user lookup", "err", err)
			os.Exit(1)
		}

		// Wire the same services the api role uses for MCP. Compliance.Storage
		// is intentionally nil — no MCP tool currently exercises the document
		// upload path, only audit.query (which uses AuditQry).
		auditRepo := opgrepo.NewAuditRepo(pool)
		outboxRepo := opgrepo.NewOutboxRepo(pool)
		stateRepo := opgrepo.NewStateEventRepo(pool)
		orderRepo := opgrepo.NewOrderRepo(pool)
		supplyRepo := qpgrepo.NewSupplyRepo(pool)
		itemRepo := mpgrepo.NewItemRepo(pool)
		plantRepo := vpgrepo.NewPlantMappingRepo(pool)
		authentikProvisioner, err := newAuthentikProvisioner(cfg)
		if err != nil {
			logger.Error("authentik provisioner", "err", err)
			os.Exit(1)
		}

		orderService := &order.Service{
			Pool:        pool,
			Orders:      orderRepo,
			OrdersTx:    orderRepo,
			StateEvents: stateRepo,
			StateTx:     stateRepo,
			Audit:       auditRepo,
			AuditTx:     auditRepo,
			Outbox:      outboxRepo,
			OutboxTx:    outboxRepo,
			QuotaTx:     supplyRepo,
			Items:       itemRepo,
			Plants:      plantRepo,
			Vendors:     vpgrepo.NewVendorRepo(pool),
			Clock:       clock.SystemClock{},
			Location:    appLocation(),
		}
		vendorService := &vendor.Service{
			Vendors:     vpgrepo.NewVendorRepo(pool),
			Plants:      plantRepo,
			Operators:   vpgrepo.NewOperatorRepo(pool),
			Provisioner: authentikProvisioner,
			Users:       userRepo,
			Sessions:    sessStore,
			Audit:       opgrepo.NewAuditRepo(pool),
		}
		menuService := &menu.Service{
			Categories: mpgrepo.NewCategoryRepo(pool),
			Items:      itemRepo,
			Images:     mpgrepo.NewImageRepo(pool),
		}
		payrollService := &payroll.Service{
			Pool:     pool,
			Batches:  payrollpgrepo.NewBatchRepo(pool),
			Entries:  payrollpgrepo.NewEntryRepo(pool),
			Disputes: payrollpgrepo.NewDisputeRepo(pool),
			Orders:   orderRepo,
			OrderTx:  orderRepo,
			Audit:    auditRepo,
			Outbox:   outboxRepo,
			Clock:    clock.SystemClock{},
		}
		complianceService := &compliance.Service{
			Pool:      pool,
			Docs:      cpgrepo.NewDocumentRepo(pool),
			Anomaly:   cpgrepo.NewAnomalyRepo(pool),
			Storage:   nil, // not needed for read-only MCP tools
			Audit:     auditRepo,
			Outbox:    outboxRepo,
			AuditQry:  auditRepo,
			VendorGov: vendorService,
			Clock:     clock.SystemClock{},
		}
		feedbackService := &feedback.Service{
			Pool:       pool,
			Ratings:    fpg.NewRatingRepo(pool),
			Complaints: fpg.NewComplaintRepo(pool),
			Orders:     fpg.NewOrderReader(pool),
			Audit:      auditRepo,
			Clock:      clock.SystemClock{},
		}
		settlementRepo := settlementpg.NewSettlementRepo(pool)
		settlementService := &settlement.Service{
			Pool:        pool,
			Settlements: settlementRepo,
			Orders:      settlementRepo,
			Audit:       auditRepo,
		}

		mcpSrv := mcpserver.New(mcpserver.Deps{
			Pool:       pool,
			Audit:      auditRepo,
			Order:      orderService,
			Vendor:     vendorService,
			Menu:       menuService,
			Payroll:    payrollService,
			Compliance: complianceService,
			Feedback:   feedbackService,
			Settlement: settlementService,
			Users:      userRepo,
			Sessions:   sessStore,
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
