package messaging_test

import (
	"context"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

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
	if err != nil {
		_ = container.Terminate(ctx)
		t.Fatalf("conn string: %v", err)
	}
	_, thisFile, _, _ := runtime.Caller(0)
	// services/api/internal/platform/messaging/dlq_test.go → ../../../../../migrations
	migrationsDir := filepath.Join(filepath.Dir(thisFile), "..", "..", "..", "..", "..", "migrations")
	m, err := migrate.New("file://"+migrationsDir, dsn)
	if err != nil {
		_ = container.Terminate(ctx)
		t.Fatalf("migrate new: %v", err)
	}
	if err := m.Up(); err != nil {
		_ = container.Terminate(ctx)
		t.Fatalf("migrate up: %v", err)
	}
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		_ = container.Terminate(ctx)
		t.Fatalf("pool: %v", err)
	}
	return pool, func() {
		pool.Close()
		_ = container.Terminate(ctx)
	}
}

func TestWriteDLQ_IncrementsCounter(t *testing.T) {
	pool, cleanup := setupPostgres(t)
	defer cleanup()
	ctx := context.Background()

	// Install a ManualReader MeterProvider as the global provider BEFORE the
	// first WriteDLQ so the lazily-bound counter binds to it.
	reader := sdkmetric.NewManualReader()
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	otel.SetMeterProvider(mp)
	t.Cleanup(func() { _ = mp.Shutdown(context.Background()) })

	require.NoError(t, messaging.WriteDLQ(ctx, pool,
		"ORDERS_V1", "order.placed.v1", "order-projector",
		map[string]any{"order_id": "o-1"}, nil, "boom"))

	var rm metricdata.ResourceMetrics
	require.NoError(t, reader.Collect(ctx, &rm))

	var sum *metricdata.Sum[int64]
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name == "tbite_dlq_messages_total" {
				s, ok := m.Data.(metricdata.Sum[int64])
				require.True(t, ok, "tbite_dlq_messages_total is not an Int64 sum (got %T)", m.Data)
				sum = &s
			}
		}
	}
	require.NotNil(t, sum, "tbite_dlq_messages_total not found in collected output")
	require.Len(t, sum.DataPoints, 1)
	assert.Equal(t, int64(1), sum.DataPoints[0].Value)
	v, ok := sum.DataPoints[0].Attributes.Value(attribute.Key("source_stream"))
	require.True(t, ok, "source_stream attribute missing")
	assert.Equal(t, "ORDERS_V1", v.AsString())
}
