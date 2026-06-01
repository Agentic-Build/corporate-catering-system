package observability

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
)

// cancelledCtx returns an already-cancelled context. Passing it to a provider
// Shutdown drives the shutdown/flush code path without letting the exporter
// actually dial the (non-existent) collector and block on DNS.
func cancelledCtx() context.Context {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	return ctx
}

// initMeterAndShutdown runs InitMeter with the supplied env and immediately
// shuts the provider down so the periodic reader goroutine does not leak.
func initMeterAndShutdown(t *testing.T, endpoint, protocol string) (func(context.Context) error, error) {
	t.Helper()
	if endpoint == "" {
		t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "")
	} else {
		t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", endpoint)
	}
	t.Setenv("OTEL_EXPORTER_OTLP_PROTOCOL", protocol)
	previous := otel.GetMeterProvider()
	t.Cleanup(func() { otel.SetMeterProvider(previous) })
	return InitMeter(context.Background(), "test-svc", "1.2.3")
}

func TestInitMeter_NoEndpoint_NoOp(t *testing.T) {
	shutdown, err := initMeterAndShutdown(t, "", "")
	require.NoError(t, err)
	require.NotNil(t, shutdown)
	require.NoError(t, shutdown(context.Background()))
}

func TestInitMeter_HTTPEndpointURL(t *testing.T) {
	shutdown, err := initMeterAndShutdown(t, "http://collector.local:4318", otlpProtocolHTTP)
	require.NoError(t, err)
	require.NotNil(t, shutdown)
	// Shutdown with a cancelled ctx exercises the flush path without dialing.
	_ = shutdown(cancelledCtx())
}

func TestInitMeter_HTTPEndpointBareHostPort(t *testing.T) {
	// Bare host:port (not a URL) exercises the WithEndpoint+WithInsecure branch.
	shutdown, err := initMeterAndShutdown(t, "collector.local:4318", otlpProtocolHTTP)
	require.NoError(t, err)
	require.NotNil(t, shutdown)
	_ = shutdown(cancelledCtx())
}

func TestInitMeter_GRPCEndpoint(t *testing.T) {
	shutdown, err := initMeterAndShutdown(t, "http://collector.local:4317", otlpProtocolGRPC)
	require.NoError(t, err)
	require.NotNil(t, shutdown)
	_ = shutdown(cancelledCtx())
}

func TestInitMeter_UnsupportedProtocol(t *testing.T) {
	shutdown, err := initMeterAndShutdown(t, "collector.local:4318", "thrift")
	require.Error(t, err)
	assert.Nil(t, shutdown)
	assert.Contains(t, err.Error(), "otlp metric exporter")
}

func TestNewMetricExporter_Unsupported(t *testing.T) {
	_, err := newMetricExporter(context.Background(), "collector.local:4318", "bogus")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported OTEL_EXPORTER_OTLP_PROTOCOL")
}

// initTracerAndShutdown wires Init with the supplied env, restoring the global
// providers afterwards.
func initTracerAndShutdown(t *testing.T, endpoint, protocol string) (func(context.Context) error, error) {
	t.Helper()
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", endpoint)
	t.Setenv("OTEL_EXPORTER_OTLP_PROTOCOL", protocol)
	previousTP := otel.GetTracerProvider()
	previousProp := otel.GetTextMapPropagator()
	t.Cleanup(func() {
		otel.SetTracerProvider(previousTP)
		otel.SetTextMapPropagator(previousProp)
	})
	return Init(context.Background(), "test-svc", "1.2.3")
}

func TestInit_HTTPEndpointURL(t *testing.T) {
	shutdown, err := initTracerAndShutdown(t, "http://collector.local:4318", otlpProtocolHTTP)
	require.NoError(t, err)
	require.NotNil(t, shutdown)
	_ = shutdown(cancelledCtx())
}

func TestInit_HTTPEndpointBareHostPort(t *testing.T) {
	shutdown, err := initTracerAndShutdown(t, "collector.local:4318", otlpProtocolHTTP)
	require.NoError(t, err)
	require.NotNil(t, shutdown)
	_ = shutdown(cancelledCtx())
}

func TestInit_GRPCEndpoint(t *testing.T) {
	shutdown, err := initTracerAndShutdown(t, "http://collector.local:4317", otlpProtocolGRPC)
	require.NoError(t, err)
	require.NotNil(t, shutdown)
	_ = shutdown(cancelledCtx())
}

func TestInit_UnsupportedProtocol(t *testing.T) {
	shutdown, err := initTracerAndShutdown(t, "collector.local:4318", "thrift")
	require.Error(t, err)
	assert.Nil(t, shutdown)
	assert.Contains(t, err.Error(), "otlp exporter")
}

func TestNewTraceExporter_Unsupported(t *testing.T) {
	_, err := newTraceExporter(context.Background(), "collector.local:4318", "bogus")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported OTEL_EXPORTER_OTLP_PROTOCOL")
}

func TestOTLPProtocolFromEnv_Explicit(t *testing.T) {
	t.Setenv("OTEL_EXPORTER_OTLP_PROTOCOL", "  GRPC ")
	assert.Equal(t, otlpProtocolGRPC, otlpProtocolFromEnv())
}

func TestOTLPEndpointHost_BareReturnedAsIs(t *testing.T) {
	host, err := otlpEndpointHost("collector.local:4317")
	require.NoError(t, err)
	assert.Equal(t, "collector.local:4317", host)
}

func TestOTLPEndpointHost_ParseError(t *testing.T) {
	// otlpEndpointIsURL("://bad") is false (Scheme empty), so otlpEndpointHost
	// returns it verbatim without error.
	host, err := otlpEndpointHost("://bad")
	require.NoError(t, err)
	assert.Equal(t, "://bad", host)
}
