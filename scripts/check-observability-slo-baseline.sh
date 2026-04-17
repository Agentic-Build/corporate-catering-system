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
  "ops/kubernetes/base/deployment-web.yaml"
  "ops/kubernetes/base/postgres-topology.yaml"
  "ops/kubernetes/base/pgbouncer.yaml"
  "ops/kubernetes/base/external-secrets.yaml"
  "ops/kubernetes/base/gateway.yaml"
  "ops/kubernetes/base/networkpolicy-default-deny.yaml"
  "ops/kubernetes/base/networkpolicy-runtime-allow.yaml"
  "ops/kubernetes/base/service-web.yaml"
  "ops/kubernetes/base/hpa.yaml"
  "ops/kubernetes/base/hpa-mcp.yaml"
  "ops/kubernetes/base/hpa-compliance-worker.yaml"
  "src/bin/observability_runtime_service.rs"
  "apps/web/src/hooks.server.ts"
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
rg -q "OrderApiLatencyP99Breach" ops/observability/slo/alerts.yaml
rg -q "p99LatencyMsMax" ops/observability/slo/hard-slo-policy.yaml
rg -q "p99LatencyMsMax" ops/observability/load/prelaunch-thresholds.yaml
rg -q --fixed-strings "p(99)<" ops/observability/load/k6-prelaunch.js
rg -q "peak-order-and-pickup-verification" ops/observability/load/k6-prelaunch.js
rg -q "/api/v1/employee/orders/.*/pickup-verifications" ops/observability/load/k6-prelaunch.js
rg -q "TOTP1:" ops/observability/load/k6-prelaunch.js
rg -q "specialRequests" ops/observability/load/k6-prelaunch.js
if rg -q "(parseOrderIdOrFallback|fallbackOrderId|specialRequestOption)" ops/observability/load/k6-prelaunch.js; then
  echo "legacy fallback or special-request payload shape detected in k6-prelaunch.js"
  exit 1
fi
rg -q "special_requests" src/bin/observability_runtime_service.rs
if rg -q "special_request_option" src/bin/observability_runtime_service.rs; then
  echo "legacy special_request_option field detected in observability runtime service"
  exit 1
fi
rg -q "HttpOrderingExecutionGateway::new" src/bin/observability_runtime_service.rs
rg -q "execute_create_employee_order" src/bin/observability_runtime_service.rs
rg -q "execute_update_employee_order" src/bin/observability_runtime_service.rs
rg -q "readinessProbe:" ops/kubernetes/base/deployment.yaml
rg -q "livenessProbe:" ops/kubernetes/base/deployment.yaml
rg -q "OTEL_EXPORTER_OTLP_ENDPOINT" ops/kubernetes/base/deployment.yaml
rg -q "OTEL_EXPORTER_OTLP_ENDPOINT" ops/kubernetes/base/deployment-mcp.yaml
rg -q "OTEL_EXPORTER_OTLP_ENDPOINT" ops/kubernetes/base/deployment-compliance-worker.yaml
rg -q "DATABASE_RW_URL" ops/kubernetes/base/deployment.yaml
rg -q "DATABASE_RO_URL" ops/kubernetes/base/deployment.yaml
rg -q "DATABASE_RW_URL" ops/kubernetes/base/deployment-mcp.yaml
rg -q "DATABASE_RO_URL" ops/kubernetes/base/deployment-mcp.yaml
rg -q "DATABASE_RW_URL" ops/kubernetes/base/deployment-compliance-worker.yaml
rg -q "DATABASE_RO_URL" ops/kubernetes/base/deployment-compliance-worker.yaml
if rg -q "DATABASE_URL" ops/kubernetes/base/deployment.yaml ops/kubernetes/base/deployment-mcp.yaml ops/kubernetes/base/deployment-compliance-worker.yaml; then
  echo "legacy direct DATABASE_URL wiring detected in runtime deployments"
  exit 1
fi
rg -q "corporate-catering-pgbouncer-rw" ops/kubernetes/base/pgbouncer.yaml
rg -q "corporate-catering-pgbouncer-ro" ops/kubernetes/base/pgbouncer.yaml
rg -q "PGBOUNCER_POOL_MODE" ops/kubernetes/base/pgbouncer.yaml
rg -q "transaction" ops/kubernetes/base/pgbouncer.yaml
rg -q "kind: Cluster" ops/kubernetes/base/postgres-topology.yaml
rg -q "corporate-catering-postgres-rw" ops/kubernetes/base/postgres-topology.yaml
rg -q "corporate-catering-postgres-ro" ops/kubernetes/base/postgres-topology.yaml
rg -q "kind: ExternalSecret" ops/kubernetes/base/external-secrets.yaml
rg -q "kind: Gateway" ops/kubernetes/base/gateway.yaml
rg -q "RateLimitPolicy" ops/kubernetes/base/gateway.yaml
rg -q "maxRequestBodyBytes" ops/kubernetes/base/gateway.yaml
rg -q "kind: NetworkPolicy" ops/kubernetes/base/networkpolicy-default-deny.yaml
rg -q "podSelector: {}" ops/kubernetes/base/networkpolicy-default-deny.yaml
rg -q "kind: NetworkPolicy" ops/kubernetes/base/networkpolicy-runtime-allow.yaml
rg -q "FRONTEND_RUNTIME_KIND" ops/kubernetes/base/deployment-web.yaml
rg -q "sveltekit-adapter-node" ops/kubernetes/base/deployment-web.yaml
rg -q "cache-control" apps/web/src/hooks.server.ts
rg -q "kind: HorizontalPodAutoscaler" ops/kubernetes/base/hpa.yaml
rg -q "http_server_requests_per_second" ops/kubernetes/base/hpa.yaml
rg -q "mcp_tool_requests_per_second" ops/kubernetes/base/hpa-mcp.yaml
rg -q "compliance_lifecycle_jobs_in_flight" ops/kubernetes/base/hpa-compliance-worker.yaml
rg -q "/api/v1/employee/orders" ops/observability/load/k6-prelaunch.js

collector_endpoint="$(awk '
  /^collector:/ { in_collector=1; next }
  in_collector && /^[[:space:]]+endpoint:/ { gsub(/^[[:space:]]+endpoint:[[:space:]]*/, "", $0); print $0; exit }
  in_collector && /^[^[:space:]]/ { in_collector=0 }
' ops/observability/otel/instrumentation-baseline.yaml)"
if [[ -z "${collector_endpoint}" ]]; then
  echo "failed to resolve collector.endpoint from ops/observability/otel/instrumentation-baseline.yaml"
  exit 1
fi

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
if [[ -z "${DATABASE_RW_URL:-}" ]]; then
  echo "DATABASE_RW_URL must be configured for hard-SLO baseline verification"
  exit 1
fi
if [[ -z "${DATABASE_RO_URL:-}" ]]; then
  echo "DATABASE_RO_URL must be configured for hard-SLO baseline verification"
  exit 1
fi

SQLX_OFFLINE="true" cargo run --quiet --bin apply_sql_migrations >/dev/null

summary_file="$(mktemp -t prelaunch-k6-summary.XXXXXX.json)"
service_log_file="$(mktemp -t prelaunch-k6-service.XXXXXX.log)"
service_pid=""

retained_summary="ops/observability/load/reports/prelaunch-k6-summary.json"
retained_report="ops/observability/load/reports/prelaunch-slo-report.json"
reports_dir="$(dirname "${retained_summary}")"
mkdir -p "${reports_dir}"

prelaunch_vendor_id="${PRELAUNCH_VENDOR_ID:-ven-load-gate-a}"
prelaunch_plant_id="${PRELAUNCH_PLANT_ID:-fab-a}"
prelaunch_menu_variant_count="${PRELAUNCH_MENU_VARIANT_COUNT:-64}"
prelaunch_delivery_day_offset="${PRELAUNCH_DELIVERY_DAY_OFFSET:-2}"
prelaunch_pickup_totp_secret="${PRELAUNCH_PICKUP_TOTP_SECRET:-prelaunch-hard-slo-secret}"
delivery_epoch_day="$(node - "${prelaunch_delivery_day_offset}" <<'NODE'
const offsetRaw = Number(process.argv[2]);
if (!Number.isInteger(offsetRaw) || offsetRaw < 1 || offsetRaw > 7) {
  throw new Error("PRELAUNCH_DELIVERY_DAY_OFFSET must be an integer between 1 and 7");
}
const unixSeconds = Math.floor(Date.now() / 1000);
const taipeiEpochDay = Math.floor((unixSeconds + 8 * 60 * 60) / 86400);
process.stdout.write(String(taipeiEpochDay + offsetRaw));
NODE
)"

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
PRELAUNCH_VENDOR_ID="${prelaunch_vendor_id}" \
PRELAUNCH_PLANT_ID="${prelaunch_plant_id}" \
PRELAUNCH_MENU_VARIANT_COUNT="${prelaunch_menu_variant_count}" \
PRELAUNCH_DELIVERY_EPOCH_DAY="${delivery_epoch_day}" \
PRELAUNCH_PICKUP_TOTP_SECRET="${prelaunch_pickup_totp_secret}" \
OTEL_SERVICE_NAME="catering-http-api" \
OTEL_EXPORTER_OTLP_ENDPOINT="${collector_endpoint}" \
SQLX_OFFLINE="true" \
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

BASE_URL="http://127.0.0.1:${PORT}" \
  PLANT_ID="${prelaunch_plant_id}" \
  MENU_VARIANT_COUNT="${prelaunch_menu_variant_count}" \
  DELIVERY_EPOCH_DAY="${delivery_epoch_day}" \
  PICKUP_TOTP_SECRET="${prelaunch_pickup_totp_secret}" \
  k6 run --quiet --summary-trend-stats "avg,min,med,max,p(90),p(95),p(99)" --summary-export "${summary_file}" ops/observability/load/k6-prelaunch.js

cp "${summary_file}" "${retained_summary}"

node - "${summary_file}" "ops/observability/slo/hard-slo-policy.yaml" "ops/observability/load/prelaunch-thresholds.yaml" "ops/observability/load/k6-prelaunch.js" "${retained_report}" "${retained_summary}" <<'NODE'
const fs = require("node:fs");

const summaryPath = process.argv[2];
const policyPath = process.argv[3];
const thresholdPath = process.argv[4];
const k6ScriptPath = process.argv[5];
const reportPath = process.argv[6];
const retainedSummaryPath = process.argv[7];

const summary = JSON.parse(fs.readFileSync(summaryPath, "utf8"));
const policyRaw = fs.readFileSync(policyPath, "utf8");
const thresholdRaw = fs.readFileSync(thresholdPath, "utf8");
const k6Script = fs.readFileSync(k6ScriptPath, "utf8");
const metrics = summary.metrics || {};

const policyScenarioPattern =
  /-\s+name:\s*([a-z0-9-]+)\s*\n\s*minRps:\s*([0-9.]+)\s*\n\s*p95LatencyMsMax:\s*([0-9.]+)\s*\n\s*p99LatencyMsMax:\s*([0-9.]+)\s*\n\s*errorRateMax:\s*([0-9.]+)\s*\n\s*readinessSuccessRateMin:\s*([0-9.]+)/g;
const thresholdScenarioPattern =
  /^  ([a-z0-9-]+):\s*\n    minRps:\s*([0-9.]+)\s*\n    thresholds:\s*\n      p95LatencyMsMax:\s*([0-9.]+)\s*\n      p99LatencyMsMax:\s*([0-9.]+)\s*\n      errorRateMax:\s*([0-9.]+)\s*\n      readinessSuccessRateMin:\s*([0-9.]+)/gm;

const policyScenarios = [];
for (const match of policyRaw.matchAll(policyScenarioPattern)) {
  policyScenarios.push({
    name: match[1],
    minRps: Number(match[2]),
    p95LatencyMsMax: Number(match[3]),
    p99LatencyMsMax: Number(match[4]),
    errorRateMax: Number(match[5]),
    readinessSuccessRateMin: Number(match[6])
  });
}

const thresholdScenarios = new Map();
for (const match of thresholdRaw.matchAll(thresholdScenarioPattern)) {
  thresholdScenarios.set(match[1], {
    minRps: Number(match[2]),
    p95LatencyMsMax: Number(match[3]),
    p99LatencyMsMax: Number(match[4]),
    errorRateMax: Number(match[5]),
    readinessSuccessRateMin: Number(match[6])
  });
}

const report = {
  generatedAt: new Date().toISOString(),
  summaryPath: retainedSummaryPath,
  scenarios: [],
  readiness: null,
  violations: [],
  status: "pass"
};

const addViolation = (message) => {
  report.violations.push(message);
  console.error(message);
};

if (policyScenarios.length === 0) {
  addViolation("failed to parse preLaunchLoadAcceptance.requiredScenarios from hard-slo-policy.yaml");
}
if (thresholdScenarios.size === 0) {
  addViolation("failed to parse scenarios from prelaunch-thresholds.yaml");
}

for (const scenario of policyScenarios) {
  const threshold = thresholdScenarios.get(scenario.name);
  if (!threshold) {
    addViolation(`policy scenario ${scenario.name} is missing from prelaunch-thresholds.yaml`);
    continue;
  }

  for (const key of ["minRps", "p95LatencyMsMax", "p99LatencyMsMax", "errorRateMax", "readinessSuccessRateMin"]) {
    if (Math.abs(Number(scenario[key]) - Number(threshold[key])) > 1e-9) {
      addViolation(`policy/threshold mismatch for scenario ${scenario.name} on ${key}`);
    }
  }

  const keyPattern = new RegExp(`["']${scenario.name.replace(/[.*+?^${}()|[\]\\]/g, "\\$&")}["']\\s*:\\s*\\{`);
  if (!keyPattern.test(k6Script)) {
    addViolation(`k6 scenario key ${scenario.name} is missing in k6-prelaunch.js`);
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
  const durationMetric = findScenarioMetric("http_req_duration", scenario);
  const failedMetric = findScenarioMetric("http_req_failed", scenario);

  if (!requestMetric) {
    addViolation(`missing request metric for scenario ${scenario}`);
    continue;
  }
  if (!durationMetric) {
    addViolation(`missing duration metric for scenario ${scenario}`);
    continue;
  }
  if (!failedMetric) {
    addViolation(`missing error-rate metric for scenario ${scenario}`);
    continue;
  }

  const observedRate = Number(
    requestMetric.metric?.rate ?? requestMetric.metric?.values?.rate ?? 0
  );
  const observedP95Raw =
    durationMetric.metric?.values?.["p(95)"] ??
    durationMetric.metric?.["p(95)"] ??
    durationMetric.metric?.p95;
  const observedP99Raw =
    durationMetric.metric?.values?.["p(99)"] ??
    durationMetric.metric?.["p(99)"] ??
    durationMetric.metric?.p99;
  if (observedP95Raw === undefined || observedP95Raw === null) {
    addViolation(`missing p95 latency quantile in summary metrics for scenario ${scenario}`);
    continue;
  }
  if (observedP99Raw === undefined || observedP99Raw === null) {
    addViolation(`missing p99 latency quantile in summary metrics for scenario ${scenario}`);
    continue;
  }
  const observedP95 = Number(observedP95Raw);
  const observedP99 = Number(observedP99Raw);
  const observedErrorRate = Number(
    failedMetric.metric?.rate ?? failedMetric.metric?.values?.rate ?? 0
  );

  report.scenarios.push({
    name: scenario,
    observed: {
      requestRate: observedRate,
      p95LatencyMs: observedP95,
      p99LatencyMs: observedP99,
      errorRate: observedErrorRate
    },
    thresholds: scenarioSpec
  });

  if (observedRate < scenarioSpec.minRps * 0.95) {
    addViolation(
      `scenario ${scenario} observed rate ${observedRate.toFixed(2)} rps is below required floor ${scenarioSpec.minRps}`
    );
  }
  if (observedP95 > scenarioSpec.p95LatencyMsMax) {
    addViolation(
      `scenario ${scenario} observed p95 latency ${observedP95.toFixed(2)}ms exceeds max ${scenarioSpec.p95LatencyMsMax}ms`
    );
  }
  if (observedP99 > scenarioSpec.p99LatencyMsMax) {
    addViolation(
      `scenario ${scenario} observed p99 latency ${observedP99.toFixed(2)}ms exceeds max ${scenarioSpec.p99LatencyMsMax}ms`
    );
  }
  if (observedErrorRate > scenarioSpec.errorRateMax) {
    addViolation(
      `scenario ${scenario} observed error rate ${observedErrorRate.toFixed(6)} exceeds max ${scenarioSpec.errorRateMax}`
    );
  }
}

const readinessCheckMetric = Object.entries(metrics).find(([name]) =>
  name.startsWith("checks") && name.includes("check_type:readiness")
);
if (!readinessCheckMetric) {
  addViolation("missing readiness check metric output");
} else {
  const readinessMetric = readinessCheckMetric[1] || {};
  let readinessRate = Number(readinessMetric.rate ?? readinessMetric.values?.rate ?? 0);
  if (!Number.isFinite(readinessRate) || readinessRate <= 0) {
    const passes = Number(readinessMetric.passes ?? readinessMetric.values?.passes ?? 0);
    const fails = Number(readinessMetric.fails ?? readinessMetric.values?.fails ?? 0);
    const total = passes + fails;
    readinessRate = total > 0 ? passes / total : 0;
  }

  const readinessMin = Math.min(
    ...policyScenarios.map((scenario) => scenario.readinessSuccessRateMin)
  );
  report.readiness = { observedRate: readinessRate, minimumRequired: readinessMin };
  if (readinessRate < readinessMin) {
    addViolation(
      `readiness success rate ${readinessRate.toFixed(5)} is below ${readinessMin}`
    );
  }
}

report.status = report.violations.length === 0 ? "pass" : "fail";
fs.writeFileSync(reportPath, JSON.stringify(report, null, 2));

if (report.violations.length > 0) {
  process.exit(1);
}
NODE

if [[ ! -s "${retained_summary}" ]]; then
  echo "retained k6 summary artifact is missing: ${retained_summary}"
  exit 1
fi
if [[ ! -s "${retained_report}" ]]; then
  echo "retained SLO evaluation artifact is missing: ${retained_report}"
  exit 1
fi

OTEL_EXPORTER_OTLP_ENDPOINT="${collector_endpoint}" \
cargo test --test observability_k8s_slo_baseline --test runtime_observability_instrumentation

echo "observability hard-SLO baseline checks passed"
echo "retained artifacts:"
echo "  - ${retained_summary}"
echo "  - ${retained_report}"
