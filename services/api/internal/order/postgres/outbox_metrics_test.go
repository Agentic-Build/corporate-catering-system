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

	"github.com/Agentic-Build/corporate-catering-system/services/api/internal/order/postgres"
)

// gaugePoints returns the int64 gauge data points for the named instrument,
// failing if it is missing or not an Int64 gauge.
func gaugePoints(t *testing.T, rm *metricdata.ResourceMetrics, name string) []metricdata.DataPoint[int64] {
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

// attrStr extracts a string attribute value from a data point, failing if absent.
func attrStr(t *testing.T, dp metricdata.DataPoint[int64], key string) string {
	t.Helper()
	v, ok := dp.Attributes.Value(attribute.Key(key))
	require.True(t, ok, "attribute %q missing on data point", key)
	return v.AsString()
}

// float64GaugePoints returns the float64 gauge data points for the named
// instrument, failing if it is missing or not a Float64 gauge.
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

func TestRegisterOutboxGauges(t *testing.T) {
	pool, cleanup := setupPostgres(t)
	defer cleanup()
	ctx := context.Background()

	// Seed unpublished rows: 2 'order', 1 'menu'. created_at is backdated so
	// the oldest-age gauges observe a strictly positive value.
	_, err := pool.Exec(ctx, `
INSERT INTO outbox_event (aggregate_type, aggregate_id, subject, payload, headers, created_at)
VALUES
  ('order', gen_random_uuid(), 'order.placed.v1',  '{}'::jsonb, '{}'::jsonb, now() - interval '90 seconds'),
  ('order', gen_random_uuid(), 'order.placed.v1',  '{}'::jsonb, '{}'::jsonb, now() - interval '30 seconds'),
  ('menu',  gen_random_uuid(), 'menu.published.v1','{}'::jsonb, '{}'::jsonb, now() - interval '10 seconds')`)
	require.NoError(t, err)

	// Install a ManualReader MeterProvider as the global provider BEFORE
	// registering the gauges (RegisterOutboxGauges reads otel.GetMeterProvider).
	reader := sdkmetric.NewManualReader()
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	otel.SetMeterProvider(mp)
	t.Cleanup(func() { _ = mp.Shutdown(context.Background()) })

	require.NoError(t, postgres.RegisterOutboxGauges(pool))

	var rm metricdata.ResourceMetrics
	require.NoError(t, reader.Collect(ctx, &rm))

	// tbite_outbox_pending — one point per aggregate_type.
	pendingByType := map[string]int64{}
	for _, dp := range gaugePoints(t, &rm, "tbite_outbox_pending") {
		pendingByType[attrStr(t, dp, "aggregate_type")] = dp.Value
	}
	assert.Equal(t, int64(2), pendingByType["order"])
	assert.Equal(t, int64(1), pendingByType["menu"])

	// Both oldest gauges carry the same strictly-positive value, no attrs.
	oldestPts := float64GaugePoints(t, &rm, "tbite_outbox_oldest_seconds")
	require.Len(t, oldestPts, 1)
	assert.Greater(t, oldestPts[0].Value, 0.0)

	oldestUnpubPts := float64GaugePoints(t, &rm, "tbite_outbox_oldest_unpublished_seconds")
	require.Len(t, oldestUnpubPts, 1)
	assert.Greater(t, oldestUnpubPts[0].Value, 0.0)

	assert.Equal(t, oldestPts[0].Value, oldestUnpubPts[0].Value, "both oldest gauges must report the same value")
}
