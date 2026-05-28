package main

// Cloud-native split roles. Each function is the entire process for a
// single Deployment. They share zero state; the only inputs are the
// parsed Config and a scoped Context. main.go's switch dispatches to
// them so ownership, scaling rule, and failure domain are visible at
// the Kubernetes object boundary.

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/google/uuid"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"golang.org/x/sync/errgroup"

	"github.com/Agentic-Build/corporate-catering-system/services/api/internal/compliance/evaluator"
	cpgrepo "github.com/Agentic-Build/corporate-catering-system/services/api/internal/compliance/postgres"
	cscanner "github.com/Agentic-Build/corporate-catering-system/services/api/internal/compliance/scanner"
	"github.com/Agentic-Build/corporate-catering-system/services/api/internal/config"
	"github.com/Agentic-Build/corporate-catering-system/services/api/internal/feedback"
	fpg "github.com/Agentic-Build/corporate-catering-system/services/api/internal/feedback/postgres"
	"github.com/Agentic-Build/corporate-catering-system/services/api/internal/httpserver"
	idhttp "github.com/Agentic-Build/corporate-catering-system/services/api/internal/identity/http"
	pgrepo "github.com/Agentic-Build/corporate-catering-system/services/api/internal/identity/postgres"
	idredis "github.com/Agentic-Build/corporate-catering-system/services/api/internal/identity/redis"
	"github.com/Agentic-Build/corporate-catering-system/services/api/internal/order"
	opgrepo "github.com/Agentic-Build/corporate-catering-system/services/api/internal/order/postgres"
	relay "github.com/Agentic-Build/corporate-catering-system/services/api/internal/order/relay"
	scheduler "github.com/Agentic-Build/corporate-catering-system/services/api/internal/order/scheduler"
	payrollpgrepo "github.com/Agentic-Build/corporate-catering-system/services/api/internal/payroll/postgres"
	payrollsettler "github.com/Agentic-Build/corporate-catering-system/services/api/internal/payroll/settler"
	"github.com/Agentic-Build/corporate-catering-system/services/api/internal/platform/cache"
	"github.com/Agentic-Build/corporate-catering-system/services/api/internal/platform/clock"
	"github.com/Agentic-Build/corporate-catering-system/services/api/internal/platform/db"
	"github.com/Agentic-Build/corporate-catering-system/services/api/internal/platform/leader"
	messaging "github.com/Agentic-Build/corporate-catering-system/services/api/internal/platform/messaging"
	"github.com/Agentic-Build/corporate-catering-system/services/api/internal/platform/storage"
)

// newRWPool constructs the RW pgxpool with the chart-supplied budget.
// All split roles share this constructor so a single env knob controls
// how many backend connections one replica may open.
func newRWPool(ctx context.Context, cfg config.Config) (*db.Pool, error) {
	return db.NewPoolWithConfig(ctx, cfg.DatabaseRW, db.PoolConfig{
		MaxConns: cfg.DBMaxConns,
		MinConns: cfg.DBMinConns,
	})
}

// newROPool returns a read-only pool aimed at the replica DSN. When
// DATABASE_RO_URL is unset, Config.EffectiveDatabaseRO selects the RW
// DSN so small deployments keep one database endpoint while prod caps
// the read budget separately.
func newROPool(ctx context.Context, cfg config.Config) (*db.Pool, error) {
	return db.NewPoolWithConfig(ctx, cfg.EffectiveDatabaseRO(), db.PoolConfig{
		MaxConns: cfg.DBMaxConnsRO,
		MinConns: cfg.DBMinConnsRO,
	})
}

// newNATS connects to NATS and returns the messaging client. Workers
// require NATS_URL; the helper fails fast when it is missing so the
// pod stays unready rather than appearing healthy while events go
// nowhere.
func newNATS(ctx context.Context, cfg config.Config) (*messaging.Client, error) {
	if cfg.NATSURL == "" {
		return nil, fmt.Errorf("NATS_URL is required for this role")
	}
	return messaging.New(ctx, cfg.NATSURL)
}

// serveProbes starts an HTTP server on PROBE_ADDR (default :2112) that
// exposes /healthz and /readyz with the supplied dependency checkers.
// Headless workers reuse this so their Deployments can probe actual
// runtime dependencies.
func serveProbes(ctx context.Context, logger *slog.Logger, deps ...httpserver.Checker) error {
	addr := os.Getenv("PROBE_ADDR")
	if addr == "" {
		addr = ":2112"
	}
	h := httpserver.NewHealth(deps...)
	mux := chi.NewRouter()
	mux.Use(middleware.Recoverer)
	mux.Get("/healthz", h.LivenessHandler())
	mux.Get("/readyz", h.ReadinessHandler())
	srv := &http.Server{Addr: addr, Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	go func() {
		<-ctx.Done()
		// drain liveness so kubelet stops sending traffic before we
		// tear down NATS subscriptions / in-flight DB transactions.
		h.SetLive(false)
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
	}()
	logger.Info("probe server listening", "addr", addr)
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

// runOutboxRelay drains the transactional outbox into JetStream. It
// has no scheduling state beyond the row-level advisory lock taken
// by the relay; horizontal scaling is safe because each replica locks
// a disjoint batch.
func runOutboxRelay(ctx context.Context, logger *slog.Logger, cfg config.Config) error {
	pool, err := newRWPool(ctx, cfg)
	if err != nil {
		return fmt.Errorf("pg pool: %w", err)
	}
	defer pool.Close()
	nc, err := newNATS(ctx, cfg)
	if err != nil {
		return err
	}
	defer nc.Close()

	r := &relay.Relay{
		Outbox: opgrepo.NewOutboxRepo(pool),
		NATS:   nc,
		Logger: logger.With("component", "outbox-relay"),
		Batch:  100,
		Sleep:  500 * time.Millisecond,
	}

	if err := opgrepo.RegisterOutboxGauges(pool); err != nil {
		logger.Warn("register outbox gauges", "err", err)
	}

	eg, egctx := errgroup.WithContext(ctx)
	eg.Go(func() error {
		return serveProbes(egctx, logger,
			httpserver.PostgresChecker("postgres-rw", pool),
			httpserver.NATSChecker("nats", nc.NC),
		)
	})
	eg.Go(func() error { return r.Run(egctx) })
	if err := eg.Wait(); err != nil && !errors.Is(err, context.Canceled) {
		return err
	}
	return nil
}

// runPayrollSettler consumes PAYROLL_V1 batch_locked events and writes
// the rendered CSV to object storage. Scaling is KEDA-driven on
// consumer lag — see chart/templates/scaledobject-payroll-settler.yaml.
func runPayrollSettler(ctx context.Context, logger *slog.Logger, cfg config.Config) error {
	pool, err := newRWPool(ctx, cfg)
	if err != nil {
		return fmt.Errorf("pg pool: %w", err)
	}
	defer pool.Close()
	nc, err := newNATS(ctx, cfg)
	if err != nil {
		return err
	}
	defer nc.Close()

	s3Client, err := storage.NewS3(ctx, storage.S3Config{
		Endpoint:        cfg.S3Endpoint,
		Region:          cfg.S3Region,
		AccessKeyID:     cfg.S3AccessKeyID,
		SecretAccessKey: cfg.S3SecretAccessKey,
		Bucket:          cfg.S3Bucket,
		UsePathStyle:    cfg.S3UsePathStyle,
	})
	if err != nil {
		return fmt.Errorf("s3: %w", err)
	}
	if err := s3Client.EnsureBucket(ctx); err != nil {
		return fmt.Errorf("ensure bucket: %w", err)
	}

	settler := &payrollsettler.Settler{
		JS:         nc.JS,
		Pool:       pool,
		Batches:    payrollpgrepo.NewBatchRepo(pool),
		Entries:    payrollpgrepo.NewEntryRepo(pool),
		Users:      payrollsettler.NewPgUserLookup(pool),
		Exceptions: payrollpgrepo.NewExceptionRepo(pool),
		Storage:    s3Client,
		Logger:     logger.With("component", "payroll-settler"),
		Audit:      opgrepo.NewAuditRepo(pool),
		Outbox:     opgrepo.NewOutboxRepo(pool),
	}

	eg, egctx := errgroup.WithContext(ctx)
	eg.Go(func() error {
		return serveProbes(egctx, logger,
			httpserver.PostgresChecker("postgres-rw", pool),
			httpserver.NATSChecker("nats", nc.NC),
		)
	})
	eg.Go(func() error { return settler.Run(egctx) })
	if err := eg.Wait(); err != nil && !errors.Is(err, context.Canceled) {
		return err
	}
	return nil
}

// runOnTimeEvaluator runs the SLO evaluator that opens
// satisfaction_drop and on_time_rate anomalies from ORDERS_V1 events.
func runOnTimeEvaluator(ctx context.Context, logger *slog.Logger, cfg config.Config) error {
	pool, err := newRWPool(ctx, cfg)
	if err != nil {
		return fmt.Errorf("pg pool: %w", err)
	}
	defer pool.Close()
	nc, err := newNATS(ctx, cfg)
	if err != nil {
		return err
	}
	defer nc.Close()

	eval := &evaluator.OnTimeRateEvaluator{
		JS:      nc.JS,
		Pool:    pool,
		Anomaly: cpgrepo.NewAnomalyRepo(pool),
		Logger:  logger.With("component", "on-time-evaluator"),
	}

	eg, egctx := errgroup.WithContext(ctx)
	eg.Go(func() error {
		return serveProbes(egctx, logger,
			httpserver.PostgresChecker("postgres-rw", pool),
			httpserver.NATSChecker("nats", nc.NC),
		)
	})
	eg.Go(func() error { return eval.Run(egctx) })
	if err := eg.Wait(); err != nil && !errors.Is(err, context.Canceled) {
		return err
	}
	return nil
}

// runWithLeaseSingleton wraps a per-tick runner with the K8s
// coordination.k8s.io Lease used by the scheduler split roles. Each
// scheduler role takes a separate lease name so they fail / restart
// independently.
func runWithLeaseSingleton(
	ctx context.Context,
	logger *slog.Logger,
	leaseNameEnv, leaseDefault string,
	deps []httpserver.Checker,
	run func(context.Context) error,
) error {
	leaseName := getenv(leaseNameEnv, leaseDefault)
	leaseNS := getenv("SCHEDULER_LEASE_NAMESPACE", "tbite")
	identity := getenv("POD_NAME", "local-"+uuid.NewString())

	eg, egctx := errgroup.WithContext(ctx)
	eg.Go(func() error { return serveProbes(egctx, logger, deps...) })
	eg.Go(func() error {
		return leader.RunWithLease(egctx, leader.Config{
			Namespace: leaseNS,
			LeaseName: leaseName,
			Identity:  identity,
			Logger:    logger.With("component", "leader"),
		}, func(leadCtx context.Context) error {
			err := run(leadCtx)
			if err != nil && !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
				return err
			}
			return nil
		})
	})
	if err := eg.Wait(); err != nil && !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	return nil
}

func runCutoffSweeper(ctx context.Context, logger *slog.Logger, cfg config.Config) error {
	pool, err := newRWPool(ctx, cfg)
	if err != nil {
		return fmt.Errorf("pg pool: %w", err)
	}
	defer pool.Close()

	orderRepo := opgrepo.NewOrderRepo(pool)
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
	interval := 60 * time.Second
	if v := os.Getenv("CUTOFF_INTERVAL"); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			interval = d
		}
	}
	return runWithLeaseSingleton(ctx, logger, "CUTOFF_LEASE_NAME", "tbite-cutoff-sweeper",
		[]httpserver.Checker{httpserver.PostgresChecker("postgres-rw", pool)},
		func(c context.Context) error { return cutoff.Run(c, interval) },
	)
}

func runNoShowSweeper(ctx context.Context, logger *slog.Logger, cfg config.Config) error {
	pool, err := newRWPool(ctx, cfg)
	if err != nil {
		return fmt.Errorf("pg pool: %w", err)
	}
	defer pool.Close()

	orderRepo := opgrepo.NewOrderRepo(pool)
	svc := &order.Service{
		Pool:     pool,
		Orders:   orderRepo,
		OrdersTx: orderRepo,
		StateTx:  opgrepo.NewStateEventRepo(pool),
		AuditTx:  opgrepo.NewAuditRepo(pool),
		OutboxTx: opgrepo.NewOutboxRepo(pool),
		Clock:    clock.SystemClock{},
	}
	noShow := &scheduler.NoShowSweep{
		Svc:      svc,
		Interval: 5 * time.Minute,
		MaxAge:   2 * time.Hour,
		Logger:   logger.With("component", "no-show"),
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
	return runWithLeaseSingleton(ctx, logger, "NO_SHOW_LEASE_NAME", "tbite-no-show-sweeper",
		[]httpserver.Checker{httpserver.PostgresChecker("postgres-rw", pool)},
		noShow.Run,
	)
}

func runDocExpiryScanner(ctx context.Context, logger *slog.Logger, cfg config.Config) error {
	pool, err := newRWPool(ctx, cfg)
	if err != nil {
		return fmt.Errorf("pg pool: %w", err)
	}
	defer pool.Close()

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
	return runWithLeaseSingleton(ctx, logger, "DOC_EXPIRY_LEASE_NAME", "tbite-doc-expiry-scanner",
		[]httpserver.Checker{httpserver.PostgresChecker("postgres-rw", pool)},
		docScanner.Run,
	)
}

func runFeedbackScanner(ctx context.Context, logger *slog.Logger, cfg config.Config) error {
	pool, err := newRWPool(ctx, cfg)
	if err != nil {
		return fmt.Errorf("pg pool: %w", err)
	}
	defer pool.Close()

	fs := &feedback.FeedbackScanner{
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
			fs.Interval = d
		}
	}
	if v := os.Getenv("FEEDBACK_SCAN_WINDOW"); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			fs.Window = d
		}
	}
	return runWithLeaseSingleton(ctx, logger, "FEEDBACK_LEASE_NAME", "tbite-feedback-scanner",
		[]httpserver.Checker{httpserver.PostgresChecker("postgres-rw", pool)},
		fs.Run,
	)
}

// runProvisionStreams is the one-shot role that declares JetStream
// streams and consumers. It runs as a pre-install / pre-upgrade Helm
// hook Job. Ordinary worker startup no longer mutates data-plane
// state; runOutboxRelay et al. require these streams to already exist.
func runProvisionStreams(ctx context.Context, logger *slog.Logger, cfg config.Config) error {
	if cfg.NATSURL == "" {
		return errors.New("NATS_URL is required for provision-streams role")
	}
	nc, err := messaging.New(ctx, cfg.NATSURL)
	if err != nil {
		return fmt.Errorf("nats connect: %w", err)
	}
	defer nc.Close()
	nc.StreamReplicas = cfg.NATSStreamReplicas
	if err := nc.ProvisionStreams(ctx); err != nil {
		return fmt.Errorf("provision streams: %w", err)
	}
	logger.Info("streams provisioned")
	return nil
}

// runRealtimeGateway serves only the long-lived SSE endpoints. It
// taps JetStream's ORDERS_V1 stream through RunBoardConsumer, fanning
// events out to local BoardHub / MenuHub instances, and exposes the
// SSE endpoints under their familiar paths so existing SvelteKit
// clients can be routed to this Deployment via HTTPRoute. The ordinary
// API role no longer needs to carry the long-connection load.
func runRealtimeGateway(ctx context.Context, logger *slog.Logger, cfg config.Config) error {
	pool, err := newRWPool(ctx, cfg)
	if err != nil {
		return fmt.Errorf("pg pool: %w", err)
	}
	defer pool.Close()
	rdb, err := cache.NewClient(ctx, cfg.RedisURL)
	if err != nil {
		return fmt.Errorf("redis: %w", err)
	}
	defer rdb.Close()
	nc, err := newNATS(ctx, cfg)
	if err != nil {
		return err
	}
	defer nc.Close()

	userRepo := pgrepo.NewUserRepo(pool)
	sessStore := idredis.NewSessionStore(rdb, 7*24*time.Hour)
	// Reuse the existing idhttp.API as a thin holder for the Bearer
	// auth middleware. The realtime gateway never serves identity
	// endpoints — only Sessions+Users are wired so that AuthMiddleware
	// can resolve callers without dragging the rest of the API
	// surface into this binary.
	authAPI := &idhttp.API{
		Sessions: sessStore,
		Users:    userRepo,
	}

	boardHub := order.NewBoardHub()
	menuHub := order.NewMenuHub()
	RegisterSSESubscriberGauge(boardHub, menuHub)

	r := chi.NewRouter()
	r.Use(func(next http.Handler) http.Handler {
		return otelhttp.NewHandler(next, "tbite.realtime")
	})
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Recoverer)
	// SSE requires no request-level timeout: kubelet probes target a
	// separate listener on PROBE_ADDR.

	health := httpserver.NewHealth(
		httpserver.PostgresChecker("postgres-rw", pool),
		httpserver.RedisChecker("valkey", rdb),
		httpserver.NATSChecker("nats", nc.NC),
	)
	r.Get("/healthz", health.LivenessHandler())
	r.Get("/readyz", health.ReadinessHandler())

	r.Group(func(rg chi.Router) {
		rg.Use(authAPI.AuthMiddleware)
		rg.Get("/api/merchant/orders/events", boardSSEHandler(boardHub))
		rg.Get("/api/employee/menu/events", menuSSEHandler(menuHub))
	})

	addr := cfg.HTTPAddr
	srv := &http.Server{
		Addr:              addr,
		Handler:           r,
		ReadHeaderTimeout: 5 * time.Second,
		// SSE writes are long-lived; we deliberately do not set
		// WriteTimeout. The Traefik HTTPRoute for this Service is
		// tuned to a 1h idle timeout (see chart httproute-realtime).
	}

	eg, egctx := errgroup.WithContext(ctx)
	eg.Go(func() error {
		return order.RunBoardConsumer(egctx, nc.JS, boardHub, menuHub, logger.With("component", "board-consumer"))
	})
	eg.Go(func() error {
		go func() {
			<-egctx.Done()
			health.SetLive(false)
			shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			_ = srv.Shutdown(shutdownCtx)
		}()
		logger.Info("realtime gateway listening", "addr", addr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			return err
		}
		return nil
	})
	if err := eg.Wait(); err != nil && !errors.Is(err, context.Canceled) {
		return err
	}
	return nil
}
