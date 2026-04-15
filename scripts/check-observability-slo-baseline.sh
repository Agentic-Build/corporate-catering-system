#!/usr/bin/env bash
set -euo pipefail

required_files=(
  "ops/observability/otel/collector.yaml"
  "ops/observability/otel/instrumentation-baseline.yaml"
  "ops/observability/slo/hard-slo-policy.yaml"
  "ops/observability/slo/alerts.yaml"
  "ops/observability/slo/grafana-dashboard-hard-slo.json"
  "ops/observability/load/prelaunch-thresholds.yaml"
  "ops/kubernetes/base/deployment.yaml"
  "ops/kubernetes/base/deployment-mcp.yaml"
  "ops/kubernetes/base/deployment-compliance-worker.yaml"
  "ops/kubernetes/base/hpa.yaml"
  "ops/kubernetes/base/hpa-mcp.yaml"
  "ops/kubernetes/base/hpa-compliance-worker.yaml"
)

for file in "${required_files[@]}"; do
  if [[ ! -f "${file}" ]]; then
    echo "missing required observability baseline artifact: ${file}"
    exit 1
  fi
done

rg -q "mode: blocking" ops/observability/slo/hard-slo-policy.yaml
rg -q "OrderApiAvailabilityBurnRateFast" ops/observability/slo/alerts.yaml
rg -q "OrderApiLatencyP95Breach" ops/observability/slo/alerts.yaml
rg -q "readinessProbe:" ops/kubernetes/base/deployment.yaml
rg -q "livenessProbe:" ops/kubernetes/base/deployment.yaml
rg -q "OTEL_EXPORTER_OTLP_ENDPOINT" ops/kubernetes/base/deployment.yaml
rg -q "OTEL_EXPORTER_OTLP_ENDPOINT" ops/kubernetes/base/deployment-mcp.yaml
rg -q "OTEL_EXPORTER_OTLP_ENDPOINT" ops/kubernetes/base/deployment-compliance-worker.yaml
rg -q "kind: HorizontalPodAutoscaler" ops/kubernetes/base/hpa.yaml
rg -q "http_server_requests_per_second" ops/kubernetes/base/hpa.yaml
rg -q "mcp_tool_requests_per_second" ops/kubernetes/base/hpa-mcp.yaml
rg -q "compliance_lifecycle_jobs_in_flight" ops/kubernetes/base/hpa-compliance-worker.yaml

cargo test --test observability_k8s_slo_baseline

echo "observability hard-SLO baseline checks passed"
