package observability

import (
	"context"
	"os"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestInit_NoEndpoint_NoOp(t *testing.T) {
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "")
	// Setenv only sets, not unsets — ensure absent.
	os.Unsetenv("OTEL_EXPORTER_OTLP_ENDPOINT")

	shutdown, err := Init(context.Background(), "test", "0.0.0")
	require.NoError(t, err)
	require.NotNil(t, shutdown)
	require.NoError(t, shutdown(context.Background()))
}

func TestOTLPProtocolFromEnv_DefaultsToHTTP(t *testing.T) {
	t.Setenv("OTEL_EXPORTER_OTLP_PROTOCOL", "")
	os.Unsetenv("OTEL_EXPORTER_OTLP_PROTOCOL")

	require.Equal(t, otlpProtocolHTTP, otlpProtocolFromEnv())
}

func TestOTLPEndpointHost_StripsURLSchemeForGRPC(t *testing.T) {
	host, err := otlpEndpointHost("http://otel-collector.tbite.svc:4317")
	require.NoError(t, err)
	require.Equal(t, "otel-collector.tbite.svc:4317", host)
}

func TestOTLPEndpointIsURL(t *testing.T) {
	require.True(t, otlpEndpointIsURL("http://otel-collector.tbite.svc:4318"))
	require.False(t, otlpEndpointIsURL("otel-collector.tbite.svc:4318"))
}
