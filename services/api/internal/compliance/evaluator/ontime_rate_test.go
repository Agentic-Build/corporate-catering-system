package evaluator_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"path/filepath"
	"runtime"
	"sync/atomic"
	"testing"
	"time"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	tcnats "github.com/testcontainers/testcontainers-go/modules/nats"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/takalawang/corporate-catering-system/services/api/internal/compliance"
	"github.com/takalawang/corporate-catering-system/services/api/internal/compliance/evaluator"
	cpgrepo "github.com/takalawang/corporate-catering-system/services/api/internal/compliance/postgres"
	"github.com/takalawang/corporate-catering-system/services/api/internal/platform/messaging"
)

func setupPostgres(t *testing.T) (*pgxpool.Pool, func()) {
	t.Helper()
	ctx := context.Background()
	container, err := tcpostgres.Run(ctx,
		"postgres:16-alpine",
		tcpostgres.WithDatabase("tbite"),
		tcpostgres.WithUsername("tbite"),
		tcpostgres.WithPassword("tbite"),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).
				WithStartupTimeout(60*time.Second)),
	)
	require.NoError(t, err)
	dsn, err := container.ConnectionString(ctx, "sslmode=disable")
	require.NoError(t, err)

	_, thisFile, _, _ := runtime.Caller(0)
	// services/api/internal/compliance/evaluator/ontime_rate_test.go
	//   → ../../../../../migrations
	migrationsDir := filepath.Join(filepath.Dir(thisFile), "..", "..", "..", "..", "..", "migrations")
	m, err := migrate.New("file://"+migrationsDir, dsn)
	require.NoError(t, err)
	require.NoError(t, m.Up())

	cfg, err := pgxpool.ParseConfig(dsn)
	require.NoError(t, err)
	cfg.MaxConns = 10
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	require.NoError(t, err)

	return pool, func() {
		pool.Close()
		_ = container.Terminate(ctx)
	}
}

func setupNATS(t *testing.T) (string, func()) {
	t.Helper()
	ctx := context.Background()
	c, err := tcnats.Run(ctx, "nats:2.10-alpine")
	require.NoError(t, err)
	url, err := c.ConnectionString(ctx)
	require.NoError(t, err)
	return url, func() { _ = c.Terminate(ctx) }
}

var vendorSeq atomic.Uint64

func seedVendor(t *testing.T, pool *pgxpool.Pool) string {
	t.Helper()
	n := vendorSeq.Add(1)
	var id string
	err := pool.QueryRow(context.Background(), `
INSERT INTO vendor (display_name, legal_name, contact_email, status)
VALUES ($1, $2, $3, 'approved')
RETURNING id`,
		fmt.Sprintf("eval-vendor-%d", n),
		fmt.Sprintf("eval-vendor-%d Ltd", n),
		fmt.Sprintf("eval-vendor-%d@test.com", n),
	).Scan(&id)
	require.NoError(t, err)
	return id
}

func TestOnTimeRateEvaluator_OpensHighAnomalyOnDrop(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test")
	}
	pool, pgCleanup := setupPostgres(t)
	defer pgCleanup()
	natsURL, natsCleanup := setupNATS(t)
	defer natsCleanup()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	nc, err := messaging.New(ctx, natsURL)
	require.NoError(t, err)
	defer nc.Close()
	require.NoError(t, nc.ProvisionStreams(ctx))

	vendorID := seedVendor(t, pool)
	anomRepo := cpgrepo.NewAnomalyRepo(pool)

	ev := &evaluator.OnTimeRateEvaluator{
		JS:      nc.JS,
		Anomaly: anomRepo,
		Logger:  slog.New(slog.NewTextHandler(io.Discard, nil)),
	}

	runCtx, runCancel := context.WithCancel(ctx)
	defer runCancel()
	runErr := make(chan error, 1)
	go func() { runErr <- ev.Run(runCtx) }()

	publish := func(subject string) {
		payload, _ := json.Marshal(map[string]any{"vendor_id": vendorID})
		_, err := nc.JS.Publish(ctx, subject, payload)
		require.NoError(t, err)
	}

	// 12 events for vendor: 8 picked_up + 4 no_show → rate = 8/12 ≈ 0.67.
	// 0.67 < HighThresh (0.90) → severity=high. Total ≥ MinSamples (10).
	for i := 0; i < 8; i++ {
		publish("order.picked_up.v1")
	}
	for i := 0; i < 4; i++ {
		publish("order.no_show.v1")
	}

	// Poll up to 5s for the anomaly to appear.
	deadline := time.Now().Add(5 * time.Second)
	var found *compliance.Anomaly
	for time.Now().Before(deadline) {
		anomalies, err := anomRepo.List(ctx,
			[]compliance.AnomalyStatus{compliance.AnomalyOpen}, nil)
		require.NoError(t, err)
		for _, a := range anomalies {
			if a.Kind == "on_time_rate_drop" && a.TargetID == vendorID {
				found = a
				break
			}
		}
		if found != nil {
			break
		}
		time.Sleep(150 * time.Millisecond)
	}
	require.NotNil(t, found, "expected on_time_rate_drop anomaly for vendor")
	assert.Equal(t, compliance.SeverityHigh, found.Severity)
	assert.Equal(t, "vendor", found.TargetKind)
	require.NotNil(t, found.Payload)
	assert.InDelta(t, 8.0/12.0, found.Payload["rate"], 0.001)
	// JSON unmarshals numbers as float64.
	assert.Equal(t, float64(12), found.Payload["total"])
	assert.Equal(t, float64(8), found.Payload["picked_up"])

	runCancel()
	select {
	case <-runErr:
	case <-time.After(5 * time.Second):
		t.Fatal("evaluator did not stop within 5s of ctx cancel")
	}
}
