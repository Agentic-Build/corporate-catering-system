package postgres_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	pgrepo "github.com/takalawang/corporate-catering-system/services/api/internal/dlq/postgres"
)

// int64GaugePoints returns the int64 gauge data points for the named instrument,
// failing the test if the instrument is missing or is not an Int64 gauge.
func int64GaugePoints(t *testing.T, rm *metricdata.ResourceMetrics, name string) []metricdata.DataPoint[int64] {
	t.Helper()
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name != name {
				continue
			}
			g, ok := m.Data.(metricdata.Gauge[int64])
			require.True(t, ok, "metric %s is not an Int64 gauge (got %T)", name, m.Data)
			return g.DataPoints
		}
	}
	t.Fatalf("metric %s not found in collected output", name)
	return nil
}

// float64GaugePoints returns the float64 gauge data points for the named
// instrument, failing the test if the instrument is missing or wrong type.
func float64GaugePoints(t *testing.T, rm *metricdata.ResourceMetrics, name string) []metricdata.DataPoint[float64] {
	t.Helper()
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name != name {
				continue
			}
			g, ok := m.Data.(metricdata.Gauge[float64])
			require.True(t, ok, "metric %s is not a Float64 gauge (got %T)", name, m.Data)
			return g.DataPoints
		}
	}
	t.Fatalf("metric %s not found in collected output", name)
	return nil
}

func int64AttrStr(t *testing.T, dp metricdata.DataPoint[int64], key string) string {
	t.Helper()
	v, ok := dp.Attributes.Value(attribute.Key(key))
	require.True(t, ok, "attribute %q missing on data point", key)
	return v.AsString()
}

func TestRegisterDLQGauges(t *testing.T) {
	pool, cleanup := setupPostgres(t)
	defer cleanup()
	ctx := context.Background()
	repo := pgrepo.NewDLQRepo(pool)
	admin := seedAdminUser(t, pool)

	// Two pending rows on ORDERS_V1, one on PAYROLL_V1.
	pendingA := newMessage()
	require.NoError(t, repo.Write(ctx, pendingA))
	pendingB := newMessage()
	require.NoError(t, repo.Write(ctx, pendingB))

	pendingPayroll := newMessage()
	pendingPayroll.SourceStream = "PAYROLL_V1"
	require.NoError(t, repo.Write(ctx, pendingPayroll))

	// A resolved row must NOT be counted as pending.
	resolved := newMessage()
	require.NoError(t, repo.Write(ctx, resolved))
	require.NoError(t, repo.MarkResolved(ctx, resolved.ID, admin, "garbage"))

	// Wire a ManualReader MeterProvider as the global provider BEFORE registering
	// (RegisterDLQGauges reads otel.GetMeterProvider).
	reader := sdkmetric.NewManualReader()
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	otel.SetMeterProvider(mp)
	t.Cleanup(func() { _ = mp.Shutdown(context.Background()) })

	require.NoError(t, pgrepo.RegisterDLQGauges(pool))

	var rm metricdata.ResourceMetrics
	require.NoError(t, reader.Collect(ctx, &rm))

	// tbite_dlq_pending — count per source_stream, excluding the resolved row.
	pendingPts := int64GaugePoints(t, &rm, "tbite_dlq_pending")
	byStream := map[string]int64{}
	for _, dp := range pendingPts {
		byStream[int64AttrStr(t, dp, "source_stream")] = dp.Value
	}
	assert.Equal(t, int64(2), byStream["ORDERS_V1"], "ORDERS_V1 pending should exclude the resolved row")
	assert.Equal(t, int64(1), byStream["PAYROLL_V1"])

	// tbite_dlq_oldest_seconds — single point, no attributes, positive age.
	oldestPts := float64GaugePoints(t, &rm, "tbite_dlq_oldest_seconds")
	require.Len(t, oldestPts, 1)
	assert.Equal(t, 0, oldestPts[0].Attributes.Len(), "oldest gauge must have no attributes")
	assert.Greater(t, oldestPts[0].Value, 0.0)
}
