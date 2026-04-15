#!/usr/bin/env bash
set -euo pipefail

required_files=(
  "ops/observability/otel/collector.yaml"
  "ops/observability/otel/instrumentation-baseline.yaml"
  "ops/observability/slo/hard-slo-policy.yaml"
  "ops/observability/slo/alerts.yaml"
  "ops/observability/slo/grafana-dashboard-hard-slo.json"
  "ops/observability/load/prelaunch-thresholds.yaml"
  "ops/observability/load/k6-prelaunch.js"
  "ops/kubernetes/base/deployment.yaml"
  "ops/kubernetes/base/deployment-mcp.yaml"
  "ops/kubernetes/base/deployment-compliance-worker.yaml"
  "ops/kubernetes/base/hpa.yaml"
  "ops/kubernetes/base/hpa-mcp.yaml"
  "ops/kubernetes/base/hpa-compliance-worker.yaml"
  "src/bin/observability_runtime_service.rs"
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
rg -q "peak_order_placement" ops/observability/load/k6-prelaunch.js
rg -q "mixed_order_and_menu_reads" ops/observability/load/k6-prelaunch.js
rg -q "/api/v1/employee/orders" ops/observability/load/k6-prelaunch.js

cargo test --test observability_k8s_slo_baseline --test runtime_observability_instrumentation

if ! command -v cargo >/dev/null 2>&1; then
  echo "cargo is required to run the prelaunch load gate"
  exit 1
fi
if ! command -v k6 >/dev/null 2>&1; then
  echo "k6 is required to enforce prelaunch load thresholds"
  exit 1
fi

summary_file="$(mktemp -t prelaunch-k6-summary.XXXXXX.json)"
service_log_file="$(mktemp -t prelaunch-k6-service.XXXXXX.log)"
service_pid=""

cleanup() {
  if [[ -n "${service_pid}" ]]; then
    kill "${service_pid}" >/dev/null 2>&1 || true
    wait "${service_pid}" >/dev/null 2>&1 || true
  fi
  rm -f "${summary_file}" "${service_log_file}"
}
trap cleanup EXIT

PORT="${LOAD_GATE_PORT:-18080}"
PRELAUNCH_BIND_ADDR="127.0.0.1:${PORT}" \
OTEL_SERVICE_NAME="catering-http-api" \
cargo run --quiet --bin observability_runtime_service >"${service_log_file}" 2>&1 &
service_pid=$!

for _ in {1..40}; do
  if curl --silent --fail --show-error "http://127.0.0.1:${PORT}/health/ready" >/dev/null; then
    break
  fi
  sleep 0.25
done

if ! curl --silent --fail --show-error "http://127.0.0.1:${PORT}/health/ready" >/dev/null; then
  echo "observability runtime service failed to start"
  cat "${service_log_file}"
  exit 1
fi

BASE_URL="http://127.0.0.1:${PORT}" k6 run --summary-export "${summary_file}" ops/observability/load/k6-prelaunch.js

node - "${summary_file}" <<'NODE'
const fs = require("node:fs");

const summaryPath = process.argv[2];
const summary = JSON.parse(fs.readFileSync(summaryPath, "utf8"));
const metrics = summary.metrics || {};

const requiredScenarios = {
  peak_order_placement: 120,
  mixed_order_and_menu_reads: 180
};

const findScenarioMetric = (prefix, scenario) => {
  for (const [name, metric] of Object.entries(metrics)) {
    if (name.startsWith(prefix) && name.includes(`scenario:${scenario}`)) {
      return { name, metric };
    }
  }
  return null;
};

for (const [scenario, minRps] of Object.entries(requiredScenarios)) {
  const requestMetric = findScenarioMetric("http_reqs", scenario);
  if (!requestMetric) {
    console.error(`missing request metric for scenario ${scenario}`);
    process.exit(1);
  }
  const observedRate = Number(
    requestMetric.metric?.rate ?? requestMetric.metric?.values?.rate ?? 0
  );
  if (observedRate < minRps * 0.95) {
    console.error(
      `scenario ${scenario} observed rate ${observedRate.toFixed(2)} rps is below required floor ${minRps}`
    );
    process.exit(1);
  }

  if (!findScenarioMetric("http_req_duration", scenario)) {
    console.error(`missing duration metric for scenario ${scenario}`);
    process.exit(1);
  }
}

const readinessCheckMetric = Object.entries(metrics).find(([name]) =>
  name.startsWith("checks") && name.includes("check_type:readiness")
);
if (!readinessCheckMetric) {
  console.error("missing readiness check metric output");
  process.exit(1);
}
const readinessMetric = readinessCheckMetric[1] || {};
let readinessRate = Number(readinessMetric.rate ?? readinessMetric.values?.rate ?? 0);
if (!Number.isFinite(readinessRate) || readinessRate <= 0) {
  const passes = Number(readinessMetric.passes ?? readinessMetric.values?.passes ?? 0);
  const fails = Number(readinessMetric.fails ?? readinessMetric.values?.fails ?? 0);
  const total = passes + fails;
  readinessRate = total > 0 ? passes / total : 0;
}
if (readinessRate < 0.999) {
  console.error(`readiness success rate ${readinessRate.toFixed(5)} is below 0.999`);
  process.exit(1);
}
NODE

echo "observability hard-SLO baseline checks passed"
