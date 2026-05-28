package db_test

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/Agentic-Build/corporate-catering-system/services/api/internal/platform/db"
)

// TestRegisterPoolMetrics verifies the four db pool gauges are emitted
// under the exact names vmalert + dashboards query, with a role label
// distinguishing rw vs ro pools.
func TestRegisterPoolMetrics(t *testing.T) {
	reader := sdkmetric.NewManualReader()
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	otel.SetMeterProvider(mp)
	t.Cleanup(func() { _ = mp.Shutdown(context.Background()) })

	cfg, err := pgxpool.ParseConfig("postgres://user:pw@127.0.0.1:1/tbite?sslmode=disable")
	require.NoError(t, err)
	cfg.MaxConns = 7
	// pgxpool.NewWithConfig does not dial until first acquire; Stat() works
	// without a live backend and returns the configured MaxConns.
	pool, err := pgxpool.NewWithConfig(context.Background(), cfg)
	require.NoError(t, err)
	t.Cleanup(pool.Close)

	require.NoError(t, db.RegisterPoolMetrics(pool, "rw"))

	var rm metricdata.ResourceMetrics
	require.NoError(t, reader.Collect(context.Background(), &rm))

	names := map[string]bool{}
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			names[m.Name] = true
		}
	}
	for _, want := range []string{
		"tbite_db_pool_acquired_connections",
		"tbite_db_pool_idle_connections",
		"tbite_db_pool_total_connections",
		"tbite_db_pool_max_connections",
	} {
		assert.Truef(t, names[want], "expected metric %q to be emitted", want)
	}

	max := gaugePointForRole(t, &rm, "tbite_db_pool_max_connections", "rw")
	assert.Equal(t, int64(7), max, "max gauge should mirror pgxpool MaxConns")
}

func gaugePointForRole(t *testing.T, rm *metricdata.ResourceMetrics, name, role string) int64 {
	t.Helper()
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name != name {
				continue
			}
			g, ok := m.Data.(metricdata.Gauge[int64])
			require.True(t, ok, "metric %s is not Int64 gauge (got %T)", name, m.Data)
			for _, dp := range g.DataPoints {
				v, ok := dp.Attributes.Value(attribute.Key("role"))
				if ok && v.AsString() == role {
					return dp.Value
				}
			}
		}
	}
	t.Fatalf("metric %s with role=%s not found", name, role)
	return 0
}
