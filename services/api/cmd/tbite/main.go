package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/google/uuid"
	mcpgo "github.com/mark3labs/mcp-go/server"
	"github.com/spf13/pflag"
	"golang.org/x/sync/errgroup"

	"github.com/takalawang/corporate-catering-system/services/api/internal/compliance"
	"github.com/takalawang/corporate-catering-system/services/api/internal/compliance/evaluator"
	chttp "github.com/takalawang/corporate-catering-system/services/api/internal/compliance/http"
	cpgrepo "github.com/takalawang/corporate-catering-system/services/api/internal/compliance/postgres"
	cscanner "github.com/takalawang/corporate-catering-system/services/api/internal/compliance/scanner"
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
	"github.com/takalawang/corporate-catering-system/services/api/internal/identity/oidc"
	pgrepo "github.com/takalawang/corporate-catering-system/services/api/internal/identity/postgres"
	idredis "github.com/takalawang/corporate-catering-system/services/api/internal/identity/redis"
	"github.com/takalawang/corporate-catering-system/services/api/internal/mcpserver"
	"github.com/takalawang/corporate-catering-system/services/api/internal/menu"
	mhttp "github.com/takalawang/corporate-catering-system/services/api/internal/menu/http"
	mpgrepo "github.com/takalawang/corporate-catering-system/services/api/internal/menu/postgres"
	"github.com/takalawang/corporate-catering-system/services/api/internal/order"
	ohttp "github.com/takalawang/corporate-catering-system/services/api/internal/order/http"
	opgrepo "github.com/takalawang/corporate-catering-system/services/api/internal/order/postgres"
	relay "github.com/takalawang/corporate-catering-system/services/api/internal/order/relay"
	scheduler "github.com/takalawang/corporate-catering-system/services/api/internal/order/scheduler"
	"github.com/takalawang/corporate-catering-system/services/api/internal/payroll"
	payrollhttp "github.com/takalawang/corporate-catering-system/services/api/internal/payroll/http"
	payrollpgrepo "github.com/takalawang/corporate-catering-system/services/api/internal/payroll/postgres"
	payrollsettler "github.com/takalawang/corporate-catering-system/services/api/internal/payroll/settler"
	"github.com/takalawang/corporate-catering-system/services/api/internal/platform/cache"
	"github.com/takalawang/corporate-catering-system/services/api/internal/platform/clock"
	"github.com/takalawang/corporate-catering-system/services/api/internal/platform/db"
	"github.com/takalawang/corporate-catering-system/services/api/internal/platform/leader"
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
	pflag.StringVar(&roleStr, "role", "api", "binary role: api|worker|scheduler|mcp-stdio")
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
			AppURLs: idhttp.AppBaseURLs{
				"employee": cfg.AppBaseURLEmployee,
				"merchant": cfg.AppBaseURLMerchant,
				"admin":    cfg.AppBaseURLAdmin,
			},
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
		// BoardHub fans live order events to the merchant prep board over SSE.
		// It is wired to NATS below when NATS_URL is configured.
		boardHub := order.NewBoardHub()
		orderAPI := &ohttp.API{Svc: orderService, Board: boardHub}

		// 7f. Payroll service + admin/employee handlers
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
		complianceService := &compliance.Service{
			Pool:     pool,
			Docs:     cpgrepo.NewDocumentRepo(pool),
			Anomaly:  cpgrepo.NewAnomalyRepo(pool),
			Storage:  s3API,
			Audit:    auditRepo,
			Outbox:   outboxRepo,
			AuditQry: auditRepo,
			Vendors:  vpgrepo.NewVendorRepo(pool),
			Clock:    clock.SystemClock{},
		}
		complianceAPI := &chttp.API{Svc: complianceService}

		// 7h. DLQ admin handlers (list/replay/resolve). NATS is optional: when
		// NATS_URL is set we wire JetStream so /replay can re-publish; otherwise
		// /replay returns 503 and only list/resolve work.
		dlqRepo := dlqpgrepo.NewDLQRepo(pool)
		dlqAPI := &dlqhttp.API{Repo: dlqRepo}
		if cfg.NATSURL != "" {
			if natsClient, err := messaging.New(ctx, cfg.NATSURL); err == nil {
				dlqAPI.JS = natsClient.JS
				defer natsClient.Close()
				// Tap ORDERS_V1 so the merchant prep board SSE endpoint can
				// push live updates. Failure here is non-fatal: the board
				// still works, just without push.
				go func() {
					if err := order.RunBoardConsumer(ctx, natsClient.JS, boardHub, logger); err != nil {
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
			stateEventRepo,
			auditRepo,
			outboxRepo,
			clock.SystemClock{},
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
		popularityRepo := mpgrepo.NewPopularityRepo(pool)
		affinityRepo := mpgrepo.NewAffinityRepo(pool)
		recentOrdersRepo := opgrepo.NewRecentOrdersRepo(pool)
		homeSvc := &menu.HomeService{
			Clock:         clock.SystemClock{},
			ServerTZ:      time.Local,
			RecentOrders:  recentOrdersRepo,
			Popularity:    popularityRepo,
			Affinity:      affinityRepo,
			FavoritesRepo: favoriteRepo,
			Alpha:         alpha,
			VendorNames: func(ctx context.Context, ids []string) (map[string]string, error) {
				out := map[string]string{}
				if len(ids) == 0 {
					return out, nil
				}
				rows, err := pool.Query(ctx, `SELECT id, display_name FROM vendor WHERE id = ANY($1)`, ids)
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
		homeAPI := &mhttp.HomeAPI{Home: homeSvc, MenuSvc: menuService}

		// 7j. Feedback (F1): employee meal ratings + complaint workflow.
		feedbackService := &feedback.Service{
			Pool:       pool,
			Ratings:    fpg.NewRatingRepo(pool),
			Complaints: fpg.NewComplaintRepo(pool),
			Orders:     fpg.NewOrderReader(pool),
			Audit:      auditRepo,
			Clock:      clock.SystemClock{},
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
			Payroll:    payrollService,
			Compliance: complianceService,
			Feedback:   feedbackService,
			Settlement: settlementService,
			Users:      userRepo,
			Sessions:   sessStore,
		})

		srv := httpserver.New(cfg.HTTPAddr, logger, api, nil, mcpSrv,
			vendorAPI.Register,
			menuAPI.Register,
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
			NATS:   natsClient,
			Logger: logger.With("component", "outbox-relay"),
			Batch:  100,
			Sleep:  500 * time.Millisecond,
		}

		// S3 client for the payroll settler. We construct + EnsureBucket at
		// boot so the worker fails fast if object storage is misconfigured,
		// rather than blowing up on the first batch_locked event.
		s3Client, err := storage.NewS3(ctx, storage.S3Config{
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
		if err := s3Client.EnsureBucket(ctx); err != nil {
			logger.Error("ensure bucket", "err", err)
			os.Exit(1)
		}

		settler := &payrollsettler.Settler{
			JS:      natsClient.JS,
			Pool:    pool,
			Batches: payrollpgrepo.NewBatchRepo(pool),
			Entries: payrollpgrepo.NewEntryRepo(pool),
			Users:   payrollsettler.NewPgUserLookup(pool),
			Storage: s3Client,
			Logger:  logger.With("component", "payroll-settler"),
			Audit:   opgrepo.NewAuditRepo(pool),
			Outbox:  outbox,
		}

		onTimeEval := &evaluator.OnTimeRateEvaluator{
			JS:      natsClient.JS,
			Anomaly: cpgrepo.NewAnomalyRepo(pool),
			Logger:  logger.With("component", "on-time-evaluator"),
		}

		logger.Info("worker starting (outbox-relay + payroll-settler + on-time-evaluator)")
		eg, egctx := errgroup.WithContext(ctx)
		eg.Go(func() error { return r.Run(egctx) })
		eg.Go(func() error { return settler.Run(egctx) })
		eg.Go(func() error { return onTimeEval.Run(egctx) })
		if err := eg.Wait(); err != nil && !errors.Is(err, context.Canceled) {
			logger.Error("worker shutdown", "err", err)
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

		docScanner := &cscanner.DocumentExpiryScanner{
			Pool:       pool,
			Docs:       cpgrepo.NewDocumentRepo(pool),
			Anomaly:    cpgrepo.NewAnomalyRepo(pool),
			Interval:   1 * time.Hour,
			DaysWindow: 14,
			Logger:     logger.With("component", "doc-expiry"),
			Clock:      clock.SystemClock{},
		}
		if v := os.Getenv("DOC_EXPIRY_INTERVAL"); v != "" {
			if d, err := time.ParseDuration(v); err == nil {
				docScanner.Interval = d
			}
		}

		// FeedbackScanner (F1): per-vendor rolling-window aggregation of meal
		// ratings + complaints, opening satisfaction_drop / complaint_spike
		// anomalies. Mirrors docScanner — single-replica under the same lease.
		feedbackScanner := &feedback.FeedbackScanner{
			Ratings:    fpg.NewRatingRepo(pool),
			Complaints: fpg.NewComplaintRepo(pool),
			Anomaly:    cpgrepo.NewAnomalyRepo(pool),
			Clock:      clock.SystemClock{},
			Logger:     logger.With("component", "feedback-scanner"),
			Interval:   1 * time.Hour,
			Window:     14 * 24 * time.Hour,
		}
		if v := os.Getenv("FEEDBACK_SCAN_INTERVAL"); v != "" {
			if d, err := time.ParseDuration(v); err == nil {
				feedbackScanner.Interval = d
			}
		}
		if v := os.Getenv("FEEDBACK_SCAN_WINDOW"); v != "" {
			if d, err := time.ParseDuration(v); err == nil {
				feedbackScanner.Window = d
			}
		}

		logger.Info("scheduler starting", "cutoff_interval", cutoffInterval, "noshow_interval", noShow.Interval, "noshow_max_age", noShow.MaxAge, "doc_expiry_interval", docScanner.Interval)

		leaseName := getenv("SCHEDULER_LEASE_NAME", "tbite-scheduler")
		leaseNS := getenv("SCHEDULER_LEASE_NAMESPACE", "tbite")
		identity := getenv("POD_NAME", "local-"+uuid.NewString())

		onLeading := func(leadCtx context.Context) error {
			eg, egctx := errgroup.WithContext(leadCtx)
			eg.Go(func() error { return cutoff.Run(egctx, cutoffInterval) })
			eg.Go(func() error { return noShow.Run(egctx) })
			eg.Go(func() error { return docScanner.Run(egctx) })
			eg.Go(func() error { return feedbackScanner.Run(egctx) })
			if err := eg.Wait(); err != nil && !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
				return err
			}
			return nil
		}

		if err := leader.RunWithLease(ctx, leader.Config{
			Namespace: leaseNS,
			LeaseName: leaseName,
			Identity:  identity,
			Logger:    logger.With("component", "leader"),
		}, onLeading); err != nil && !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
			logger.Error("scheduler shutdown", "err", err)
			os.Exit(1)
		}
		logger.Info("scheduler shutdown")
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

		pool, err := db.NewPool(ctx, cfg.DatabaseRW)
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
			Clock:       clock.SystemClock{},
		}
		vendorService := &vendor.Service{
			Vendors:     vpgrepo.NewVendorRepo(pool),
			Plants:      plantRepo,
			Operators:   vpgrepo.NewOperatorRepo(pool),
			Provisioner: authentikProvisioner,
			Users:       userRepo,
			Sessions:    sessStore,
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
			Pool:     pool,
			Docs:     cpgrepo.NewDocumentRepo(pool),
			Anomaly:  cpgrepo.NewAnomalyRepo(pool),
			Storage:  nil, // not needed for read-only MCP tools
			Audit:    auditRepo,
			Outbox:   outboxRepo,
			AuditQry: auditRepo,
			Clock:    clock.SystemClock{},
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
