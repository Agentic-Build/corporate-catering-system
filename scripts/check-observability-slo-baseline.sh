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
rg -q "/api/v1/employee/orders" ops/observability/load/k6-prelaunch.js

cargo test --test observability_k8s_slo_baseline --test runtime_observability_instrumentation

if ! command -v cargo >/dev/null 2>&1; then
  echo "cargo is required to run the prelaunch load gate"
  exit 1
fi
if ! command -v node >/dev/null 2>&1; then
  echo "node is required to evaluate policy-driven load thresholds"
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

collector_endpoint="$(awk '
  /^collector:/ { in_collector=1; next }
  in_collector && /^[[:space:]]+endpoint:/ { gsub(/^[[:space:]]+endpoint:[[:space:]]*/, "", $0); print $0; exit }
  in_collector && /^[^[:space:]]/ { in_collector=0 }
' ops/observability/otel/instrumentation-baseline.yaml)"
if [[ -z "${collector_endpoint}" ]]; then
  echo "failed to resolve collector.endpoint from ops/observability/otel/instrumentation-baseline.yaml"
  exit 1
fi

PORT="${LOAD_GATE_PORT:-18080}"
PRELAUNCH_BIND_ADDR="127.0.0.1:${PORT}" \
OTEL_SERVICE_NAME="catering-http-api" \
OTEL_EXPORTER_OTLP_ENDPOINT="${collector_endpoint}" \
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

node - "${summary_file}" "ops/observability/slo/hard-slo-policy.yaml" "ops/observability/load/prelaunch-thresholds.yaml" "ops/observability/load/k6-prelaunch.js" <<'NODE'
const fs = require("node:fs");

const summaryPath = process.argv[2];
const policyPath = process.argv[3];
const thresholdPath = process.argv[4];
const k6ScriptPath = process.argv[5];
const summary = JSON.parse(fs.readFileSync(summaryPath, "utf8"));
const policyRaw = fs.readFileSync(policyPath, "utf8");
const thresholdRaw = fs.readFileSync(thresholdPath, "utf8");
const k6Script = fs.readFileSync(k6ScriptPath, "utf8");
const metrics = summary.metrics || {};

const policyScenarioPattern =
  /-\s+name:\s*([a-z0-9-]+)\s*\n\s*minRps:\s*([0-9.]+)\s*\n\s*p95LatencyMsMax:\s*([0-9.]+)\s*\n\s*errorRateMax:\s*([0-9.]+)\s*\n\s*readinessSuccessRateMin:\s*([0-9.]+)/g;
const thresholdScenarioPattern =
  /^  ([a-z0-9-]+):\s*\n    minRps:\s*([0-9.]+)\s*\n    thresholds:\s*\n      p95LatencyMsMax:\s*([0-9.]+)\s*\n      errorRateMax:\s*([0-9.]+)\s*\n      readinessSuccessRateMin:\s*([0-9.]+)/gm;

const policyScenarios = [];
for (const match of policyRaw.matchAll(policyScenarioPattern)) {
  policyScenarios.push({
    name: match[1],
    minRps: Number(match[2]),
    p95LatencyMsMax: Number(match[3]),
    errorRateMax: Number(match[4]),
    readinessSuccessRateMin: Number(match[5])
  });
}
if (policyScenarios.length === 0) {
  console.error("failed to parse preLaunchLoadAcceptance.requiredScenarios from hard-slo-policy.yaml");
  process.exit(1);
}

const thresholdScenarios = new Map();
for (const match of thresholdRaw.matchAll(thresholdScenarioPattern)) {
  thresholdScenarios.set(match[1], {
    minRps: Number(match[2]),
    p95LatencyMsMax: Number(match[3]),
    errorRateMax: Number(match[4]),
    readinessSuccessRateMin: Number(match[5])
  });
}
if (thresholdScenarios.size === 0) {
  console.error("failed to parse scenarios from prelaunch-thresholds.yaml");
  process.exit(1);
}

for (const scenario of policyScenarios) {
  const threshold = thresholdScenarios.get(scenario.name);
  if (!threshold) {
    console.error(`policy scenario ${scenario.name} is missing from prelaunch-thresholds.yaml`);
    process.exit(1);
  }
  for (const key of ["minRps", "p95LatencyMsMax", "errorRateMax", "readinessSuccessRateMin"]) {
    if (Math.abs(Number(scenario[key]) - Number(threshold[key])) > 1e-9) {
      console.error(`policy/threshold mismatch for scenario ${scenario.name} on ${key}`);
      process.exit(1);
    }
  }

  const keyPattern = new RegExp(`["']${scenario.name.replace(/[.*+?^${}()|[\]\\]/g, "\\$&")}["']\\s*:\\s*\\{`);
  if (!keyPattern.test(k6Script)) {
    console.error(`k6 scenario key ${scenario.name} is missing in k6-prelaunch.js`);
    process.exit(1);
  }
}

const findScenarioMetric = (prefix, scenario) => {
  for (const [name, metric] of Object.entries(metrics)) {
    if (name.startsWith(prefix) && name.includes(`scenario:${scenario}`)) {
      return { name, metric };
    }
  }
  return null;
};

for (const scenarioSpec of policyScenarios) {
  const scenario = scenarioSpec.name;
  const requestMetric = findScenarioMetric("http_reqs", scenario);
  if (!requestMetric) {
    console.error(`missing request metric for scenario ${scenario}`);
    process.exit(1);
  }
  const observedRate = Number(
    requestMetric.metric?.rate ?? requestMetric.metric?.values?.rate ?? 0
  );
  if (observedRate < scenarioSpec.minRps * 0.95) {
    console.error(
      `scenario ${scenario} observed rate ${observedRate.toFixed(2)} rps is below required floor ${scenarioSpec.minRps}`
    );
    process.exit(1);
  }

  const durationMetric = findScenarioMetric("http_req_duration", scenario);
  if (!durationMetric) {
    console.error(`missing duration metric for scenario ${scenario}`);
    process.exit(1);
  }
  const observedP95 = Number(
    durationMetric.metric?.values?.["p(95)"] ??
    durationMetric.metric?.["p(95)"] ??
    durationMetric.metric?.p95 ??
    0
  );
  if (observedP95 > scenarioSpec.p95LatencyMsMax) {
    console.error(
      `scenario ${scenario} observed p95 latency ${observedP95.toFixed(2)}ms exceeds max ${scenarioSpec.p95LatencyMsMax}ms`
    );
    process.exit(1);
  }

  const failedMetric = findScenarioMetric("http_req_failed", scenario);
  if (!failedMetric) {
    console.error(`missing error-rate metric for scenario ${scenario}`);
    process.exit(1);
  }
  const observedErrorRate = Number(
    failedMetric.metric?.rate ?? failedMetric.metric?.values?.rate ?? 0
  );
  if (observedErrorRate > scenarioSpec.errorRateMax) {
    console.error(
      `scenario ${scenario} observed error rate ${observedErrorRate.toFixed(6)} exceeds max ${scenarioSpec.errorRateMax}`
    );
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
const readinessMin = Math.min(...policyScenarios.map((scenario) => scenario.readinessSuccessRateMin));
if (readinessRate < readinessMin) {
  console.error(`readiness success rate ${readinessRate.toFixed(5)} is below ${readinessMin}`);
  process.exit(1);
}
NODE

echo "observability hard-SLO baseline checks passed"
