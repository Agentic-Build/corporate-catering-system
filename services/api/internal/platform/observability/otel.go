package observability

import (
	"context"
	"fmt"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
)

// Init configures the global tracer provider.
// Reads OTEL_EXPORTER_OTLP_ENDPOINT (default: empty -> no-op tracer).
// Returns a shutdown func that flushes pending spans.
func Init(ctx context.Context, serviceName, version string) (func(context.Context) error, error) {
	endpoint := otlpEndpointFromEnv()
	if endpoint == "" {
		// No-op: just set the global propagator and return a no-op shutdown.
		// The default global tracer provider is a no-op, so leaving it alone
		// means span starts/ends are essentially free.
		otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
			propagation.TraceContext{},
			propagation.Baggage{},
		))
		return func(context.Context) error { return nil }, nil
	}

	exporter, err := newTraceExporter(ctx, endpoint, otlpProtocolFromEnv())
	if err != nil {
		return nil, fmt.Errorf("otlp exporter: %w", err)
	}

	// resource.Default() reads OTEL_SERVICE_NAME + OTEL_RESOURCE_ATTRIBUTES,
	// so passing it second lets env vars override the role-derived defaults.
	res, _ := resource.Merge(
		resource.NewWithAttributes(
			semconv.SchemaURL,
			semconv.ServiceName(serviceName),
			semconv.ServiceVersion(version),
		),
		resource.Default(),
	)

	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter, sdktrace.WithBatchTimeout(5*time.Second)),
		sdktrace.WithResource(res),
	)
	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))

	return tp.Shutdown, nil
}

func newTraceExporter(ctx context.Context, endpoint, protocol string) (*otlptrace.Exporter, error) {
	switch protocol {
	case otlpProtocolGRPC:
		host, err := otlpEndpointHost(endpoint)
		if err != nil {
			return nil, err
		}
		return otlptracegrpc.New(ctx,
			otlptracegrpc.WithEndpoint(host),
			otlptracegrpc.WithInsecure(),
		)
	case otlpProtocolHTTP:
		opts := []otlptracehttp.Option{}
		if otlpEndpointIsURL(endpoint) {
			opts = append(opts, otlptracehttp.WithEndpointURL(endpoint))
		} else {
			opts = append(opts,
				otlptracehttp.WithEndpoint(endpoint),
				otlptracehttp.WithInsecure(),
			)
		}
		return otlptrace.New(ctx, otlptracehttp.NewClient(opts...))
	default:
		return nil, fmt.Errorf("unsupported OTEL_EXPORTER_OTLP_PROTOCOL %q", protocol)
	}
}
