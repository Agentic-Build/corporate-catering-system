package observability

import (
	"context"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

// withCollectibleMetrics wires a manual-reader MeterProvider, resets the
// package singletons, initialises the instruments, and returns a collect()
// helper. Everything is restored on test cleanup.
func withCollectibleMetrics(t *testing.T) func(context.Context) metricdata.ResourceMetrics {
	t.Helper()
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
	return func(ctx context.Context) metricdata.ResourceMetrics {
		var rm metricdata.ResourceMetrics
		require.NoError(t, reader.Collect(ctx, &rm))
		return rm
	}
}

func findMetric(t *testing.T, rm metricdata.ResourceMetrics, name string) metricdata.Metrics {
	t.Helper()
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name == name {
				return m
			}
		}
	}
	t.Fatalf("metric %s not collected", name)
	return metricdata.Metrics{}
}

func sumInt64(t *testing.T, rm metricdata.ResourceMetrics, name string) []metricdata.DataPoint[int64] {
	t.Helper()
	m := findMetric(t, rm, name)
	s, ok := m.Data.(metricdata.Sum[int64])
	require.True(t, ok, "metric %s is not Int64 sum (got %T)", name, m.Data)
	return s.DataPoints
}

func histInt64(t *testing.T, rm metricdata.ResourceMetrics, name string) []metricdata.HistogramDataPoint[int64] {
	t.Helper()
	m := findMetric(t, rm, name)
	h, ok := m.Data.(metricdata.Histogram[int64])
	require.True(t, ok, "metric %s is not Int64 histogram (got %T)", name, m.Data)
	return h.DataPoints
}

func histFloat64(t *testing.T, rm metricdata.ResourceMetrics, name string) []metricdata.HistogramDataPoint[float64] {
	t.Helper()
	m := findMetric(t, rm, name)
	h, ok := m.Data.(metricdata.Histogram[float64])
	require.True(t, ok, "metric %s is not Float64 histogram (got %T)", name, m.Data)
	return h.DataPoints
}

func attrStr[N int64 | float64](t *testing.T, dp metricdata.DataPoint[N], key string) string {
	t.Helper()
	v, ok := dp.Attributes.Value(attribute.Key(key))
	require.True(t, ok, "attribute %q missing", key)
	return v.AsString()
}

func histAttrStr[N int64 | float64](t *testing.T, dp metricdata.HistogramDataPoint[N], key string) string {
	t.Helper()
	v, ok := dp.Attributes.Value(attribute.Key(key))
	require.True(t, ok, "attribute %q missing", key)
	return v.AsString()
}

// TestEmitHelpers_NilMetricsAreNoOps exercises every nil-guard early return so
// callers stay safe before MustInitMetrics runs. metrics must be nil here.
func TestEmitHelpers_NilMetricsAreNoOps(t *testing.T) {
	resetMetricsForTest()
	t.Cleanup(resetMetricsForTest)
	require.Nil(t, metrics)

	ctx := context.Background()
	// None of these should panic with a nil singleton.
	RecordDependencyReady(ctx, "db", true)
	RecordOrderPlaced(ctx, "p", "v", "lunch", "ok")
	RecordOrderCancelled(ctx, "p", "v", "r", "admin")
	RecordOrderModified(ctx, "p", "v")
	RecordOrderReady(ctx, "v", 3)
	RecordPickupVerified(ctx, "p", "v", "success")
	RecordOrderNoShow(ctx, 2)
	RecordOrderPlaceLatency(ctx, 0.1, "p", "lunch", "ok")
	RecordOrderPrice(ctx, 100, "p", "v")
	RecordQuotaExhausted(ctx, "p", "v", "lunch", "m")
	RecordSupplyAdjusted(ctx, "v", "up", 5)
	RecordSettlementRun(ctx, "v", "ok", 1.5, 1000)
	RecordPayrollEntry(ctx, "2026-05", 5000)
	RecordPayrollDispute(ctx, "open")
	RecordPayrollReversal(ctx, "error")
	RecordComplianceViolation(ctx, "R1", "high", "v")
	RecordComplianceDocExpiring(ctx, "v", 3)
	MCPToolCall(ctx, "tool", "cli", "success", "read", 0.2, nil)
	RecordMCPAuthFailure(ctx, "bad_token")
}

func TestRecordDependencyReady_NoPodNoRole(t *testing.T) {
	t.Setenv("HOSTNAME", "")
	t.Setenv("TBITE_ROLE", "")
	collect := withCollectibleMetrics(t)
	ctx := context.Background()

	RecordDependencyReady(ctx, "queue", true)
	rm := collect(ctx)
	pts := int64GaugePoints(t, &rm, "tbite_dependency_ready")
	require.Len(t, pts, 1)
	assert.Equal(t, int64(1), pts[0].Value)
	assert.Equal(t, "queue", int64AttrStr(t, pts[0], "dependency"))
	_, hasPod := pts[0].Attributes.Value(attribute.Key("pod"))
	assert.False(t, hasPod, "pod attr should be absent when HOSTNAME empty")
	_, hasRole := pts[0].Attributes.Value(attribute.Key("role"))
	assert.False(t, hasRole, "role attr should be absent when TBITE_ROLE empty")
}

func TestRecordOrderCounters(t *testing.T) {
	collect := withCollectibleMetrics(t)
	ctx := context.Background()

	RecordOrderPlaced(ctx, "plant-1", "vendor-1", "lunch", "success")
	RecordOrderCancelled(ctx, "plant-1", "vendor-1", "user", "employee")
	RecordOrderModified(ctx, "plant-1", "vendor-1")
	RecordPickupVerified(ctx, "plant-1", "vendor-1", "success")

	rm := collect(ctx)

	placed := sumInt64(t, rm, "catering.order.placed.count")
	require.Len(t, placed, 1)
	assert.Equal(t, int64(1), placed[0].Value)
	assert.Equal(t, "plant-1", attrStr(t, placed[0], "plant_id"))
	assert.Equal(t, "lunch", attrStr(t, placed[0], "meal_window"))
	assert.Equal(t, "success", attrStr(t, placed[0], "outcome"))

	cancelled := sumInt64(t, rm, "catering.order.cancelled.count")
	require.Len(t, cancelled, 1)
	assert.Equal(t, "user", attrStr(t, cancelled[0], "reason"))
	assert.Equal(t, "employee", attrStr(t, cancelled[0], "actor_role"))

	modified := sumInt64(t, rm, "catering.order.modified.count")
	require.Len(t, modified, 1)
	assert.Equal(t, int64(1), modified[0].Value)

	verified := sumInt64(t, rm, "catering.order.pickup_verified.count")
	require.Len(t, verified, 1)
	assert.Equal(t, "totp", attrStr(t, verified[0], "method"))
	assert.Equal(t, "success", attrStr(t, verified[0], "outcome"))
}

func TestRecordOrderReady_PositiveAndNonPositive(t *testing.T) {
	collect := withCollectibleMetrics(t)
	ctx := context.Background()

	RecordOrderReady(ctx, "vendor-9", 0)  // skipped: count <= 0
	RecordOrderReady(ctx, "vendor-9", -1) // skipped
	RecordOrderReady(ctx, "vendor-9", 4)  // recorded

	rm := collect(ctx)
	pts := sumInt64(t, rm, "catering.order.ready.count")
	require.Len(t, pts, 1)
	assert.Equal(t, int64(4), pts[0].Value)
	assert.Equal(t, "vendor-9", attrStr(t, pts[0], "vendor_id"))
}

func TestRecordOrderNoShow_PositiveAndNonPositive(t *testing.T) {
	collect := withCollectibleMetrics(t)
	ctx := context.Background()

	RecordOrderNoShow(ctx, 0)  // skipped
	RecordOrderNoShow(ctx, -3) // skipped
	RecordOrderNoShow(ctx, 7)  // recorded

	rm := collect(ctx)
	pts := sumInt64(t, rm, "catering.order.no_show.count")
	require.Len(t, pts, 1)
	assert.Equal(t, int64(7), pts[0].Value)
}

func TestRecordOrderPlaceLatencyAndPrice(t *testing.T) {
	collect := withCollectibleMetrics(t)
	ctx := context.Background()

	RecordOrderPlaceLatency(ctx, 0.42, "plant-1", "dinner", "success")
	RecordOrderPrice(ctx, 0, "plant-1", "vendor-1")   // skipped: minor <= 0
	RecordOrderPrice(ctx, -5, "plant-1", "vendor-1")  // skipped
	RecordOrderPrice(ctx, 250, "plant-1", "vendor-1") // recorded

	rm := collect(ctx)

	lat := histFloat64(t, rm, "catering.order.place.duration")
	require.Len(t, lat, 1)
	assert.Equal(t, uint64(1), lat[0].Count)
	assert.Equal(t, "dinner", histAttrStr(t, lat[0], "meal_window"))

	price := histInt64(t, rm, "catering.order.price_minor")
	require.Len(t, price, 1)
	assert.Equal(t, uint64(1), price[0].Count)
	assert.Equal(t, int64(250), price[0].Sum)
	assert.Equal(t, "vendor-1", histAttrStr(t, price[0], "vendor_id"))
}

func TestRecordQuotaExhausted(t *testing.T) {
	collect := withCollectibleMetrics(t)
	ctx := context.Background()

	RecordQuotaExhausted(ctx, "plant-1", "vendor-1", "lunch", "menu-7")

	rm := collect(ctx)
	pts := sumInt64(t, rm, "catering.quota.exhausted.count")
	require.Len(t, pts, 1)
	assert.Equal(t, "menu-7", attrStr(t, pts[0], "menu_item_id"))
}

func TestRecordSupplyAdjusted_WithAndWithoutQty(t *testing.T) {
	collect := withCollectibleMetrics(t)
	ctx := context.Background()

	RecordSupplyAdjusted(ctx, "vendor-1", "up", 0)  // count recorded, qty skipped
	RecordSupplyAdjusted(ctx, "vendor-2", "down", 6) // both recorded

	rm := collect(ctx)

	cnt := sumInt64(t, rm, "catering.supply.adjusted.count")
	require.Len(t, cnt, 2)

	qty := histInt64(t, rm, "catering.supply.adjusted.qty")
	require.Len(t, qty, 1) // only the deltaAbs>0 call
	assert.Equal(t, int64(6), qty[0].Sum)
	assert.Equal(t, "down", histAttrStr(t, qty[0], "direction"))
}

func TestRecordSettlementRun_WithAndWithoutAmount(t *testing.T) {
	collect := withCollectibleMetrics(t)
	ctx := context.Background()

	RecordSettlementRun(ctx, "vendor-1", "ok", 2.0, 0)     // amount skipped
	RecordSettlementRun(ctx, "vendor-1", "ok", 3.0, 90000) // amount recorded

	rm := collect(ctx)

	cnt := sumInt64(t, rm, "catering.settlement.run.count")
	require.Len(t, cnt, 1)
	assert.Equal(t, int64(2), cnt[0].Value)

	dur := histFloat64(t, rm, "catering.settlement.run.duration")
	require.Len(t, dur, 1)
	assert.Equal(t, uint64(2), dur[0].Count)

	amt := histInt64(t, rm, "catering.settlement.amount_minor")
	require.Len(t, amt, 1)
	assert.Equal(t, int64(90000), amt[0].Sum)
}

func TestRecordPayrollAndReversalAndDispute(t *testing.T) {
	collect := withCollectibleMetrics(t)
	ctx := context.Background()

	RecordPayrollEntry(ctx, "2026-05", 12345)
	RecordPayrollDispute(ctx, "open")
	RecordPayrollReversal(ctx, "duplicate")

	rm := collect(ctx)

	entry := histInt64(t, rm, "catering.payroll.entry.amount_minor")
	require.Len(t, entry, 1)
	assert.Equal(t, int64(12345), entry[0].Sum)
	assert.Equal(t, "2026-05", histAttrStr(t, entry[0], "period"))

	dispute := sumInt64(t, rm, "catering.payroll.dispute.count")
	require.Len(t, dispute, 1)
	assert.Equal(t, "open", attrStr(t, dispute[0], "action"))

	reversal := sumInt64(t, rm, "catering.payroll.reversal.count")
	require.Len(t, reversal, 1)
	assert.Equal(t, "duplicate", attrStr(t, reversal[0], "reason"))
}

func TestRecordComplianceViolation(t *testing.T) {
	collect := withCollectibleMetrics(t)
	ctx := context.Background()

	RecordComplianceViolation(ctx, "R-42", "critical", "vendor-3")

	rm := collect(ctx)
	pts := sumInt64(t, rm, "catering.compliance.violation.count")
	require.Len(t, pts, 1)
	assert.Equal(t, "R-42", attrStr(t, pts[0], "rule_id"))
	assert.Equal(t, "critical", attrStr(t, pts[0], "severity"))
}

func TestRecordComplianceDocExpiring_AllBuckets(t *testing.T) {
	collect := withCollectibleMetrics(t)
	ctx := context.Background()

	cases := map[int]string{
		3:   "<=7",
		10:  "<=14",
		20:  "<=30",
		45:  "<=60",
		120: "60+",
	}
	for days := range cases {
		RecordComplianceDocExpiring(ctx, "vendor-1", days)
	}

	rm := collect(ctx)
	pts := sumInt64(t, rm, "catering.compliance.doc_expiring.count")
	require.Len(t, pts, len(cases))

	got := map[string]bool{}
	for _, p := range pts {
		got[attrStr(t, p, "expiry_bucket")] = true
	}
	for _, want := range cases {
		assert.True(t, got[want], "expected expiry_bucket %q", want)
	}
}

func TestMCPToolCall_SuccessNoLog(t *testing.T) {
	collect := withCollectibleMetrics(t)
	ctx := context.Background()

	MCPToolCall(ctx, "list_orders", "client-a", "success", "read", 0.05, nil)

	rm := collect(ctx)

	inv := sumInt64(t, rm, "mcp.tool.invocation.count")
	require.Len(t, inv, 1)
	assert.Equal(t, "list_orders", attrStr(t, inv[0], "tool_name"))
	assert.Equal(t, "client-a", attrStr(t, inv[0], "client_id"))

	dur := histFloat64(t, rm, "mcp.tool.duration")
	require.Len(t, dur, 1)

	side := sumInt64(t, rm, "mcp.tool.side_effects.count")
	require.Len(t, side, 1)
	assert.Equal(t, "read", attrStr(t, side[0], "annotation"))
}

func TestMCPToolCall_NonSuccessLogsDebug(t *testing.T) {
	collect := withCollectibleMetrics(t)
	ctx := context.Background()

	rec := &countingHandler{}
	log := slog.New(rec)
	MCPToolCall(ctx, "delete_order", "client-b", "error", "write", 0.9, log)

	rm := collect(ctx)
	inv := sumInt64(t, rm, "mcp.tool.invocation.count")
	require.Len(t, inv, 1)
	assert.Equal(t, "error", attrStr(t, inv[0], "outcome"))
	// The non-success branch must have emitted a debug log line.
	assert.Equal(t, 1, rec.records)
}

func TestRecordMCPAuthFailure(t *testing.T) {
	collect := withCollectibleMetrics(t)
	ctx := context.Background()

	RecordMCPAuthFailure(ctx, "expired_token")

	rm := collect(ctx)
	pts := sumInt64(t, rm, "mcp.auth.failure.count")
	require.Len(t, pts, 1)
	assert.Equal(t, "expired_token", attrStr(t, pts[0], "reason"))
}

// countingHandler is a minimal slog.Handler that counts emitted records and
// always reports Debug as enabled so the non-success log branch fires.
type countingHandler struct{ records int }

func (h *countingHandler) Enabled(context.Context, slog.Level) bool { return true }
func (h *countingHandler) Handle(_ context.Context, _ slog.Record) error {
	h.records++
	return nil
}
func (h *countingHandler) WithAttrs([]slog.Attr) slog.Handler { return h }
func (h *countingHandler) WithGroup(string) slog.Handler      { return h }
