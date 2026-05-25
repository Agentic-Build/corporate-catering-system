package main

// SSE-side OTel instruments used by the realtime gateway role and
// observed by chart/tbite-platform/dashboards/sse-gateway.json.
//
// The instruments are lazily initialised on first use so a process
// that never opens an SSE connection (api, workers, scheduler) does
// not register them. The realtime role calls realtimeMetrics() once
// during boot.

import (
	"context"
	"sync"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"github.com/takalawang/corporate-catering-system/services/api/internal/order"
)

type sseInstruments struct {
	activeConnections metric.Int64UpDownCounter
	fanoutLag         metric.Float64Histogram
	disconnects       metric.Int64Counter
}

var (
	sseOnce   sync.Once
	sseInstr  sseInstruments
	sseInited bool
)

func realtimeMetrics() sseInstruments {
	sseOnce.Do(func() {
		meter := otel.GetMeterProvider().Meter("tbite.realtime")
		conn, err := meter.Int64UpDownCounter("tbite_sse_active_connections",
			metric.WithDescription("Current number of open SSE connections on this realtime gateway pod."))
		if err == nil {
			sseInstr.activeConnections = conn
		}
		lag, err := meter.Float64Histogram("tbite_sse_fanout_lag_seconds",
			metric.WithDescription("Wall time between event arrival at the gateway and delivery to a subscriber."),
			metric.WithUnit("s"))
		if err == nil {
			sseInstr.fanoutLag = lag
		}
		disc, err := meter.Int64Counter("tbite_sse_disconnects_total",
			metric.WithDescription("Total SSE disconnects, split by surface and reason."))
		if err == nil {
			sseInstr.disconnects = disc
		}
		sseInited = true
	})
	return sseInstr
}

// onConnect increments the active-connection gauge labelled by topic
// surface so the dashboard can split board vs menu vs future surfaces.
func sseOnConnect(ctx context.Context, surface string) {
	m := realtimeMetrics()
	if m.activeConnections == nil {
		return
	}
	m.activeConnections.Add(ctx, 1, metric.WithAttributes(attribute.String("surface", surface)))
}

// onDisconnect decrements the gauge and counts the disconnect by
// surface + reason. Pairs with sseOnConnect via `defer`.
func sseOnDisconnect(ctx context.Context, surface, reason string) {
	m := realtimeMetrics()
	if m.activeConnections != nil {
		m.activeConnections.Add(ctx, -1, metric.WithAttributes(attribute.String("surface", surface)))
	}
	if m.disconnects != nil {
		m.disconnects.Add(ctx, 1, metric.WithAttributes(
			attribute.String("surface", surface),
			attribute.String("reason", reason)))
	}
}

// RegisterSSESubscriberGauge registers the topic-subscriber observable
// gauge, observing live subscriber counts from the board and menu hubs.
func RegisterSSESubscriberGauge(boardHub *order.BoardHub, menuHub *order.MenuHub) {
	meter := otel.GetMeterProvider().Meter("tbite.realtime")
	gauge, err := meter.Int64ObservableGauge("tbite_sse_topic_subscribers",
		metric.WithDescription("Current number of SSE subscribers per topic on this realtime gateway pod."))
	if err != nil {
		return
	}
	_, _ = meter.RegisterCallback(func(_ context.Context, o metric.Observer) error {
		if boardHub != nil {
			o.ObserveInt64(gauge, int64(boardHub.SubscriberCount()),
				metric.WithAttributes(attribute.String("topic", "board")))
		}
		if menuHub != nil {
			o.ObserveInt64(gauge, int64(menuHub.SubscriberCount()),
				metric.WithAttributes(attribute.String("topic", "menu")))
		}
		return nil
	}, gauge)
}

// recordFanoutLag records the time between an event becoming
// available at the hub (Publish/Broadcast) and the SSE write that
// delivers it. The handler measures wall time between dequeue and
// successful write.
func sseRecordFanoutLag(ctx context.Context, surface string, lag time.Duration) {
	m := realtimeMetrics()
	if m.fanoutLag == nil {
		return
	}
	m.fanoutLag.Record(ctx, lag.Seconds(),
		metric.WithAttributes(attribute.String("surface", surface)))
}
