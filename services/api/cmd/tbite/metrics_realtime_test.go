package main

import (
	"context"
	"testing"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/Agentic-Build/corporate-catering-system/services/api/internal/order"
)

// newReader installs a MeterProvider backed by a ManualReader as the
// global provider. realtimeMetrics() caches its instruments via
// sync.Once, so the provider MUST be set before the first call.
func newReader(t *testing.T) *sdkmetric.ManualReader {
	t.Helper()
	reader := sdkmetric.NewManualReader()
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	otel.SetMeterProvider(mp)
	return reader
}

// findMetric returns the data points of the named metric, or fails.
func collectMetric(t *testing.T, reader *sdkmetric.ManualReader, name string) metricdata.Aggregation {
	t.Helper()
	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("collect: %v", err)
	}
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name == name {
				return m.Data
			}
		}
	}
	t.Fatalf("metric %q not found in collected output", name)
	return nil
}

func TestSSEOnDisconnect_RecordsDisconnectCounter(t *testing.T) {
	reader := newReader(t)

	ctx := context.Background()
	sseOnConnect(ctx, "board")
	sseOnDisconnect(ctx, "board", "write_error")

	data := collectMetric(t, reader, "tbite_sse_disconnects_total")
	sum, ok := data.(metricdata.Sum[int64])
	if !ok {
		t.Fatalf("expected Sum[int64], got %T", data)
	}
	var found bool
	for _, dp := range sum.DataPoints {
		surface, _ := dp.Attributes.Value(attribute.Key("surface"))
		reason, _ := dp.Attributes.Value(attribute.Key("reason"))
		if surface.AsString() == "board" && reason.AsString() == "write_error" {
			found = true
			if dp.Value != 1 {
				t.Fatalf("disconnects value = %d, want 1", dp.Value)
			}
		}
	}
	if !found {
		t.Fatalf("no disconnect data point with surface=board reason=write_error: %+v", sum.DataPoints)
	}
}

func TestRegisterSSESubscriberGauge_ObservesHubCounts(t *testing.T) {
	reader := newReader(t)

	boardHub := order.NewBoardHub()
	menuHub := order.NewMenuHub()

	// 2 board subscribers (across two vendors) and 1 menu subscriber.
	_, ub1 := boardHub.Subscribe("vendor-1")
	defer ub1()
	_, ub2 := boardHub.Subscribe("vendor-2")
	defer ub2()
	_, um1 := menuHub.Subscribe()
	defer um1()

	RegisterSSESubscriberGauge(boardHub, menuHub)

	data := collectMetric(t, reader, "tbite_sse_topic_subscribers")
	gauge, ok := data.(metricdata.Gauge[int64])
	if !ok {
		t.Fatalf("expected Gauge[int64], got %T", data)
	}
	got := map[string]int64{}
	for _, dp := range gauge.DataPoints {
		topic, _ := dp.Attributes.Value(attribute.Key("topic"))
		got[topic.AsString()] = dp.Value
	}
	if got["board"] != 2 {
		t.Errorf("board subscribers = %d, want 2", got["board"])
	}
	if got["menu"] != 1 {
		t.Errorf("menu subscribers = %d, want 1", got["menu"])
	}
}
