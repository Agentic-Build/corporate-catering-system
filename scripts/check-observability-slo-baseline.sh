#!/usr/bin/env bash
set -euo pipefail

required_files=(
  "ops/observability/otel/collector.yaml"
  "ops/observability/otel/instrumentation-baseline.yaml"
  "ops/observability/slo/hard-slo-policy.yaml"
  "ops/observability/slo/alerts.yaml"
  "ops/observability/slo/grafana-dashboard-hard-slo.json"
  "ops/observability/load/prelaunch-thresholds.yaml"
  "ops/observability/load/staged-capacity-policy.json"
  "ops/observability/load/k6-prelaunch.js"
  "scripts/evaluate-prelaunch-load.py"
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
  "ops/kubernetes/base/hpa-web.yaml"
  "ops/kubernetes/components/topology-multi-az/kustomization.yaml"
  "ops/kubernetes/components/topology-multi-az/patch-deployment-api.yaml"
  "ops/kubernetes/components/autoscaling-keda-worker/kustomization.yaml"
  "ops/kubernetes/components/autoscaling-keda-worker/scaledobject-compliance-worker.yaml"
  "ops/kubernetes/components/autoscaling-keda-worker/delete-hpa-compliance-worker.yaml"
  "ops/kubernetes/overlays/dev/kustomization.yaml"
  "ops/kubernetes/overlays/staging/kustomization.yaml"
  "ops/kubernetes/overlays/production/kustomization.yaml"
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
rg -q "staged_phase" ops/observability/load/k6-prelaunch.js
rg -q "load_split" ops/observability/load/k6-prelaunch.js
rg -q "peak-order-and-pickup-verification" ops/observability/load/k6-prelaunch.js
rg -q "/api/v1/employee/orders/.*/pickup-verifications" ops/observability/load/k6-prelaunch.js
rg -q "TOTP1:" ops/observability/load/k6-prelaunch.js
rg -q '"clarificationIds": \[' ops/observability/load/staged-capacity-policy.json
rg -q '"CLAR-008"' ops/observability/load/staged-capacity-policy.json
rg -q '"decisionIssueId": "ISS-005"' ops/observability/load/staged-capacity-policy.json
rg -q '"value": 25000' ops/observability/load/staged-capacity-policy.json
rg -q "specialRequests" ops/observability/load/k6-prelaunch.js
if rg -q "(parseOrderIdOrFallback|fallbackOrderId|specialRequestOption)" ops/observability/load/k6-prelaunch.js; then
  echo "legacy fallback or special-request payload shape detected in k6-prelaunch.js"
  exit 1
fi
rg -q "minReplicas: 4" ops/kubernetes/overlays/staging/patch-autoscaling.yaml
rg -q "maxReplicas: 16" ops/kubernetes/overlays/staging/patch-autoscaling.yaml
rg -q 'averageValue: "85"' ops/kubernetes/overlays/staging/patch-autoscaling.yaml
rg -q "stabilizationWindowSeconds: 15" ops/kubernetes/overlays/staging/patch-autoscaling.yaml
rg -q "stabilizationWindowSeconds: 240" ops/kubernetes/overlays/staging/patch-autoscaling.yaml
rg -q "cooldownPeriod: 240" ops/kubernetes/overlays/staging/patch-autoscaling.yaml
rg -q 'threshold: "14"' ops/kubernetes/overlays/staging/patch-autoscaling.yaml
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
for hardened_manifest in \
  ops/kubernetes/base/deployment.yaml \
  ops/kubernetes/base/deployment-mcp.yaml \
  ops/kubernetes/base/deployment-compliance-worker.yaml \
  ops/kubernetes/base/deployment-web.yaml \
  ops/kubernetes/base/pgbouncer.yaml \
  ops/kubernetes/base/job-object-storage-provision.yaml
do
  rg -q "automountServiceAccountToken: false" "${hardened_manifest}"
  rg -q "runAsNonRoot: true" "${hardened_manifest}"
  rg -q "seccompProfile:" "${hardened_manifest}"
  rg -q "type: RuntimeDefault" "${hardened_manifest}"
  rg -q "allowPrivilegeEscalation: false" "${hardened_manifest}"
  rg -q "capabilities:" "${hardened_manifest}"
  rg -q "drop:" "${hardened_manifest}"
  rg -q "ALL" "${hardened_manifest}"
done
rg -q "corporate-catering-pgbouncer-rw" ops/kubernetes/base/pgbouncer.yaml
rg -q "corporate-catering-pgbouncer-ro" ops/kubernetes/base/pgbouncer.yaml
rg -q "PGBOUNCER_POOL_MODE" ops/kubernetes/base/pgbouncer.yaml
rg -q "transaction" ops/kubernetes/base/pgbouncer.yaml
rg -q "kind: Cluster" ops/kubernetes/base/postgres-topology.yaml
rg -q "corporate-catering-postgres-rw" ops/kubernetes/base/postgres-topology.yaml
rg -q "corporate-catering-postgres-ro" ops/kubernetes/base/postgres-topology.yaml
rg -q "kind: ExternalSecret" ops/kubernetes/base/external-secrets.yaml
rg -q "kind: Gateway" ops/kubernetes/base/gateway.yaml
rg -q "protocol: HTTPS" ops/kubernetes/base/gateway.yaml
if rg -q '^[[:space:]]*protocol:[[:space:]]*HTTP[[:space:]]*$' ops/kubernetes/base/gateway.yaml; then
  echo "gateway must not expose plaintext HTTP listeners"
  exit 1
fi
rg -q "allowMethods" ops/kubernetes/base/gateway.yaml
rg -q '^[[:space:]]*-[[:space:]]*DELETE[[:space:]]*$' ops/kubernetes/base/gateway.yaml
rg -q "RateLimitPolicy" ops/kubernetes/base/gateway.yaml
rg -q "maxRequestBodyBytes" ops/kubernetes/base/gateway.yaml
rg -q "kind: NetworkPolicy" ops/kubernetes/base/networkpolicy-default-deny.yaml
rg -q --fixed-strings "podSelector: {}" ops/kubernetes/base/networkpolicy-default-deny.yaml
rg -q "kind: NetworkPolicy" ops/kubernetes/base/networkpolicy-runtime-allow.yaml
rg -q "corporate-catering-postgres-allow-pgbouncer-and-cluster" ops/kubernetes/base/networkpolicy-runtime-allow.yaml
rg -q "corporate-catering-runtime-allow-egress-object-storage" ops/kubernetes/base/networkpolicy-runtime-allow.yaml
rg -q "corporate-catering-object-storage-provision" ops/kubernetes/base/networkpolicy-runtime-allow.yaml
rg -q "port: 5432" ops/kubernetes/base/networkpolicy-runtime-allow.yaml
rg -q "port: 9000" ops/kubernetes/base/networkpolicy-runtime-allow.yaml
rg -q "FRONTEND_RUNTIME_KIND" ops/kubernetes/base/deployment-web.yaml
rg -q "sveltekit-adapter-node" ops/kubernetes/base/deployment-web.yaml
rg -q "cache-control" apps/web/src/hooks.server.ts
rg -q "kind: HorizontalPodAutoscaler" ops/kubernetes/base/hpa.yaml
rg -q "http_server_requests_per_second" ops/kubernetes/base/hpa.yaml
rg -q "mcp_tool_requests_per_second" ops/kubernetes/base/hpa-mcp.yaml
rg -q "compliance_lifecycle_jobs_in_flight" ops/kubernetes/base/hpa-compliance-worker.yaml
rg -q "kind: HorizontalPodAutoscaler" ops/kubernetes/base/hpa-web.yaml
rg -q "name: memory" ops/kubernetes/base/hpa-web.yaml
rg -q "topology.kubernetes.io/zone" ops/kubernetes/components/topology-multi-az/patch-deployment-api.yaml
rg -q "kind: ScaledObject" ops/kubernetes/components/autoscaling-keda-worker/scaledobject-compliance-worker.yaml
rg -q --fixed-strings '$patch: delete' ops/kubernetes/components/autoscaling-keda-worker/delete-hpa-compliance-worker.yaml
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
if ! command -v python3 >/dev/null 2>&1; then
  echo "python3 is required to evaluate staged capacity policy"
  exit 1
fi
if ! command -v k6 >/dev/null 2>&1; then
  echo "k6 is required to enforce prelaunch load thresholds"
  exit 1
fi
if ! command -v kustomize >/dev/null 2>&1; then
  echo "kustomize is required to validate dev/staging/production overlays"
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

for overlay in dev staging production; do
  overlay_manifest="$(mktemp -t kustomize-${overlay}.XXXXXX.yaml)"
  kustomize build "ops/kubernetes/overlays/${overlay}" >"${overlay_manifest}"
  rg -q "kind: ScaledObject" "${overlay_manifest}"
  rg -q "topologySpreadConstraints" "${overlay_manifest}"
  rg -q "name: corporate-catering-compliance-worker" "${overlay_manifest}"
  rg -q "runtime.corporate-catering.io/scaling-strategy: hpa-plus-keda-worker" "${overlay_manifest}"
  case "${overlay}" in
    dev)
      rg -q "key: dev/corporate-catering/runtime" "${overlay_manifest}"
      ;;
    staging)
      rg -q "key: staging/corporate-catering/runtime" "${overlay_manifest}"
      ;;
    production)
      rg -q "key: prod/corporate-catering/runtime" "${overlay_manifest}"
      ;;
  esac
  rm -f "${overlay_manifest}"
done

summary_file="$(mktemp -t prelaunch-k6-summary.XXXXXX.json)"
service_log_file="$(mktemp -t prelaunch-k6-service.XXXXXX.log)"
service_pid=""

retained_summary="ops/observability/load/reports/prelaunch-k6-summary.json"
retained_report="ops/observability/load/reports/prelaunch-slo-report.json"
retained_staged_report="ops/observability/load/reports/staged-capacity-report.json"
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

python3 scripts/evaluate-prelaunch-load.py \
  --summary "${summary_file}" \
  --hard-slo-policy "ops/observability/slo/hard-slo-policy.yaml" \
  --thresholds "ops/observability/load/prelaunch-thresholds.yaml" \
  --staged-policy "ops/observability/load/staged-capacity-policy.json" \
  --k6-script "ops/observability/load/k6-prelaunch.js" \
  --autoscaling-manifest "ops/kubernetes/overlays/staging/patch-autoscaling.yaml" \
  --slo-report "${retained_report}" \
  --staged-report "${retained_staged_report}" \
  --retained-summary-path "${retained_summary}"

if [[ ! -s "${retained_summary}" ]]; then
  echo "retained k6 summary artifact is missing: ${retained_summary}"
  exit 1
fi
if [[ ! -s "${retained_report}" ]]; then
  echo "retained SLO evaluation artifact is missing: ${retained_report}"
  exit 1
fi
if [[ ! -s "${retained_staged_report}" ]]; then
  echo "retained staged capacity evaluation artifact is missing: ${retained_staged_report}"
  exit 1
fi

OTEL_EXPORTER_OTLP_ENDPOINT="${collector_endpoint}" \
cargo test --test observability_k8s_slo_baseline --test runtime_observability_instrumentation

echo "observability hard-SLO baseline checks passed"
echo "retained artifacts:"
echo "  - ${retained_summary}"
echo "  - ${retained_report}"
echo "  - ${retained_staged_report}"
