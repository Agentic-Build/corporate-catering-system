#!/usr/bin/env bash
set -euo pipefail

cd "$(dirname "$0")/../.."

namespace="${NAMESPACE:-tbite}"
release="${RELEASE:-tbite}"
api_service="${API_SERVICE:-svc/${release}-tbite-platform-api}"
db_service="${DB_SERVICE:-svc/${release}-pg-pooler-rw}"
valkey_service="${VALKEY_SERVICE:-svc/tbite-valkey-primary}"
duration="${DURATION:-3m}"
concurrency="${CONCURRENCY:-24}"
total_rps="${TOTAL_RPS:-${RPS:-240}}"
rps_per_worker="${RPS_PER_WORKER:-}"
max_5xx="${MAX_5XX:-0}"
max_net_errors="${MAX_NET_ERRORS:-0}"
scale_timeout_seconds="${SCALE_TIMEOUT_SECONDS:-240}"
baseline_timeout_seconds="${BASELINE_TIMEOUT_SECONDS:-600}"
poll_seconds="${POLL_SECONDS:-5}"
employees="${EMPLOYEES:-200}"
scenario="${SCENARIO:-mixed}"
plants="${PLANTS:-hc-12a-1f,hc-12a-3f,hc-12b-1f,tc-15a-1f,tn-18p1-1f,tn-18p3-1f,tn-18p7-2f}"
min_range_signal_seconds="${MIN_RANGE_SIGNAL_SECONDS:-15}"
hpa="${HPA:-${release}-tbite-platform-api}"
deployment="${DEPLOYMENT:-${release}-tbite-platform-api}"
vm_service="${VM_SERVICE:-vmsingle-${release}-victoria-metrics-k8s-stack}"
vm_url="${VM_URL:-}"
vm_local_port="${VM_LOCAL_PORT:-18428}"

local_api_port="${LOCAL_API_PORT:-18080}"
local_db_port="${LOCAL_DB_PORT:-15432}"
local_valkey_port="${LOCAL_VALKEY_PORT:-16379}"

pids=()
cleanup() {
  if [[ "${#pids[@]}" -gt 0 ]]; then
    kill "${pids[@]}" >/dev/null 2>&1 || true
  fi
}
trap cleanup EXIT

float_gt() {
  awk -v left="$1" -v right="$2" 'BEGIN { exit !(left > right) }'
}

float_ge() {
  awk -v left="$1" -v right="$2" 'BEGIN { exit !(left >= right) }'
}

float_le() {
  awk -v left="$1" -v right="$2" 'BEGIN { exit !(left <= right) }'
}

require_integer() {
  local name="$1"
  local value="$2"
  if [[ -z "${value}" || "${value}" == *[!0-9]* ]]; then
    echo "${name} must be a non-negative integer, got '${value}'" >&2
    exit 2
  fi
}

port_forward() {
  local target="$1"
  local mapping="$2"
  kubectl -n "${namespace}" port-forward "${target}" "${mapping}" >/tmp/tbite-local-ha-port-forward.log 2>&1 &
  pids+=("$!")
}

start_vm_port_forward() {
  if [[ -n "${vm_url}" ]]; then
    return 0
  fi

  vm_url="http://127.0.0.1:${vm_local_port}"
  local log_file
  log_file="$(mktemp -t tbite-vm-port-forward.XXXXXX.log)"
  kubectl -n "${namespace}" port-forward "svc/${vm_service}" "${vm_local_port}:8428" >"${log_file}" 2>&1 &
  local vm_pid="$!"
  pids+=("${vm_pid}")

  local deadline=$((SECONDS + 20))
  while (( SECONDS < deadline )); do
    if curl -fsS "${vm_url}/health" >/dev/null 2>&1; then
      return 0
    fi
    if ! kill -0 "${vm_pid}" >/dev/null 2>&1; then
      cat "${log_file}" >&2 || true
      echo "VictoriaMetrics port-forward exited before becoming ready" >&2
      exit 1
    fi
    sleep 1
  done

  cat "${log_file}" >&2 || true
  echo "timed out waiting for VictoriaMetrics port-forward on ${vm_url}" >&2
  exit 1
}

promql_value() {
  local query="$1"
  curl -fsS --get "${vm_url}/api/v1/query" --data-urlencode "query=${query}" \
    | jq -r '.data.result[0].value[1] // empty'
}

dashboard_hpa_current() {
  promql_value "sum(kube_horizontalpodautoscaler_status_current_replicas{namespace=\"${namespace}\",horizontalpodautoscaler=\"${hpa}\"}) or vector(0)"
}

dashboard_hpa_desired() {
  promql_value "sum(kube_horizontalpodautoscaler_status_desired_replicas{namespace=\"${namespace}\",horizontalpodautoscaler=\"${hpa}\"}) or vector(0)"
}

dashboard_hpa_min() {
  promql_value "sum(kube_horizontalpodautoscaler_spec_min_replicas{namespace=\"${namespace}\",horizontalpodautoscaler=\"${hpa}\"}) or vector(0)"
}

dashboard_hpa_scale_pressure() {
  promql_value "clamp_min((sum(kube_horizontalpodautoscaler_status_desired_replicas{namespace=\"${namespace}\",horizontalpodautoscaler=\"${hpa}\"}) or vector(0)) - (sum(kube_horizontalpodautoscaler_spec_min_replicas{namespace=\"${namespace}\",horizontalpodautoscaler=\"${hpa}\"}) or vector(0)), 0)"
}

dashboard_hpa_current_over_min() {
  promql_value "clamp_min((sum(kube_horizontalpodautoscaler_status_current_replicas{namespace=\"${namespace}\",horizontalpodautoscaler=\"${hpa}\"}) or vector(0)) - (sum(kube_horizontalpodautoscaler_spec_min_replicas{namespace=\"${namespace}\",horizontalpodautoscaler=\"${hpa}\"}) or vector(0)), 0)"
}

dashboard_api_cpu_utilization() {
  promql_value "sum(kube_horizontalpodautoscaler_status_target_metric{namespace=\"${namespace}\",horizontalpodautoscaler=\"${hpa}\",metric_name=\"cpu\",metric_target_type=\"utilization\"}) or vector(0)"
}

dashboard_api_cpu_target() {
  promql_value "sum(kube_horizontalpodautoscaler_spec_target_metric{namespace=\"${namespace}\",horizontalpodautoscaler=\"${hpa}\",metric_name=\"cpu\",metric_target_type=\"utilization\"}) or vector(0)"
}

dashboard_api_cpu_over_target() {
  promql_value "clamp_min((sum(kube_horizontalpodautoscaler_status_target_metric{namespace=\"${namespace}\",horizontalpodautoscaler=\"${hpa}\",metric_name=\"cpu\",metric_target_type=\"utilization\"}) or vector(0)) - (sum(kube_horizontalpodautoscaler_spec_target_metric{namespace=\"${namespace}\",horizontalpodautoscaler=\"${hpa}\",metric_name=\"cpu\",metric_target_type=\"utilization\"}) or vector(0)), 0)"
}

dashboard_api_scale_event_seconds_10m() {
  promql_value "(sum_over_time((((clamp_min((sum(kube_horizontalpodautoscaler_status_desired_replicas{namespace=\"${namespace}\",horizontalpodautoscaler=\"${hpa}\"}) or vector(0)) - (sum(kube_horizontalpodautoscaler_spec_min_replicas{namespace=\"${namespace}\",horizontalpodautoscaler=\"${hpa}\"}) or vector(0)), 0) > bool 0) + (clamp_min((sum(kube_horizontalpodautoscaler_status_current_replicas{namespace=\"${namespace}\",horizontalpodautoscaler=\"${hpa}\"}) or vector(0)) - (sum(kube_horizontalpodautoscaler_spec_min_replicas{namespace=\"${namespace}\",horizontalpodautoscaler=\"${hpa}\"}) or vector(0)), 0) > bool 0) + (clamp_min((sum(kube_horizontalpodautoscaler_status_target_metric{namespace=\"${namespace}\",horizontalpodautoscaler=\"${hpa}\",metric_name=\"cpu\",metric_target_type=\"utilization\"}) or vector(0)) - (sum(kube_horizontalpodautoscaler_spec_target_metric{namespace=\"${namespace}\",horizontalpodautoscaler=\"${hpa}\",metric_name=\"cpu\",metric_target_type=\"utilization\"}) or vector(0)), 0) > bool 0)) > bool 0)[10m:15s]) or vector(0)) * 15"
}

dashboard_bad_scale_conditions() {
  promql_value "sum(kube_horizontalpodautoscaler_status_condition{namespace=\"${namespace}\",horizontalpodautoscaler=~\"(keda-hpa-)?${release}-tbite-platform-.*\",condition=~\"AbleToScale|ScalingActive\",status!=\"true\"} == 1) or vector(0)"
}

dashboard_cpu_hpas_inactive() {
  promql_value "sum(kube_horizontalpodautoscaler_status_condition{namespace=\"${namespace}\",horizontalpodautoscaler=~\"${release}-tbite-platform-(api|realtime|web-.*)\",condition=\"ScalingActive\",status=\"false\"} == 1) or vector(0)"
}

dashboard_metrics_server_missing() {
  promql_value "((max(kube_deployment_status_replicas_available{namespace=\"kube-system\",deployment=\"metrics-server\"}) or vector(0)) < bool 1)"
}

dashboard_api_ready_pods() {
  promql_value "sum(kube_pod_status_ready{namespace=\"${namespace}\",pod=~\"${deployment}-.*\",condition=\"true\"}) or vector(0)"
}

dashboard_api_unavailable_replicas() {
  promql_value "sum(kube_deployment_status_replicas_unavailable{namespace=\"${namespace}\",deployment=\"${deployment}\"}) or vector(0)"
}

wait_for_dashboard_at_least() {
  local name="$1"
  local metric_func="$2"
  local threshold="$3"
  local timeout="$4"
  local deadline=$((SECONDS + timeout))
  local current

  while (( SECONDS < deadline )); do
    current="$("${metric_func}")"
    if [[ -n "${current}" ]] && float_ge "${current}" "${threshold}"; then
      printf '%s %s=%s threshold=%s\n' "$(date -u +%Y-%m-%dT%H:%M:%SZ)" "${name}" "${current}" "${threshold}"
      return 0
    fi
    printf '%s %s=%s waiting_for_at_least=%s\n' "$(date -u +%Y-%m-%dT%H:%M:%SZ)" "${name}" "${current:-empty}" "${threshold}"
    sleep "${poll_seconds}"
  done

  echo "timed out waiting for dashboard signal ${name} >= ${threshold}" >&2
  return 1
}

wait_for_dashboard_greater_than() {
  local name="$1"
  local metric_func="$2"
  local threshold="$3"
  local timeout="$4"
  local deadline=$((SECONDS + timeout))
  local current

  while (( SECONDS < deadline )); do
    current="$("${metric_func}")"
    if [[ -n "${current}" ]] && float_gt "${current}" "${threshold}"; then
      printf '%s %s=%s threshold_gt=%s\n' "$(date -u +%Y-%m-%dT%H:%M:%SZ)" "${name}" "${current}" "${threshold}"
      return 0
    fi
    printf '%s %s=%s waiting_for_greater_than=%s\n' "$(date -u +%Y-%m-%dT%H:%M:%SZ)" "${name}" "${current:-empty}" "${threshold}"
    sleep "${poll_seconds}"
  done

  echo "timed out waiting for dashboard signal ${name} > ${threshold}" >&2
  return 1
}

wait_for_dashboard_at_most() {
  local name="$1"
  local metric_func="$2"
  local threshold="$3"
  local timeout="$4"
  local deadline=$((SECONDS + timeout))
  local current

  while (( SECONDS < deadline )); do
    current="$("${metric_func}")"
    if [[ -n "${current}" ]] && float_le "${current}" "${threshold}"; then
      printf '%s %s=%s threshold=%s\n' "$(date -u +%Y-%m-%dT%H:%M:%SZ)" "${name}" "${current}" "${threshold}"
      return 0
    fi
    printf '%s %s=%s waiting_for_at_most=%s\n' "$(date -u +%Y-%m-%dT%H:%M:%SZ)" "${name}" "${current:-empty}" "${threshold}"
    sleep "${poll_seconds}"
  done

  echo "timed out waiting for dashboard signal ${name} <= ${threshold}" >&2
  return 1
}

wait_for_dashboard_baseline() {
  start_vm_port_forward
  wait_for_dashboard_at_most "dashboard_api_scale_pressure" dashboard_hpa_scale_pressure 0 "${baseline_timeout_seconds}"
  wait_for_dashboard_at_most "dashboard_api_unavailable_replicas" dashboard_api_unavailable_replicas 0 "${baseline_timeout_seconds}"
  wait_for_dashboard_at_least "dashboard_api_ready_pods" dashboard_api_ready_pods "${min_replicas}" "${baseline_timeout_seconds}"
  wait_for_dashboard_at_most "dashboard_metrics_server_missing" dashboard_metrics_server_missing 0 "${baseline_timeout_seconds}"
  wait_for_dashboard_at_most "dashboard_cpu_hpas_inactive" dashboard_cpu_hpas_inactive 0 "${baseline_timeout_seconds}"
  wait_for_dashboard_at_most "dashboard_bad_scale_conditions" dashboard_bad_scale_conditions 0 "${baseline_timeout_seconds}"
}

wait_for_dashboard_scale_up() {
  start_vm_port_forward
  local target
  target="$(dashboard_api_cpu_target)"
  wait_for_dashboard_greater_than "dashboard_api_cpu_utilization" dashboard_api_cpu_utilization "${target}" "${scale_timeout_seconds}"
  wait_for_dashboard_greater_than "dashboard_api_cpu_over_target" dashboard_api_cpu_over_target 0 "${scale_timeout_seconds}"
  wait_for_dashboard_greater_than "dashboard_api_scale_pressure" dashboard_hpa_scale_pressure 0 "${scale_timeout_seconds}"
  wait_for_dashboard_at_least "dashboard_api_scale_event_seconds_10m" dashboard_api_scale_event_seconds_10m "${min_range_signal_seconds}" "${scale_timeout_seconds}"
  wait_for_dashboard_at_most "dashboard_metrics_server_missing" dashboard_metrics_server_missing 0 "${scale_timeout_seconds}"
  wait_for_dashboard_at_most "dashboard_cpu_hpas_inactive" dashboard_cpu_hpas_inactive 0 "${scale_timeout_seconds}"
  wait_for_dashboard_at_most "dashboard_bad_scale_conditions" dashboard_bad_scale_conditions 0 "${scale_timeout_seconds}"
}

wait_for_dashboard_floor() {
  start_vm_port_forward
  wait_for_dashboard_at_most "dashboard_api_scale_pressure" dashboard_hpa_scale_pressure 0 "${baseline_timeout_seconds}"
  wait_for_dashboard_at_most "dashboard_api_current_over_min" dashboard_hpa_current_over_min 0 "${baseline_timeout_seconds}"
  wait_for_dashboard_at_most "dashboard_api_desired" dashboard_hpa_desired "${min_replicas}" "${baseline_timeout_seconds}"
  wait_for_dashboard_at_most "dashboard_api_current" dashboard_hpa_current "${min_replicas}" "${baseline_timeout_seconds}"
  wait_for_dashboard_at_least "dashboard_api_ready_pods" dashboard_api_ready_pods "${min_replicas}" "${baseline_timeout_seconds}"
  wait_for_dashboard_at_most "dashboard_metrics_server_missing" dashboard_metrics_server_missing 0 "${baseline_timeout_seconds}"
  wait_for_dashboard_at_most "dashboard_cpu_hpas_inactive" dashboard_cpu_hpas_inactive 0 "${baseline_timeout_seconds}"
  wait_for_dashboard_at_most "dashboard_bad_scale_conditions" dashboard_bad_scale_conditions 0 "${baseline_timeout_seconds}"
}

require_workload_seed() {
  local primary counts matching_vendors future_supply
  primary="$(kubectl -n "${namespace}" get cluster tbite-pg -o jsonpath='{.status.currentPrimary}')"
  if [[ -z "${primary}" ]]; then
    echo "CNPG cluster tbite-pg does not report a current primary." >&2
    exit 1
  fi

  counts="$(kubectl -n "${namespace}" exec -i "${primary}" -c postgres -- \
    psql -q -d tbite -At -v ON_ERROR_STOP=1 -v plants="${plants}" <<'SQL'
WITH requested_plants AS (
  SELECT trim(value) AS plant
  FROM regexp_split_to_table(:'plants', ',') AS value
)
SELECT count(DISTINCT mi.vendor_id)
FROM menu_item mi
JOIN vendor v ON v.id = mi.vendor_id AND v.status = 'approved'
JOIN vendor_plant_mapping vpm ON vpm.vendor_id = mi.vendor_id AND vpm.active
JOIN requested_plants rp ON rp.plant = vpm.plant
WHERE mi.status = 'active';

SELECT count(*)
FROM meal_supply ms
JOIN menu_item mi ON mi.id = ms.menu_item_id AND mi.status = 'active'
WHERE ms.supply_date BETWEEN CURRENT_DATE + INTERVAL '1 day' AND CURRENT_DATE + INTERVAL '7 days';
SQL
)"
  matching_vendors="$(printf '%s\n' "${counts}" | sed -n '1p')"
  future_supply="$(printf '%s\n' "${counts}" | sed -n '2p')"
  if [[ -z "${matching_vendors}" || -z "${future_supply}" || "${matching_vendors}" -lt 1 || "${future_supply}" -lt 1 ]]; then
    echo "Local HA workload seed is missing for PLANTS=${plants}." >&2
    echo "Run: make local-ha-seed" >&2
    exit 1
  fi
  echo "workload seed: matching_vendors=${matching_vendors} future_supply_rows=${future_supply}"
}

print_sample() {
  local current desired cpu target ready unavailable
  current="$(kubectl -n "${namespace}" get hpa "${hpa}" -o jsonpath='{.status.currentReplicas}' 2>/dev/null || true)"
  desired="$(kubectl -n "${namespace}" get hpa "${hpa}" -o jsonpath='{.status.desiredReplicas}' 2>/dev/null || true)"
  cpu="$(kubectl -n "${namespace}" get hpa "${hpa}" -o jsonpath='{.status.currentMetrics[0].resource.current.averageUtilization}' 2>/dev/null || true)"
  target="$(kubectl -n "${namespace}" get hpa "${hpa}" -o jsonpath='{.spec.metrics[0].resource.target.averageUtilization}' 2>/dev/null || true)"
  ready="$(kubectl -n "${namespace}" get deployment "${deployment}" -o jsonpath='{.status.readyReplicas}' 2>/dev/null || true)"
  unavailable="$(kubectl -n "${namespace}" get deployment "${deployment}" -o jsonpath='{.status.unavailableReplicas}' 2>/dev/null || true)"
  printf '%s hpa_current=%s hpa_desired=%s hpa_cpu=%s hpa_target=%s ready=%s unavailable=%s\n' \
    "$(date -u +%Y-%m-%dT%H:%M:%SZ)" "${current:-unknown}" "${desired:-unknown}" "${cpu:-unknown}" "${target:-unknown}" "${ready:-0}" "${unavailable:-0}"
}

wait_for_hpa_floor() {
  local deadline current desired
  deadline=$((SECONDS + baseline_timeout_seconds))
  while true; do
    current="$(kubectl -n "${namespace}" get hpa "${hpa}" -o jsonpath='{.status.currentReplicas}' 2>/dev/null || echo 0)"
    desired="$(kubectl -n "${namespace}" get hpa "${hpa}" -o jsonpath='{.status.desiredReplicas}' 2>/dev/null || echo 0)"
    if (( current <= min_replicas && desired <= min_replicas )); then
      break
    fi
    print_sample
    if (( SECONDS > deadline )); then
      echo "timed out waiting for ${hpa} to return to minReplicas=${min_replicas}" >&2
      exit 1
    fi
    sleep "${poll_seconds}"
  done
}

database_url="$(kubectl -n "${namespace}" get secret tbite-db -o jsonpath='{.data.rwUrl}' | base64 -d)"
valkey_password="$(kubectl -n "${namespace}" get secret tbite-valkey -o jsonpath='{.data.password}' | base64 -d)"

require_integer SCALE_TIMEOUT_SECONDS "${scale_timeout_seconds}"
require_integer BASELINE_TIMEOUT_SECONDS "${baseline_timeout_seconds}"
require_integer POLL_SECONDS "${poll_seconds}"
require_integer MAX_5XX "${max_5xx}"
require_integer MAX_NET_ERRORS "${max_net_errors}"
require_integer MIN_RANGE_SIGNAL_SECONDS "${min_range_signal_seconds}"
if (( concurrency < 1 )); then
  echo "CONCURRENCY must be at least 1" >&2
  exit 1
fi
if [[ -z "${rps_per_worker}" ]]; then
  rps_per_worker="$(awk -v total="${total_rps}" -v workers="${concurrency}" 'BEGIN {
    if (total <= 0 || workers <= 0) exit 1
    printf "%.4f", total / workers
  }')"
fi

require_workload_seed
min_replicas="$(kubectl -n "${namespace}" get hpa "${hpa}" -o jsonpath='{.spec.minReplicas}')"
require_integer minReplicas "${min_replicas}"
wait_for_hpa_floor
wait_for_dashboard_baseline

port_forward "${api_service}" "${local_api_port}:80"
port_forward "${db_service}" "${local_db_port}:5432"
port_forward "${valkey_service}" "${local_valkey_port}:6379"
sleep 3

local_database_url="$(printf '%s' "${database_url}" | sed -E "s#@[^/@?]+(:[0-9]+)?/#@127.0.0.1:${local_db_port}/#")"
local_redis_url="redis://:${valkey_password}@127.0.0.1:${local_valkey_port}/0"

echo "==> baseline"
kubectl -n "${namespace}" get hpa
kubectl -n "${namespace}" get pods -l app.kubernetes.io/component=api -o wide
echo "load target: total_rps=${total_rps} concurrency=${concurrency} rps_per_worker=${rps_per_worker}"

go run ./services/api/cmd/stress \
  --base-url="http://127.0.0.1:${local_api_port}" \
  --db="${local_database_url}" \
  --redis="${local_redis_url}" \
  --scenario="${scenario}" \
  --duration="${duration}" \
  --rps="${rps_per_worker}" \
  --concurrency="${concurrency}" \
  --employees="${employees}" \
  --plants="${plants}" \
  --max-5xx="${max_5xx}" \
  --max-net-errors="${max_net_errors}" \
  --quiet &
stress_pid="$!"
scaled=false
dashboard_scaled=false

while kill -0 "${stress_pid}" >/dev/null 2>&1; do
  print_sample
  kubectl -n "${namespace}" get hpa "${hpa}"
  kubectl -n "${namespace}" get pods -l app.kubernetes.io/component=api -o wide
  desired="$(kubectl -n "${namespace}" get hpa "${hpa}" -o jsonpath='{.status.desiredReplicas}' 2>/dev/null || echo 0)"
  if (( desired > min_replicas )); then
    scaled=true
    if [[ "${dashboard_scaled}" != "true" ]]; then
      wait_for_dashboard_scale_up
      dashboard_scaled=true
    fi
  fi
  sleep "${WATCH_INTERVAL:-15}"
done

stress_status=0
wait "${stress_pid}" || stress_status="$?"

if [[ "${scaled}" != "true" ]]; then
  kubectl -n "${namespace}" describe hpa "${hpa}" >&2 || true
  echo "${hpa} did not scale above minReplicas=${min_replicas}" >&2
  exit 1
fi
if [[ "${dashboard_scaled}" != "true" ]]; then
  wait_for_dashboard_scale_up
fi
if [[ "${stress_status}" != "0" ]]; then
  exit "${stress_status}"
fi

echo "==> waiting for API HPA to return to min=${min_replicas}"
wait_for_hpa_floor
wait_for_dashboard_floor

echo "==> final"
print_sample
printf 'api_scale_event_seconds_10m=%s\n' "$(dashboard_api_scale_event_seconds_10m)"
kubectl -n "${namespace}" get hpa "${hpa}"
kubectl -n "${namespace}" get pods -l app.kubernetes.io/component=api -o wide
