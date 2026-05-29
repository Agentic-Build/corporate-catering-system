package observability

import (
	"context"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

func resetMetricsForTest() {
	metricsOnce = sync.Once{}
	metrics = nil
}

func int64GaugePoints(t *testing.T, rm *metricdata.ResourceMetrics, name string) []metricdata.DataPoint[int64] {
	t.Helper()
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name != name {
				continue
			}
			gauge, ok := m.Data.(metricdata.Gauge[int64])
			require.True(t, ok, "metric %s is not an Int64 gauge (got %T)", name, m.Data)
			return gauge.DataPoints
		}
	}
	t.Fatalf("metric %s not found in collected output", name)
	return nil
}

func int64AttrStr(t *testing.T, dp metricdata.DataPoint[int64], key string) string {
	t.Helper()
	value, ok := dp.Attributes.Value(attribute.Key(key))
	require.True(t, ok, "attribute %q missing on data point", key)
	return value.AsString()
}

func TestRecordDependencyReady(t *testing.T) {
	ctx := context.Background()
	t.Setenv("HOSTNAME", "tbite-tbite-platform-api-0")
	t.Setenv("TBITE_ROLE", "api")

	reader := sdkmetric.NewManualReader()
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	previous := otel.GetMeterProvider()
	otel.SetMeterProvider(mp)
	resetMetricsForTest()
	t.Cleanup(func() {
		resetMetricsForTest()
		otel.SetMeterProvider(previous)
		_ = mp.Shutdown(context.Background())
	})

	MustInitMetrics()
	RecordDependencyReady(ctx, "object-storage", false)

	var failed metricdata.ResourceMetrics
	require.NoError(t, reader.Collect(ctx, &failed))
	failedPoints := int64GaugePoints(t, &failed, "tbite_dependency_ready")
	require.Len(t, failedPoints, 1)
	assert.Equal(t, int64(0), failedPoints[0].Value)
	assert.Equal(t, "object-storage", int64AttrStr(t, failedPoints[0], "dependency"))
	assert.Equal(t, "tbite-tbite-platform-api-0", int64AttrStr(t, failedPoints[0], "pod"))
	assert.Equal(t, "api", int64AttrStr(t, failedPoints[0], "role"))

	RecordDependencyReady(ctx, "object-storage", true)

	var recovered metricdata.ResourceMetrics
	require.NoError(t, reader.Collect(ctx, &recovered))
	recoveredPoints := int64GaugePoints(t, &recovered, "tbite_dependency_ready")
	require.Len(t, recoveredPoints, 1)
	assert.Equal(t, int64(1), recoveredPoints[0].Value)
	assert.Equal(t, "object-storage", int64AttrStr(t, recoveredPoints[0], "dependency"))
	assert.Equal(t, "tbite-tbite-platform-api-0", int64AttrStr(t, recoveredPoints[0], "pod"))
	assert.Equal(t, "api", int64AttrStr(t, recoveredPoints[0], "role"))
}
