package observability

import (
	"fmt"
	"net/url"
	"os"
	"strings"
)

const (
	otlpProtocolGRPC = "grpc"
	otlpProtocolHTTP = "http/protobuf"
)

func otlpEndpointFromEnv() string {
	return strings.TrimSpace(os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT"))
}

func otlpProtocolFromEnv() string {
	protocol := strings.ToLower(strings.TrimSpace(os.Getenv("OTEL_EXPORTER_OTLP_PROTOCOL")))
	if protocol == "" {
		return otlpProtocolHTTP
	}
	return protocol
}

func otlpEndpointIsURL(endpoint string) bool {
	u, err := url.Parse(endpoint)
	return err == nil && u.Scheme != "" && u.Host != ""
}

func otlpEndpointHost(endpoint string) (string, error) {
	if !otlpEndpointIsURL(endpoint) {
		return endpoint, nil
	}
	u, err := url.Parse(endpoint)
	if err != nil {
		return "", err
	}
	if u.Host == "" {
		return "", fmt.Errorf("missing host in OTLP endpoint %q", endpoint)
	}
	return u.Host, nil
}
