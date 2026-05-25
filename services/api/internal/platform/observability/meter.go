package observability

import (
	"context"
	"fmt"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetricgrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp"
	"go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
)

// InitMeter configures the global MeterProvider. Reads
// OTEL_EXPORTER_OTLP_ENDPOINT (default: empty -> no-op meter). The returned
// shutdown func flushes pending metric data points.
//
// Domain metric emission lives in package-level instruments built in metrics.go;
// callers must invoke MustInitMetrics after InitMeter so those instruments bind
// to the real MeterProvider.
func InitMeter(ctx context.Context, serviceName, version string) (func(context.Context) error, error) {
	endpoint := otlpEndpointFromEnv()
	if endpoint == "" {
		// Default no-op MeterProvider keeps instrument calls cheap.
		return func(context.Context) error { return nil }, nil
	}

	exporter, err := newMetricExporter(ctx, endpoint, otlpProtocolFromEnv())
	if err != nil {
		return nil, fmt.Errorf("otlp metric exporter: %w", err)
	}

	// resource.Default() reads OTEL_SERVICE_NAME + OTEL_RESOURCE_ATTRIBUTES.
	// Placing it second lets env-provided values take precedence over the
	// role-derived defaults — same convention used by InitTracer.
	res, _ := resource.Merge(
		resource.NewWithAttributes(
			semconv.SchemaURL,
			semconv.ServiceName(serviceName),
			semconv.ServiceVersion(version),
		),
		resource.Default(),
	)

	// Override the SDK default histogram boundaries (0,5,10,…,10000) for
	// duration histograms. The defaults assume the metric is in milliseconds;
	// our duration instruments emit in seconds (unit "s"), so without this
	// every measurement falls in the [0,5] bucket and quantiles snap to bucket
	// midpoints (~2.5s at p50). The boundary list below matches OTel semconv
	// recommendations for HTTP server latency and is what otelhttp already
	// uses for `http.server.request.duration`.
	secondsView := metric.NewView(
		metric.Instrument{Unit: "s"},
		metric.Stream{
			Aggregation: metric.AggregationExplicitBucketHistogram{
				Boundaries: []float64{0.005, 0.01, 0.025, 0.05, 0.075, 0.1, 0.25, 0.5, 0.75, 1, 2.5, 5, 7.5, 10},
			},
		},
	)

	mp := metric.NewMeterProvider(
		metric.WithResource(res),
		metric.WithView(secondsView),
		metric.WithReader(metric.NewPeriodicReader(
			exporter,
			metric.WithInterval(15*time.Second),
		)),
	)
	otel.SetMeterProvider(mp)
	return mp.Shutdown, nil
}

func newMetricExporter(ctx context.Context, endpoint, protocol string) (metric.Exporter, error) {
	switch protocol {
	case otlpProtocolGRPC:
		host, err := otlpEndpointHost(endpoint)
		if err != nil {
			return nil, err
		}
		return otlpmetricgrpc.New(ctx,
			otlpmetricgrpc.WithEndpoint(host),
			otlpmetricgrpc.WithInsecure(),
		)
	case otlpProtocolHTTP:
		opts := []otlpmetrichttp.Option{}
		if otlpEndpointIsURL(endpoint) {
			opts = append(opts, otlpmetrichttp.WithEndpointURL(endpoint))
		} else {
			opts = append(opts,
				otlpmetrichttp.WithEndpoint(endpoint),
				otlpmetrichttp.WithInsecure(),
			)
		}
		return otlpmetrichttp.New(ctx, opts...)
	default:
		return nil, fmt.Errorf("unsupported OTEL_EXPORTER_OTLP_PROTOCOL %q", protocol)
	}
}
