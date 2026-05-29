#!/usr/bin/env bash
set -euo pipefail

cd "$(dirname "$0")/../.."

namespace="${NAMESPACE:-tbite}"
release="${RELEASE:-tbite}"
cluster="${CNPG_CLUSTER:-${release}-pg}"
pooler="${CNPG_POOLER:-${cluster}-pooler-rw}"
database_rw_url_secret="${DATABASE_RW_URL_SECRET:-${release}-db}"
database_rw_url_key="${DATABASE_RW_URL_KEY:-rwUrl}"
timeout_seconds="${FAILOVER_TIMEOUT_SECONDS:-360}"
poll_seconds="${POLL_SECONDS:-5}"
min_range_signal_seconds="${MIN_RANGE_SIGNAL_SECONDS:-15}"
vm_service="${VM_SERVICE:-vmsingle-${release}-victoria-metrics-k8s-stack}"
vm_url="${VM_URL:-}"
vm_local_port="${VM_LOCAL_PORT:-18428}"
max_replication_lag="${MAX_REPLICATION_LAG:-5}"
port_forward_pid=""
pooler_patched=false
original_pooler_instances=""
baseline_pooler_degraded_seconds=0
baseline_database_degraded_seconds=0
baseline_app_dependency_clients_degraded_seconds=0

cleanup() {
  if [[ "${pooler_patched}" == "true" && -n "${original_pooler_instances}" ]]; then
    kubectl -n "${namespace}" patch pooler "${pooler}" --type=merge \
      -p "{\"spec\":{\"instances\":${original_pooler_instances}}}" >/dev/null 2>&1 || true
  fi
  if [[ -n "${port_forward_pid}" ]]; then
    kill "${port_forward_pid}" >/dev/null 2>&1 || true
  fi
}
trap cleanup EXIT

float_ge() {
  awk -v left="$1" -v right="$2" 'BEGIN { exit !(left >= right) }'
}

float_le() {
  awk -v left="$1" -v right="$2" 'BEGIN { exit !(left <= right) }'
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

promql_value() {
  local query="$1"
  curl -fsS --get "${vm_url}/api/v1/query" --data-urlencode "query=${query}" \
    | jq -r '.data.result[0].value[1] // empty'
}

dashboard_expr() {
  local panel_title="$1"
  local legend="$2"
  PANEL_TITLE="${panel_title}" LEGEND="${legend}" NAMESPACE="${namespace}" RELEASE="${release}" ENV_NAME="${ENV_NAME:-local-ha}" node --input-type=module <<'NODE'
import fs from "node:fs";

const dashboard = JSON.parse(fs.readFileSync("chart/tbite-platform/dashboards/local-ha-drills.json", "utf8"));
const panel = dashboard.panels?.find((candidate) => candidate.title === process.env.PANEL_TITLE);
const target = panel?.targets?.find((candidate) => candidate.legendFormat === process.env.LEGEND);
if (!target?.expr) {
  throw new Error(`dashboard target not found: ${process.env.PANEL_TITLE} / ${process.env.LEGEND}`);
}
console.log(
  target.expr
    .replace(/\$namespace\b/g, process.env.NAMESPACE)
    .replace(/\$release\b/g, process.env.RELEASE)
    .replace(/\$env\b/g, process.env.ENV_NAME),
);
NODE
}

dashboard_target_value() {
  local panel_title="$1"
  local legend="$2"
  promql_value "$(dashboard_expr "${panel_title}" "${legend}")"
}

pooler_unhealthy_query() {
  cat <<QUERY
(((sum(kube_deployment_status_replicas_available{namespace="${namespace}",deployment="${pooler}"}) or vector(0)) < bool ${original_pooler_instances}) + ((sum(kube_pod_status_ready{namespace="${namespace}",pod=~"${pooler}-.*",condition="true"}) or vector(0)) < bool ${original_pooler_instances}) + ((sum(kube_endpointslice_endpoints{namespace="${namespace}",endpointslice=~"${pooler}-.*",ready="true"}) or vector(0)) < bool ${original_pooler_instances}))
QUERY
}

dashboard_pooler_available_replicas() {
  promql_value "sum(kube_deployment_status_replicas_available{namespace=\"${namespace}\",deployment=\"${pooler}\"}) or vector(0)"
}

dashboard_pooler_ready_pods() {
  promql_value "sum(kube_pod_status_ready{namespace=\"${namespace}\",pod=~\"${pooler}-.*\",condition=\"true\"}) or vector(0)"
}

dashboard_pooler_ready_endpoints() {
  promql_value "sum(kube_endpointslice_endpoints{namespace=\"${namespace}\",endpointslice=~\"${pooler}-.*\",ready=\"true\"}) or vector(0)"
}

dashboard_pooler_unhealthy() {
  promql_value "$(pooler_unhealthy_query)"
}

dashboard_pooler_degraded_seconds_10m() {
  promql_value "(sum_over_time((($(pooler_unhealthy_query)) > bool 0)[10m:15s]) or vector(0)) * 15"
}

dashboard_cnpg_unhealthy() {
  promql_value "(((sum(cnpg_collector_up{namespace=\"${namespace}\",cluster=\"${cluster}\"}) or vector(0)) < bool ${instances}) + ((sum(1 - cnpg_pg_replication_in_recovery{namespace=\"${namespace}\",pod=~\"${cluster}-[0-9]+\"}) or vector(0)) != bool 1) + ((max(cnpg_pg_replication_lag{namespace=\"${namespace}\",pod=~\"${cluster}-[0-9]+\"}) or vector(0)) > bool ${max_replication_lag}) + ((sum(kube_pod_status_ready{namespace=\"${namespace}\",pod=~\"${cluster}-[0-9]+\",condition=\"true\"}) or vector(0)) < bool ${instances}) + ((sum(kube_endpointslice_endpoints{namespace=\"${namespace}\",endpointslice=~\"${cluster}-rw-.*\",ready=\"true\"}) or vector(0)) != bool 1) + ((sum(label_replace(kube_endpointslice_endpoints{namespace=\"${namespace}\",endpointslice=~\"${cluster}-rw-.*\",ready=\"true\"}, \"pod\", \"\$1\", \"targetref_name\", \"(.*)\") and on(namespace,pod) ((1 - cnpg_pg_replication_in_recovery{namespace=\"${namespace}\",pod=~\"${cluster}-[0-9]+\"}) == 1)) or vector(0)) != bool 1))"
}

dashboard_db_client_not_ready_pods() {
  promql_value "sum(kube_pod_status_ready{namespace=\"${namespace}\",pod=~\"${release}-tbite-platform-(api|realtime|worker-outbox-relay|worker-payroll-settler|worker-on-time-evaluator|scheduler-cutoff|scheduler-no-show|scheduler-doc-expiry|scheduler-feedback).*\",condition=\"false\"} == 1) or vector(0)"
}

dashboard_database_degraded() {
  dashboard_target_value "Data service availability" "database service degraded"
}

dashboard_messaging_degraded() {
  dashboard_target_value "Data service availability" "messaging service degraded"
}

dashboard_cache_degraded() {
  dashboard_target_value "Data service availability" "cache service degraded"
}

dashboard_object_storage_degraded() {
  dashboard_target_value "Data service availability" "object storage service degraded"
}

dashboard_app_dependency_clients_degraded() {
  dashboard_target_value "Data service availability" "app dependency clients degraded"
}

dashboard_database_degraded_seconds_10m() {
  dashboard_target_value "Data service activity" "database service degraded seconds / 10m"
}

dashboard_app_dependency_clients_degraded_seconds_10m() {
  dashboard_target_value "Data service activity" "app dependency clients degraded seconds / 10m"
}

dashboard_bad_scale_conditions() {
  promql_value "sum(kube_horizontalpodautoscaler_status_condition{namespace=\"${namespace}\",horizontalpodautoscaler=~\"(keda-hpa-)?${release}-tbite-platform-.*\",condition=~\"AbleToScale|ScalingActive\",status!=\"true\"} == 1) or vector(0)"
}

wait_for_dashboard_at_least() {
  local name="$1"
  local metric_func="$2"
  local threshold="$3"
  local deadline=$((SECONDS + timeout_seconds))
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

wait_for_dashboard_at_most() {
  local name="$1"
  local metric_func="$2"
  local threshold="$3"
  local deadline=$((SECONDS + timeout_seconds))
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

wait_for_dashboard_delta_at_least() {
  local name="$1"
  local metric_func="$2"
  local baseline="$3"
  local min_delta="$4"
  local deadline=$((SECONDS + timeout_seconds))
  local current

  while (( SECONDS < deadline )); do
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

database_rw_url() {
  local encoded
  encoded="$(kubectl -n "${namespace}" get secret "${database_rw_url_secret}" -o json \
    | jq -r --arg key "${database_rw_url_key}" '.data[$key] // empty')"
  if [[ -z "${encoded}" ]]; then
    echo "secret ${namespace}/${database_rw_url_secret} does not contain key ${database_rw_url_key}" >&2
    exit 1
  fi
  printf '%s' "${encoded}" | base64 -d
}

database_direct_rw_url() {
  kubectl -n "${namespace}" get secret "${cluster}-app" -o jsonpath='{.data.uri}' | base64 -d
}

with_connect_timeout() {
  local url="$1"
  if [[ "${url}" == *"?"* ]]; then
    printf '%s&connect_timeout=3' "${url}"
  else
    printf '%s?connect_timeout=3' "${url}"
  fi
}

choose_probe_pod() {
  kubectl -n "${namespace}" get pods -l "cnpg.io/cluster=${cluster},cnpg.io/podRole=instance" -o json \
    | jq -r '
        .items
        | map(select(.status.phase == "Running"))
        | map(select([.status.conditions[]? | select(.type == "Ready" and .status == "True")] | length > 0))
        | sort_by(.metadata.creationTimestamp)
        | .[0].metadata.name // empty
      '
}

psql_probe() {
  local url="$1"
  local phase="$2"
  local probe_pod
  probe_pod="$(choose_probe_pod)"
  if [[ -z "${probe_pod}" ]]; then
    echo "could not find a Ready CNPG instance pod to run the Postgres probe" >&2
    exit 1
  fi

  kubectl -n "${namespace}" exec "${probe_pod}" -c postgres -- env DATABASE_URL="$(with_connect_timeout "${url}")" PHASE="${phase}" sh -lc '
    set -e
    got="$(psql -X -qAt -v ON_ERROR_STOP=1 "${DATABASE_URL}" -c "select 1" | tail -n 1 | tr -d "\r")"
    if [ "${got}" != "1" ]; then
      echo "Postgres probe failed during ${PHASE}: got ${got}, want 1" >&2
      exit 1
    fi
    echo "postgres_probe_${PHASE}=ok probe_pod=${HOSTNAME}"
  '
}

psql_probe_must_fail() {
  local url="$1"
  local phase="$2"
  local probe_pod output status
  probe_pod="$(choose_probe_pod)"
  if [[ -z "${probe_pod}" ]]; then
    echo "could not find a Ready CNPG instance pod to run the Postgres probe" >&2
    exit 1
  fi

  set +e
  output="$(
    kubectl -n "${namespace}" exec "${probe_pod}" -c postgres -- env DATABASE_URL="$(with_connect_timeout "${url}")" PHASE="${phase}" sh -lc '
      psql -X -qAt -v ON_ERROR_STOP=1 "${DATABASE_URL}" -c "select 1"
    ' 2>&1
  )"
  status=$?
  set -e
  if [[ "${status}" -eq 0 ]]; then
    echo "expected pooler Postgres probe to fail during ${phase}, but it succeeded" >&2
    printf '%s\n' "${output}" >&2
    exit 1
  fi
  printf 'postgres_probe_%s=failed_as_expected status=%s\n' "${phase}" "${status}"
}

wait_for_dashboard_baseline() {
  start_vm_port_forward
  wait_for_dashboard_at_least "dashboard_pooler_available_replicas" dashboard_pooler_available_replicas "${original_pooler_instances}"
  wait_for_dashboard_at_least "dashboard_pooler_ready_pods" dashboard_pooler_ready_pods "${original_pooler_instances}"
  wait_for_dashboard_at_least "dashboard_pooler_ready_endpoints" dashboard_pooler_ready_endpoints "${original_pooler_instances}"
  wait_for_dashboard_at_most "dashboard_pooler_unhealthy" dashboard_pooler_unhealthy 0
  wait_for_dashboard_at_most "dashboard_cnpg_unhealthy" dashboard_cnpg_unhealthy 0
  wait_for_dashboard_at_most "dashboard_db_client_not_ready_pods" dashboard_db_client_not_ready_pods 0
  wait_for_dashboard_at_most "dashboard_database_degraded" dashboard_database_degraded 0
  wait_for_dashboard_at_most "dashboard_messaging_degraded" dashboard_messaging_degraded 0
  wait_for_dashboard_at_most "dashboard_cache_degraded" dashboard_cache_degraded 0
  wait_for_dashboard_at_most "dashboard_object_storage_degraded" dashboard_object_storage_degraded 0
  wait_for_dashboard_at_most "dashboard_app_dependency_clients_degraded" dashboard_app_dependency_clients_degraded 0

  baseline_pooler_degraded_seconds="$(dashboard_pooler_degraded_seconds_10m)"
  baseline_pooler_degraded_seconds="${baseline_pooler_degraded_seconds:-0}"
  baseline_database_degraded_seconds="$(dashboard_database_degraded_seconds_10m)"
  baseline_database_degraded_seconds="${baseline_database_degraded_seconds:-0}"
  baseline_app_dependency_clients_degraded_seconds="$(dashboard_app_dependency_clients_degraded_seconds_10m)"
  baseline_app_dependency_clients_degraded_seconds="${baseline_app_dependency_clients_degraded_seconds:-0}"
  printf 'baseline_dashboard_pooler_degraded_seconds_10m=%s\n' "${baseline_pooler_degraded_seconds}"
  printf 'baseline_dashboard_database_degraded_seconds_10m=%s\n' "${baseline_database_degraded_seconds}"
  printf 'baseline_dashboard_app_dependency_clients_degraded_seconds_10m=%s\n' "${baseline_app_dependency_clients_degraded_seconds}"
}

wait_for_dashboard_fault() {
  wait_for_dashboard_at_least "dashboard_pooler_unhealthy" dashboard_pooler_unhealthy 1
  wait_for_dashboard_at_most "dashboard_pooler_available_replicas" dashboard_pooler_available_replicas 0
  wait_for_dashboard_at_most "dashboard_pooler_ready_pods" dashboard_pooler_ready_pods 0
  wait_for_dashboard_at_most "dashboard_pooler_ready_endpoints" dashboard_pooler_ready_endpoints 0
  wait_for_dashboard_delta_at_least "dashboard_pooler_degraded_seconds_10m" dashboard_pooler_degraded_seconds_10m "${baseline_pooler_degraded_seconds}" "${min_range_signal_seconds}"
  wait_for_dashboard_at_most "dashboard_cnpg_unhealthy" dashboard_cnpg_unhealthy 0
  wait_for_dashboard_at_least "dashboard_db_client_not_ready_pods" dashboard_db_client_not_ready_pods 1
  wait_for_dashboard_at_least "dashboard_database_degraded" dashboard_database_degraded 1
  wait_for_dashboard_delta_at_least "dashboard_database_degraded_seconds_10m" dashboard_database_degraded_seconds_10m "${baseline_database_degraded_seconds}" "${min_range_signal_seconds}"
  wait_for_dashboard_at_least "dashboard_app_dependency_clients_degraded" dashboard_app_dependency_clients_degraded 1
  wait_for_dashboard_delta_at_least "dashboard_app_dependency_clients_degraded_seconds_10m" dashboard_app_dependency_clients_degraded_seconds_10m "${baseline_app_dependency_clients_degraded_seconds}" "${min_range_signal_seconds}"
  wait_for_dashboard_at_most "dashboard_messaging_degraded" dashboard_messaging_degraded 0
  wait_for_dashboard_at_most "dashboard_cache_degraded" dashboard_cache_degraded 0
  wait_for_dashboard_at_most "dashboard_object_storage_degraded" dashboard_object_storage_degraded 0
}

wait_for_dashboard_recovery() {
  wait_for_dashboard_at_least "dashboard_pooler_available_replicas" dashboard_pooler_available_replicas "${original_pooler_instances}"
  wait_for_dashboard_at_least "dashboard_pooler_ready_pods" dashboard_pooler_ready_pods "${original_pooler_instances}"
  wait_for_dashboard_at_least "dashboard_pooler_ready_endpoints" dashboard_pooler_ready_endpoints "${original_pooler_instances}"
  wait_for_dashboard_at_most "dashboard_pooler_unhealthy" dashboard_pooler_unhealthy 0
  wait_for_dashboard_at_most "dashboard_cnpg_unhealthy" dashboard_cnpg_unhealthy 0
  wait_for_dashboard_at_most "dashboard_db_client_not_ready_pods" dashboard_db_client_not_ready_pods 0
  wait_for_dashboard_at_most "dashboard_database_degraded" dashboard_database_degraded 0
  wait_for_dashboard_at_most "dashboard_messaging_degraded" dashboard_messaging_degraded 0
  wait_for_dashboard_at_most "dashboard_cache_degraded" dashboard_cache_degraded 0
  wait_for_dashboard_at_most "dashboard_object_storage_degraded" dashboard_object_storage_degraded 0
  wait_for_dashboard_at_most "dashboard_app_dependency_clients_degraded" dashboard_app_dependency_clients_degraded 0
}

instances="$(kubectl -n "${namespace}" get cluster "${cluster}" -o jsonpath='{.spec.instances}')"
original_pooler_instances="$(kubectl -n "${namespace}" get pooler "${pooler}" -o jsonpath='{.spec.instances}')"
if [[ -z "${original_pooler_instances}" || "${original_pooler_instances}" -lt 1 ]]; then
  echo "pooler ${namespace}/${pooler} must have spec.instances >= 1" >&2
  exit 2
fi

rw_url="$(database_rw_url)"
if [[ "${rw_url}" != *"@${pooler}.${namespace}"* && "${rw_url}" != *"@${pooler}.${namespace}.svc"* ]]; then
  echo "${database_rw_url_secret}.${database_rw_url_key} must point at ${pooler}; got ${rw_url}" >&2
  exit 2
fi
direct_rw_url="$(database_direct_rw_url)"

wait_for_dashboard_baseline
psql_probe "${rw_url}" "pooler_baseline"
psql_probe "${direct_rw_url}" "direct_baseline"

echo "==> scaling CNPG pooler ${pooler} to zero"
kubectl -n "${namespace}" patch pooler "${pooler}" --type=merge -p '{"spec":{"instances":0}}'
pooler_patched=true

wait_for_dashboard_fault
printf 'fault_dashboard_pooler_unhealthy=%s\n' "$(dashboard_pooler_unhealthy)"
printf 'fault_dashboard_pooler_available_replicas=%s\n' "$(dashboard_pooler_available_replicas)"
printf 'fault_dashboard_pooler_ready_pods=%s\n' "$(dashboard_pooler_ready_pods)"
printf 'fault_dashboard_pooler_ready_endpoints=%s\n' "$(dashboard_pooler_ready_endpoints)"
printf 'fault_dashboard_cnpg_unhealthy=%s\n' "$(dashboard_cnpg_unhealthy)"
printf 'fault_dashboard_db_client_not_ready_pods=%s\n' "$(dashboard_db_client_not_ready_pods)"
printf 'fault_dashboard_database_degraded=%s\n' "$(dashboard_database_degraded)"
printf 'fault_dashboard_messaging_degraded=%s\n' "$(dashboard_messaging_degraded)"
printf 'fault_dashboard_cache_degraded=%s\n' "$(dashboard_cache_degraded)"
printf 'fault_dashboard_object_storage_degraded=%s\n' "$(dashboard_object_storage_degraded)"
printf 'fault_dashboard_app_dependency_clients_degraded=%s\n' "$(dashboard_app_dependency_clients_degraded)"
printf 'fault_dashboard_bad_scale_conditions=%s\n' "$(dashboard_bad_scale_conditions)"
psql_probe_must_fail "${rw_url}" "pooler_fault"
psql_probe "${direct_rw_url}" "direct_during_pooler_fault"

echo "==> restoring CNPG pooler ${pooler} to ${original_pooler_instances} instances"
kubectl -n "${namespace}" patch pooler "${pooler}" --type=merge -p "{\"spec\":{\"instances\":${original_pooler_instances}}}"
pooler_patched=false
kubectl -n "${namespace}" rollout status "deployment/${pooler}" --timeout="${timeout_seconds}s"

wait_for_dashboard_recovery
psql_probe "${rw_url}" "pooler_recovery"
printf 'dashboard_pooler_degraded_seconds_10m=%s\n' "$(dashboard_pooler_degraded_seconds_10m)"
printf 'dashboard_database_degraded_seconds_10m=%s\n' "$(dashboard_database_degraded_seconds_10m)"
printf 'dashboard_database_degraded=%s\n' "$(dashboard_database_degraded)"
printf 'dashboard_messaging_degraded=%s\n' "$(dashboard_messaging_degraded)"
printf 'dashboard_cache_degraded=%s\n' "$(dashboard_cache_degraded)"
printf 'dashboard_object_storage_degraded=%s\n' "$(dashboard_object_storage_degraded)"
printf 'dashboard_app_dependency_clients_degraded=%s\n' "$(dashboard_app_dependency_clients_degraded)"
printf 'dashboard_app_dependency_clients_degraded_seconds_10m=%s\n' "$(dashboard_app_dependency_clients_degraded_seconds_10m)"
kubectl -n "${namespace}" get pooler "${pooler}" -o wide
kubectl -n "${namespace}" get deploy "${pooler}" -o wide
kubectl -n "${namespace}" get pods -l "cnpg.io/poolerName=${pooler}" -o wide
