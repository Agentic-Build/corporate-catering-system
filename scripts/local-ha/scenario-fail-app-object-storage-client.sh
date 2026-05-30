#!/usr/bin/env bash
set -euo pipefail

cd "$(dirname "$0")/../.."

namespace="${NAMESPACE:-tbite}"
release="${RELEASE:-tbite}"
deployment="${TARGET_DEPLOYMENT:-${release}-tbite-platform-api}"
container="${TARGET_CONTAINER:-api}"
bad_s3_endpoint="${BAD_S3_ENDPOINT:-http://127.0.0.1:9}"
timeout_seconds="${TIMEOUT_SECONDS:-360}"
poll_seconds="${POLL_SECONDS:-5}"
vm_service="${VM_SERVICE:-vmsingle-${release}-victoria-metrics-k8s-stack}"
vm_url="${VM_URL:-}"
vm_local_port="${VM_LOCAL_PORT:-18428}"
dashboard_file="chart/tbite-platform/dashboards/local-ha-drills.json"
port_forward_pid=""
patched=false
original_s3_present=false
original_s3_value=""
baseline_app_dependency_degraded_seconds=0
baseline_dependency_readiness_changes=0
readonly PANEL_DATA_SERVICE_AVAILABILITY="Data service availability"
readonly PANEL_DEPENDENCY_READINESS_HEALTH="Dependency readiness and scaler health"

float_ge() {
  local left="$1" right="$2"
  awk -v left="$left" -v right="$right" 'BEGIN { exit !(left >= right) }'
}

float_gt() {
  local left="$1" right="$2"
  awk -v left="$left" -v right="$right" 'BEGIN { exit !(left > right) }'
}

float_le() {
  local left="$1" right="$2"
  awk -v left="$left" -v right="$right" 'BEGIN { exit !(left <= right) }'
}

start_vm_port_forward() {
  if [[ -n "${vm_url}" ]]; then
    return 0
  fi

  vm_url="http://127.0.0.1:${vm_local_port}"
  local log_file
  log_file="$(mktemp -t tbite-vm-port-forward.XXXXXX.log)"
  kubectl -n "${namespace}" port-forward "svc/${vm_service}" "${vm_local_port}:8428" >"${log_file}" 2>&1 &
  port_forward_pid="$!"

  local deadline=$((SECONDS + 20))
  while (( SECONDS < deadline )); do
    if ! kill -0 "${port_forward_pid}" >/dev/null 2>&1; then
      cat "${log_file}" >&2 || true
      echo "VictoriaMetrics port-forward exited before becoming ready" >&2
      exit 1
    fi
    if curl -fsS "${vm_url}/health" >/dev/null 2>&1; then
      return 0
    fi
    sleep 1
  done

  cat "${log_file}" >&2 || true
  echo "timed out waiting for VictoriaMetrics port-forward on ${vm_url}" >&2
  exit 1
}

restore_deployment() {
  if [[ "${patched}" != "true" ]]; then
    return 0
  fi

  echo "==> restoring ${deployment} S3_ENDPOINT"
  if [[ "${original_s3_present}" == "true" ]]; then
    kubectl -n "${namespace}" set env "deployment/${deployment}" "S3_ENDPOINT=${original_s3_value}" --containers="${container}" >/dev/null
  else
    kubectl -n "${namespace}" set env "deployment/${deployment}" S3_ENDPOINT- --containers="${container}" >/dev/null
  fi
  kubectl -n "${namespace}" rollout status "deployment/${deployment}" --timeout="${timeout_seconds}s"
  patched=false
}

cleanup() {
  local status=$?
  if [[ -n "${port_forward_pid}" ]]; then
    kill "${port_forward_pid}" >/dev/null 2>&1 || true
  fi
  if [[ "${patched}" == "true" ]]; then
    restore_deployment || true
  fi
  exit "${status}"
}
trap cleanup EXIT

promql_value() {
  local query="$1"
  curl -fsS --get "${vm_url}/api/v1/query" --data-urlencode "query=${query}" \
    | jq -r '.data.result[0].value[1] // empty'
}

dashboard_expr() {
  local panel_title="$1"
  local legend="$2"
  PANEL_TITLE="${panel_title}" \
    LEGEND="${legend}" \
    DASHBOARD_NAMESPACE="${namespace}" \
    DASHBOARD_RELEASE="${release}" \
    DASHBOARD_ENV="${ENVIRONMENT:-local-ha}" \
    DASHBOARD_FILE="${dashboard_file}" \
    node --input-type=module <<'NODE'
import fs from 'node:fs';

const dashboard = JSON.parse(fs.readFileSync(process.env.DASHBOARD_FILE, 'utf8'));
const panel = dashboard.panels.find((candidate) => candidate.title === process.env.PANEL_TITLE);
if (!panel) {
  throw new Error(`dashboard panel not found: ${process.env.PANEL_TITLE}`);
}

const target = panel.targets.find((candidate) => candidate.legendFormat === process.env.LEGEND);
if (!target) {
  throw new Error(`dashboard target not found: ${process.env.PANEL_TITLE} / ${process.env.LEGEND}`);
}

const substitutions = {
  '$namespace': process.env.DASHBOARD_NAMESPACE,
  '$release': process.env.DASHBOARD_RELEASE,
  '$env': process.env.DASHBOARD_ENV,
};

let expr = target.expr;
for (const [placeholder, value] of Object.entries(substitutions)) {
  expr = expr.split(placeholder).join(value);
}

process.stdout.write(expr);
NODE
}

dashboard_target_value() {
  local panel_title="$1"
  local legend="$2"
  promql_value "$(dashboard_expr "${panel_title}" "${legend}")"
}

dashboard_data_database_degraded() {
  dashboard_target_value "${PANEL_DATA_SERVICE_AVAILABILITY}" "database service degraded"
}

dashboard_data_messaging_degraded() {
  dashboard_target_value "${PANEL_DATA_SERVICE_AVAILABILITY}" "messaging service degraded"
}

dashboard_data_cache_degraded() {
  dashboard_target_value "${PANEL_DATA_SERVICE_AVAILABILITY}" "cache service degraded"
}

dashboard_data_object_storage_degraded() {
  dashboard_target_value "${PANEL_DATA_SERVICE_AVAILABILITY}" "object storage service degraded"
}

dashboard_data_app_dependency_clients_degraded() {
  dashboard_target_value "${PANEL_DATA_SERVICE_AVAILABILITY}" "app dependency clients degraded"
}

dashboard_data_app_dependency_clients_degraded_seconds_10m() {
  dashboard_target_value "Data service activity" "app dependency clients degraded seconds / 10m"
}

dashboard_dependency_database_clients_not_ready() {
  dashboard_target_value "${PANEL_DEPENDENCY_READINESS_HEALTH}" "database clients not-ready"
}

dashboard_dependency_messaging_clients_not_ready() {
  dashboard_target_value "${PANEL_DEPENDENCY_READINESS_HEALTH}" "messaging clients not-ready"
}

dashboard_dependency_cache_clients_not_ready() {
  dashboard_target_value "${PANEL_DEPENDENCY_READINESS_HEALTH}" "cache clients not-ready"
}

dashboard_dependency_object_storage_clients_not_ready() {
  dashboard_target_value "${PANEL_DEPENDENCY_READINESS_HEALTH}" "object storage clients not-ready"
}

dashboard_object_storage_dependency_ready_series() {
  promql_value "count(tbite_dependency_ready{dependency=\"object-storage\"}) or vector(0)"
}

dashboard_dependency_readiness_changes_10m() {
  dashboard_target_value "${PANEL_DEPENDENCY_READINESS_HEALTH}" "dependency readiness changes / 10m"
}

dashboard_api_not_ready_pods() {
  promql_value "sum(kube_pod_status_ready{namespace=\"${namespace}\",pod=~\"${deployment}-.*\",condition=\"false\"} == 1) or vector(0)"
}

app_crashloops() {
  kubectl -n "${namespace}" get pods \
    -l "app.kubernetes.io/instance=${release},app.kubernetes.io/name=tbite-platform" \
    -o json \
    | jq -r '
        .items[]
        | .metadata.name as $pod
        | [.status.containerStatuses[]?
           | select((.state.waiting.reason // "") == "CrashLoopBackOff")
           | .name] as $containers
        | select(($containers | length) > 0)
        | [$pod, ($containers | join(","))]
        | @tsv'
}

fail_if_app_crashloops() {
  local crashloops
  crashloops="$(app_crashloops)"
  if [[ -n "${crashloops}" ]]; then
    echo "platform app pods entered CrashLoopBackOff during app dependency client drill:" >&2
    printf 'pod\tcontainers\n' >&2
    printf '%s\n' "${crashloops}" >&2
    exit 1
  fi
}

wait_for_dashboard_at_least() {
  local name="$1"
  local metric_func="$2"
  local threshold="$3"
  local deadline=$((SECONDS + timeout_seconds))
  local current

  while (( SECONDS < deadline )); do
    fail_if_app_crashloops
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

wait_for_dashboard_at_most() {
  local name="$1"
  local metric_func="$2"
  local threshold="$3"
  local deadline=$((SECONDS + timeout_seconds))
  local current

  while (( SECONDS < deadline )); do
    fail_if_app_crashloops
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

wait_for_dashboard_delta_at_least() {
  local name="$1"
  local metric_func="$2"
  local baseline="$3"
  local min_delta="$4"
  local deadline=$((SECONDS + timeout_seconds))
  local current

  while (( SECONDS < deadline )); do
    fail_if_app_crashloops
    current="$("${metric_func}")"
    if [[ -n "${current}" ]] && awk -v current="${current}" -v baseline="${baseline}" -v min_delta="${min_delta}" 'BEGIN { exit !((current - baseline) >= min_delta) }'; then
      printf '%s %s=%s baseline=%s min_delta=%s\n' "$(date -u +%Y-%m-%dT%H:%M:%SZ)" "${name}" "${current}" "${baseline}" "${min_delta}"
      return 0
    fi
    printf '%s %s=%s waiting_for_delta_at_least=%s baseline=%s\n' "$(date -u +%Y-%m-%dT%H:%M:%SZ)" "${name}" "${current:-empty}" "${min_delta}" "${baseline}"
    sleep "${poll_seconds}"
  done

  echo "timed out waiting for dashboard signal ${name} to increase by at least ${min_delta} from baseline ${baseline}" >&2
  return 1
}

wait_for_baseline() {
  start_vm_port_forward
  kubectl -n "${namespace}" rollout status "deployment/${deployment}" --timeout="${timeout_seconds}s"
  wait_for_dashboard_at_least "dashboard_object_storage_dependency_ready_series" dashboard_object_storage_dependency_ready_series 1
  wait_for_dashboard_at_most "dashboard_data_database_degraded" dashboard_data_database_degraded 0
  wait_for_dashboard_at_most "dashboard_data_messaging_degraded" dashboard_data_messaging_degraded 0
  wait_for_dashboard_at_most "dashboard_data_cache_degraded" dashboard_data_cache_degraded 0
  wait_for_dashboard_at_most "dashboard_data_object_storage_degraded" dashboard_data_object_storage_degraded 0
  wait_for_dashboard_at_most "dashboard_data_app_dependency_clients_degraded" dashboard_data_app_dependency_clients_degraded 0
  wait_for_dashboard_at_most "dashboard_dependency_database_clients_not_ready" dashboard_dependency_database_clients_not_ready 0
  wait_for_dashboard_at_most "dashboard_dependency_messaging_clients_not_ready" dashboard_dependency_messaging_clients_not_ready 0
  wait_for_dashboard_at_most "dashboard_dependency_cache_clients_not_ready" dashboard_dependency_cache_clients_not_ready 0
  wait_for_dashboard_at_most "dashboard_dependency_object_storage_clients_not_ready" dashboard_dependency_object_storage_clients_not_ready 0
  baseline_app_dependency_degraded_seconds="$(dashboard_data_app_dependency_clients_degraded_seconds_10m)"
  baseline_app_dependency_degraded_seconds="${baseline_app_dependency_degraded_seconds:-0}"
  baseline_dependency_readiness_changes="$(dashboard_dependency_readiness_changes_10m)"
  baseline_dependency_readiness_changes="${baseline_dependency_readiness_changes:-0}"
  printf 'baseline_dashboard_data_app_dependency_clients_degraded_seconds_10m=%s\n' "${baseline_app_dependency_degraded_seconds}"
  printf 'baseline_dashboard_dependency_readiness_changes_10m=%s\n' "${baseline_dependency_readiness_changes}"
}

capture_original_env() {
  local value
  value="$(
    kubectl -n "${namespace}" get deployment "${deployment}" -o json \
      | jq -r --arg container "${container}" '
          .spec.template.spec.containers[]
          | select(.name == $container)
          | .env[]?
          | select(.name == "S3_ENDPOINT")
          | if has("valueFrom") then "__VALUE_FROM_UNSUPPORTED__" else (.value // "") end
        ' \
      | head -n 1
  )"
  if [[ "${value}" == "__VALUE_FROM_UNSUPPORTED__" ]]; then
    echo "deployment ${namespace}/${deployment} has S3_ENDPOINT valueFrom; refusing to overwrite without an exact restore path" >&2
    exit 1
  fi
  if [[ -n "${value}" ]]; then
    original_s3_present=true
    original_s3_value="${value}"
  fi
}

wait_for_recovery() {
  restore_deployment
  wait_for_dashboard_at_most "dashboard_api_not_ready_pods" dashboard_api_not_ready_pods 0
  wait_for_dashboard_at_most "dashboard_data_database_degraded" dashboard_data_database_degraded 0
  wait_for_dashboard_at_most "dashboard_data_messaging_degraded" dashboard_data_messaging_degraded 0
  wait_for_dashboard_at_most "dashboard_data_cache_degraded" dashboard_data_cache_degraded 0
  wait_for_dashboard_at_most "dashboard_data_object_storage_degraded" dashboard_data_object_storage_degraded 0
  wait_for_dashboard_at_most "dashboard_data_app_dependency_clients_degraded" dashboard_data_app_dependency_clients_degraded 0
  wait_for_dashboard_at_most "dashboard_dependency_database_clients_not_ready" dashboard_dependency_database_clients_not_ready 0
  wait_for_dashboard_at_most "dashboard_dependency_messaging_clients_not_ready" dashboard_dependency_messaging_clients_not_ready 0
  wait_for_dashboard_at_most "dashboard_dependency_cache_clients_not_ready" dashboard_dependency_cache_clients_not_ready 0
  wait_for_dashboard_at_most "dashboard_dependency_object_storage_clients_not_ready" dashboard_dependency_object_storage_clients_not_ready 0
  wait_for_dashboard_delta_at_least "dashboard_data_app_dependency_clients_degraded_seconds_10m" dashboard_data_app_dependency_clients_degraded_seconds_10m "${baseline_app_dependency_degraded_seconds}" 15
  wait_for_dashboard_delta_at_least "dashboard_dependency_readiness_changes_10m" dashboard_dependency_readiness_changes_10m "${baseline_dependency_readiness_changes}" 1
}

capture_original_env
wait_for_baseline

echo "==> patching ${deployment} ${container} S3_ENDPOINT=${bad_s3_endpoint}"
kubectl -n "${namespace}" set env "deployment/${deployment}" "S3_ENDPOINT=${bad_s3_endpoint}" --containers="${container}" >/dev/null
patched=true

wait_for_dashboard_at_least "dashboard_api_not_ready_pods" dashboard_api_not_ready_pods 1
wait_for_dashboard_at_least "dashboard_data_app_dependency_clients_degraded" dashboard_data_app_dependency_clients_degraded 1
wait_for_dashboard_at_least "dashboard_dependency_object_storage_clients_not_ready" dashboard_dependency_object_storage_clients_not_ready 1
wait_for_dashboard_at_most "dashboard_data_database_degraded" dashboard_data_database_degraded 0
wait_for_dashboard_at_most "dashboard_data_messaging_degraded" dashboard_data_messaging_degraded 0
wait_for_dashboard_at_most "dashboard_data_cache_degraded" dashboard_data_cache_degraded 0
wait_for_dashboard_at_most "dashboard_data_object_storage_degraded" dashboard_data_object_storage_degraded 0
wait_for_dashboard_at_most "dashboard_dependency_database_clients_not_ready" dashboard_dependency_database_clients_not_ready 0
wait_for_dashboard_at_most "dashboard_dependency_messaging_clients_not_ready" dashboard_dependency_messaging_clients_not_ready 0
wait_for_dashboard_at_most "dashboard_dependency_cache_clients_not_ready" dashboard_dependency_cache_clients_not_ready 0
printf 'fault_dashboard_api_not_ready_pods=%s\n' "$(dashboard_api_not_ready_pods)"
printf 'fault_dashboard_data_app_dependency_clients_degraded=%s\n' "$(dashboard_data_app_dependency_clients_degraded)"
printf 'fault_dashboard_dependency_database_clients_not_ready=%s\n' "$(dashboard_dependency_database_clients_not_ready)"
printf 'fault_dashboard_dependency_messaging_clients_not_ready=%s\n' "$(dashboard_dependency_messaging_clients_not_ready)"
printf 'fault_dashboard_dependency_cache_clients_not_ready=%s\n' "$(dashboard_dependency_cache_clients_not_ready)"
printf 'fault_dashboard_dependency_object_storage_clients_not_ready=%s\n' "$(dashboard_dependency_object_storage_clients_not_ready)"
printf 'fault_dashboard_data_object_storage_degraded=%s\n' "$(dashboard_data_object_storage_degraded)"

wait_for_recovery
printf 'recovery_dashboard_data_app_dependency_clients_degraded_seconds_10m=%s\n' "$(dashboard_data_app_dependency_clients_degraded_seconds_10m)"
printf 'recovery_dashboard_dependency_readiness_changes_10m=%s\n' "$(dashboard_dependency_readiness_changes_10m)"
kubectl -n "${namespace}" get pods -l "app.kubernetes.io/instance=${release},app.kubernetes.io/name=tbite-platform,app.kubernetes.io/component=api" -o wide
