#!/usr/bin/env bash
set -euo pipefail

cd "$(dirname "$0")/../.."

namespace="${NAMESPACE:-tbite}"
release="${RELEASE:-tbite}"
statefulset="${VALKEY_STATEFULSET:-${release}-valkey-node}"
valkey_secret="${VALKEY_SECRET:-${release}-valkey}"
primary_service="${VALKEY_PRIMARY_SERVICE:-${release}-valkey-primary}"
pod="${POD:-}"
target_role="${TARGET_ROLE:-master}"
timeout="${TIMEOUT:-5m}"
poll_seconds="${POLL_SECONDS:-5}"
vm_service="${VM_SERVICE:-vmsingle-${release}-victoria-metrics-k8s-stack}"
vm_url="${VM_URL:-}"
vm_local_port="${VM_LOCAL_PORT:-18428}"
max_scrape_errors="${MAX_VALKEY_SCRAPE_ERRORS:-0}"
cache_client_regex="${CACHE_CLIENT_COMPONENT_REGEX:-api|realtime}"
app_component_regex="${APP_COMPONENT_REGEX:-api|realtime|web-employee|web-merchant|web-admin|worker-outbox-relay|worker-payroll-settler|worker-on-time-evaluator|scheduler-cutoff|scheduler-no-show|scheduler-doc-expiry|scheduler-feedback}"
crashloop_observe_seconds="${CRASHLOOP_OBSERVE_SECONDS:-45}"
min_range_signal_seconds="${MIN_RANGE_SIGNAL_SECONDS:-15}"
dashboard_file="chart/tbite-platform/dashboards/local-ha-drills.json"
port_forward_pid=""
baseline_data_cache_degraded_seconds=0
baseline_data_events=0

float_ge() {
  awk -v left="$1" -v right="$2" 'BEGIN { exit !(left >= right) }'
}

float_gt() {
  awk -v left="$1" -v right="$2" 'BEGIN { exit !(left > right) }'
}

float_le() {
  awk -v left="$1" -v right="$2" 'BEGIN { exit !(left <= right) }'
}

timeout_seconds() {
  local value="$1"
  local number="$1"
  local unit="s"
  if [[ "${value}" == *[smh] ]]; then
    number="${value%?}"
    unit="${value: -1}"
  fi
  if [[ -z "${number}" || "${number}" == *[!0-9]* ]]; then
    echo "invalid TIMEOUT '${value}', expected seconds or a value ending in s, m, or h" >&2
    exit 2
  fi
  case "${unit}" in
    s) echo "${number}" ;;
    m) echo "$(( number * 60 ))" ;;
    h) echo "$(( number * 3600 ))" ;;
  esac
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

cleanup() {
  if [[ -n "${port_forward_pid}" ]]; then
    kill "${port_forward_pid}" >/dev/null 2>&1 || true
  fi
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
  dashboard_target_value "Data service availability" "database service degraded"
}

dashboard_data_messaging_degraded() {
  dashboard_target_value "Data service availability" "messaging service degraded"
}

dashboard_data_cache_degraded() {
  dashboard_target_value "Data service availability" "cache service degraded"
}

dashboard_data_object_storage_degraded() {
  dashboard_target_value "Data service availability" "object storage service degraded"
}

dashboard_data_app_dependency_clients_degraded() {
  dashboard_target_value "Data service availability" "app dependency clients degraded"
}

dashboard_data_cache_degraded_seconds_10m() {
  dashboard_target_value "Data service activity" "cache service degraded seconds / 10m"
}

dashboard_data_failover_recreate_error_events_10m() {
  dashboard_target_value "Data service activity" "data service failover/recreate/error events / 10m"
}

dashboard_valkey_ready_pods() {
  promql_value "sum(kube_pod_status_ready{namespace=\"${namespace}\",pod=~\"${statefulset}-[0-9]+\",condition=\"true\"}) or vector(0)"
}

dashboard_valkey_scrape_up() {
  promql_value "sum(redis_up{namespace=\"${namespace}\"}) or vector(0)"
}

dashboard_valkey_masters() {
  promql_value "sum((redis_instance_info{namespace=\"${namespace}\",role=\"master\"} and on(pod) (timestamp(redis_instance_info{namespace=\"${namespace}\",role=\"master\"}) == on(pod) group_left max by (pod) (timestamp(redis_instance_info{namespace=\"${namespace}\"})))) and on(pod) (redis_up{namespace=\"${namespace}\"} == 1)) or vector(0)"
}

dashboard_valkey_replicas() {
  promql_value "sum((redis_instance_info{namespace=\"${namespace}\",role=\"slave\"} and on(pod) (timestamp(redis_instance_info{namespace=\"${namespace}\",role=\"slave\"}) == on(pod) group_left max by (pod) (timestamp(redis_instance_info{namespace=\"${namespace}\"})))) and on(pod) (redis_up{namespace=\"${namespace}\"} == 1)) or vector(0)"
}

dashboard_valkey_connected_replicas() {
  promql_value "sum(redis_connected_slaves{namespace=\"${namespace}\"} and on(pod) (redis_instance_info{namespace=\"${namespace}\",role=\"master\"} and on(pod) (timestamp(redis_instance_info{namespace=\"${namespace}\",role=\"master\"}) == on(pod) group_left max by (pod) (timestamp(redis_instance_info{namespace=\"${namespace}\"}))))) or vector(0)"
}

dashboard_valkey_replica_link_min() {
  promql_value "min(redis_master_link_up{namespace=\"${namespace}\"} and on(pod) (redis_instance_info{namespace=\"${namespace}\",role=\"slave\"} and on(pod) (timestamp(redis_instance_info{namespace=\"${namespace}\",role=\"slave\"}) == on(pod) group_left max by (pod) (timestamp(redis_instance_info{namespace=\"${namespace}\"}))))) or vector(1)"
}

dashboard_valkey_primary_endpoint_count() {
  promql_value "sum(kube_endpointslice_endpoints{namespace=\"${namespace}\",endpointslice=~\"${primary_service}.*\",ready=\"true\"}) or vector(0)"
}

dashboard_valkey_primary_endpoint_master() {
  promql_value "sum(label_replace(kube_endpointslice_endpoints{namespace=\"${namespace}\",endpointslice=~\"${primary_service}.*\",ready=\"true\"}, \"pod\", \"\$1\", \"targetref_name\", \"(.*)\") and on(namespace,pod) ((redis_instance_info{namespace=\"${namespace}\",role=\"master\"} and on(pod) (timestamp(redis_instance_info{namespace=\"${namespace}\",role=\"master\"}) == on(pod) group_left max by (pod) (timestamp(redis_instance_info{namespace=\"${namespace}\"})))) and on(pod) (redis_up{namespace=\"${namespace}\"} == 1))) or vector(0)"
}

dashboard_valkey_scrape_errors() {
  promql_value "sum(redis_exporter_last_scrape_error{namespace=\"${namespace}\"}) or vector(0)"
}

dashboard_valkey_unhealthy() {
  promql_value "(((sum(kube_pod_status_ready{namespace=\"${namespace}\",pod=~\"${statefulset}-[0-9]+\",condition=\"true\"}) or vector(0)) < bool ${desired}) + ((sum(redis_up{namespace=\"${namespace}\"}) or vector(0)) < bool ${desired}) + ((sum((redis_instance_info{namespace=\"${namespace}\",role=\"master\"} and on(pod) (timestamp(redis_instance_info{namespace=\"${namespace}\",role=\"master\"}) == on(pod) group_left max by (pod) (timestamp(redis_instance_info{namespace=\"${namespace}\"})))) and on(pod) (redis_up{namespace=\"${namespace}\"} == 1)) or vector(0)) != bool 1) + ((sum((redis_instance_info{namespace=\"${namespace}\",role=\"slave\"} and on(pod) (timestamp(redis_instance_info{namespace=\"${namespace}\",role=\"slave\"}) == on(pod) group_left max by (pod) (timestamp(redis_instance_info{namespace=\"${namespace}\"})))) and on(pod) (redis_up{namespace=\"${namespace}\"} == 1)) or vector(0)) < bool ${expected_replicas}) + ((sum(redis_connected_slaves{namespace=\"${namespace}\"} and on(pod) (redis_instance_info{namespace=\"${namespace}\",role=\"master\"} and on(pod) (timestamp(redis_instance_info{namespace=\"${namespace}\",role=\"master\"}) == on(pod) group_left max by (pod) (timestamp(redis_instance_info{namespace=\"${namespace}\"}))))) or vector(0)) < bool ${expected_replicas}) + ((min(redis_master_link_up{namespace=\"${namespace}\"} and on(pod) (redis_instance_info{namespace=\"${namespace}\",role=\"slave\"} and on(pod) (timestamp(redis_instance_info{namespace=\"${namespace}\",role=\"slave\"}) == on(pod) group_left max by (pod) (timestamp(redis_instance_info{namespace=\"${namespace}\"}))))) or vector(1)) < bool 1) + ((sum(kube_endpointslice_endpoints{namespace=\"${namespace}\",endpointslice=~\"${primary_service}.*\",ready=\"true\"}) or vector(0)) != bool 1) + ((sum(label_replace(kube_endpointslice_endpoints{namespace=\"${namespace}\",endpointslice=~\"${primary_service}.*\",ready=\"true\"}, \"pod\", \"\$1\", \"targetref_name\", \"(.*)\") and on(namespace,pod) ((redis_instance_info{namespace=\"${namespace}\",role=\"master\"} and on(pod) (timestamp(redis_instance_info{namespace=\"${namespace}\",role=\"master\"}) == on(pod) group_left max by (pod) (timestamp(redis_instance_info{namespace=\"${namespace}\"})))) and on(pod) (redis_up{namespace=\"${namespace}\"} == 1))) or vector(0)) != bool 1) + ((sum(redis_exporter_last_scrape_error{namespace=\"${namespace}\"}) or vector(0)) > bool ${max_scrape_errors}))"
}

dashboard_valkey_degraded_seconds_10m() {
  promql_value "(sum_over_time(((((sum(kube_pod_status_ready{namespace=\"${namespace}\",pod=~\"${statefulset}-[0-9]+\",condition=\"true\"}) or vector(0)) < bool ${desired}) + ((sum(redis_up{namespace=\"${namespace}\"}) or vector(0)) < bool ${desired}) + ((sum((redis_instance_info{namespace=\"${namespace}\",role=\"master\"} and on(pod) (timestamp(redis_instance_info{namespace=\"${namespace}\",role=\"master\"}) == on(pod) group_left max by (pod) (timestamp(redis_instance_info{namespace=\"${namespace}\"})))) and on(pod) (redis_up{namespace=\"${namespace}\"} == 1)) or vector(0)) != bool 1) + ((sum((redis_instance_info{namespace=\"${namespace}\",role=\"slave\"} and on(pod) (timestamp(redis_instance_info{namespace=\"${namespace}\",role=\"slave\"}) == on(pod) group_left max by (pod) (timestamp(redis_instance_info{namespace=\"${namespace}\"})))) and on(pod) (redis_up{namespace=\"${namespace}\"} == 1)) or vector(0)) < bool ${expected_replicas}) + ((sum(redis_connected_slaves{namespace=\"${namespace}\"} and on(pod) (redis_instance_info{namespace=\"${namespace}\",role=\"master\"} and on(pod) (timestamp(redis_instance_info{namespace=\"${namespace}\",role=\"master\"}) == on(pod) group_left max by (pod) (timestamp(redis_instance_info{namespace=\"${namespace}\"}))))) or vector(0)) < bool ${expected_replicas}) + ((min(redis_master_link_up{namespace=\"${namespace}\"} and on(pod) (redis_instance_info{namespace=\"${namespace}\",role=\"slave\"} and on(pod) (timestamp(redis_instance_info{namespace=\"${namespace}\",role=\"slave\"}) == on(pod) group_left max by (pod) (timestamp(redis_instance_info{namespace=\"${namespace}\"}))))) or vector(1)) < bool 1) + ((sum(kube_endpointslice_endpoints{namespace=\"${namespace}\",endpointslice=~\"${primary_service}.*\",ready=\"true\"}) or vector(0)) != bool 1) + ((sum(label_replace(kube_endpointslice_endpoints{namespace=\"${namespace}\",endpointslice=~\"${primary_service}.*\",ready=\"true\"}, \"pod\", \"\$1\", \"targetref_name\", \"(.*)\") and on(namespace,pod) ((redis_instance_info{namespace=\"${namespace}\",role=\"master\"} and on(pod) (timestamp(redis_instance_info{namespace=\"${namespace}\",role=\"master\"}) == on(pod) group_left max by (pod) (timestamp(redis_instance_info{namespace=\"${namespace}\"})))) and on(pod) (redis_up{namespace=\"${namespace}\"} == 1))) or vector(0)) != bool 1) + ((sum(redis_exporter_last_scrape_error{namespace=\"${namespace}\"}) or vector(0)) > bool ${max_scrape_errors})) > bool 0)[10m:15s]) or vector(0)) * 15"
}

dashboard_valkey_role_changes() {
  promql_value "sum(changes(redis_instance_info{namespace=\"${namespace}\",role=\"master\"}[10m])) or vector(0)"
}

dashboard_valkey_pod_recreations() {
  promql_value "sum(changes(kube_pod_created{namespace=\"${namespace}\",pod=~\"${statefulset}-[0-9]+\"}[10m])) or vector(0)"
}

dashboard_valkey_pod_created_timestamp() {
  local target_pod="$1"
  promql_value "max(kube_pod_created{namespace=\"${namespace}\",pod=\"${target_pod}\"}) or vector(0)"
}

dashboard_cache_client_not_ready_pods() {
  promql_value "sum(kube_pod_status_ready{namespace=\"${namespace}\",pod=~\"${release}-tbite-platform-(${cache_client_regex}).*\",condition=\"false\"} == 1) or vector(0)"
}

dashboard_cache_client_readiness_changes() {
  promql_value "sum(changes(kube_pod_status_ready{namespace=\"${namespace}\",pod=~\"${release}-tbite-platform-(${cache_client_regex}).*\",condition=\"true\"}[10m])) or vector(0)"
}

wait_for_dashboard_at_least() {
  local name="$1"
  local metric_func="$2"
  local threshold="$3"
  local deadline=$((SECONDS + $(timeout_seconds "${timeout}")))
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
  local deadline=$((SECONDS + $(timeout_seconds "${timeout}")))
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
  local deadline=$((SECONDS + $(timeout_seconds "${timeout}")))
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

wait_for_dashboard_delta_or_present() {
  local name="$1"
  local metric_func="$2"
  local baseline="$3"
  local min_delta="$4"
  local min_present="$5"
  local deadline=$((SECONDS + $(timeout_seconds "${timeout}")))
  local current

  while (( SECONDS < deadline )); do
    current="$("${metric_func}")"
    if [[ -n "${current}" ]] && awk -v current="${current}" -v baseline="${baseline}" -v min_delta="${min_delta}" -v min_present="${min_present}" '
      BEGIN {
        exit !(((current - baseline) >= min_delta) || (baseline >= min_present && current >= min_present))
      }'; then
      printf '%s %s=%s baseline=%s min_delta=%s min_present=%s\n' "$(date -u +%Y-%m-%dT%H:%M:%SZ)" "${name}" "${current}" "${baseline}" "${min_delta}" "${min_present}"
      return 0
    fi
    printf '%s %s=%s waiting_for_delta_at_least=%s_or_present=%s baseline=%s\n' "$(date -u +%Y-%m-%dT%H:%M:%SZ)" "${name}" "${current:-empty}" "${min_delta}" "${min_present}" "${baseline}"
    sleep "${poll_seconds}"
  done

  echo "timed out waiting for dashboard signal ${name} to increase by ${min_delta} from baseline ${baseline} or stay present at ${min_present}" >&2
  return 1
}

wait_for_dashboard_baseline() {
  start_vm_port_forward
  wait_for_dashboard_at_most "dashboard_data_database_degraded" dashboard_data_database_degraded 0
  wait_for_dashboard_at_most "dashboard_data_messaging_degraded" dashboard_data_messaging_degraded 0
  wait_for_dashboard_at_most "dashboard_data_cache_degraded" dashboard_data_cache_degraded 0
  wait_for_dashboard_at_most "dashboard_data_object_storage_degraded" dashboard_data_object_storage_degraded 0
  wait_for_dashboard_at_most "dashboard_data_app_dependency_clients_degraded" dashboard_data_app_dependency_clients_degraded 0
  wait_for_dashboard_at_least "dashboard_valkey_ready_pods" dashboard_valkey_ready_pods "${desired}"
  wait_for_dashboard_at_least "dashboard_valkey_scrape_up" dashboard_valkey_scrape_up "${desired}"
  wait_for_dashboard_at_least "dashboard_valkey_masters" dashboard_valkey_masters 1
  wait_for_dashboard_at_most "dashboard_valkey_masters" dashboard_valkey_masters 1
  wait_for_dashboard_at_least "dashboard_valkey_replicas" dashboard_valkey_replicas "${expected_replicas}"
  wait_for_dashboard_at_most "dashboard_valkey_replicas" dashboard_valkey_replicas "${expected_replicas}"
  wait_for_dashboard_at_least "dashboard_valkey_connected_replicas" dashboard_valkey_connected_replicas "${expected_replicas}"
  wait_for_dashboard_at_least "dashboard_valkey_replica_link_min" dashboard_valkey_replica_link_min 1
  wait_for_dashboard_at_least "dashboard_valkey_primary_endpoint_count" dashboard_valkey_primary_endpoint_count 1
  wait_for_dashboard_at_most "dashboard_valkey_primary_endpoint_count" dashboard_valkey_primary_endpoint_count 1
  wait_for_dashboard_at_least "dashboard_valkey_primary_endpoint_master" dashboard_valkey_primary_endpoint_master 1
  wait_for_dashboard_at_most "dashboard_valkey_primary_endpoint_master" dashboard_valkey_primary_endpoint_master 1
  wait_for_dashboard_at_most "dashboard_valkey_scrape_errors" dashboard_valkey_scrape_errors "${max_scrape_errors}"
  wait_for_dashboard_at_most "dashboard_valkey_unhealthy" dashboard_valkey_unhealthy 0
  wait_for_dashboard_at_most "dashboard_cache_client_not_ready_pods" dashboard_cache_client_not_ready_pods 0
  baseline_data_cache_degraded_seconds="$(dashboard_data_cache_degraded_seconds_10m)"
  baseline_data_cache_degraded_seconds="${baseline_data_cache_degraded_seconds:-0}"
  baseline_data_events="$(dashboard_data_failover_recreate_error_events_10m)"
  baseline_data_events="${baseline_data_events:-0}"
  printf 'baseline_dashboard_data_cache_degraded_seconds_10m=%s\n' "${baseline_data_cache_degraded_seconds}"
  printf 'baseline_dashboard_data_failover_recreate_error_events_10m=%s\n' "${baseline_data_events}"
}

wait_for_dashboard_recovery() {
  local baseline_pod_created="$1"
  local target_pod="$2"
  local deleted_role="$3"
  local baseline_role_changes="$4"
  local baseline_pod_recreations="$5"
  local baseline_degraded_seconds="$6"

  wait_for_dashboard_at_least "dashboard_valkey_ready_pods" dashboard_valkey_ready_pods "${desired}"
  wait_for_dashboard_at_least "dashboard_valkey_scrape_up" dashboard_valkey_scrape_up "${desired}"
  wait_for_dashboard_at_least "dashboard_valkey_masters" dashboard_valkey_masters 1
  wait_for_dashboard_at_most "dashboard_valkey_masters" dashboard_valkey_masters 1
  wait_for_dashboard_at_least "dashboard_valkey_replicas" dashboard_valkey_replicas "${expected_replicas}"
  wait_for_dashboard_at_most "dashboard_valkey_replicas" dashboard_valkey_replicas "${expected_replicas}"
  wait_for_dashboard_at_least "dashboard_valkey_connected_replicas" dashboard_valkey_connected_replicas "${expected_replicas}"
  wait_for_dashboard_at_least "dashboard_valkey_replica_link_min" dashboard_valkey_replica_link_min 1
  wait_for_dashboard_at_least "dashboard_valkey_primary_endpoint_count" dashboard_valkey_primary_endpoint_count 1
  wait_for_dashboard_at_most "dashboard_valkey_primary_endpoint_count" dashboard_valkey_primary_endpoint_count 1
  wait_for_dashboard_at_least "dashboard_valkey_primary_endpoint_master" dashboard_valkey_primary_endpoint_master 1
  wait_for_dashboard_at_most "dashboard_valkey_primary_endpoint_master" dashboard_valkey_primary_endpoint_master 1
  wait_for_dashboard_at_most "dashboard_valkey_scrape_errors" dashboard_valkey_scrape_errors "${max_scrape_errors}"
  wait_for_dashboard_at_most "dashboard_valkey_unhealthy" dashboard_valkey_unhealthy 0
  wait_for_dashboard_at_most "dashboard_data_database_degraded" dashboard_data_database_degraded 0
  wait_for_dashboard_at_most "dashboard_data_messaging_degraded" dashboard_data_messaging_degraded 0
  wait_for_dashboard_at_most "dashboard_data_cache_degraded" dashboard_data_cache_degraded 0
  wait_for_dashboard_at_most "dashboard_data_object_storage_degraded" dashboard_data_object_storage_degraded 0
  wait_for_dashboard_at_most "dashboard_data_app_dependency_clients_degraded" dashboard_data_app_dependency_clients_degraded 0
  wait_for_dashboard_at_most "dashboard_cache_client_not_ready_pods" dashboard_cache_client_not_ready_pods 0
  wait_for_dashboard_delta_at_least "dashboard_valkey_pod_recreations" dashboard_valkey_pod_recreations "${baseline_pod_recreations}" 1
  wait_for_dashboard_delta_or_present "dashboard_valkey_degraded_seconds_10m" dashboard_valkey_degraded_seconds_10m "${baseline_degraded_seconds}" "${min_range_signal_seconds}" "${min_range_signal_seconds}"
  wait_for_dashboard_delta_or_present "dashboard_data_cache_degraded_seconds_10m" dashboard_data_cache_degraded_seconds_10m "${baseline_data_cache_degraded_seconds}" "${min_range_signal_seconds}" "${min_range_signal_seconds}"
  wait_for_dashboard_delta_or_present "dashboard_data_failover_recreate_error_events_10m" dashboard_data_failover_recreate_error_events_10m "${baseline_data_events}" 1 1
  wait_for_dashboard_target_pod_recreated "${target_pod}" "${baseline_pod_created}"
  if [[ "${deleted_role}" == "master" ]]; then
    wait_for_dashboard_delta_at_least "dashboard_valkey_role_changes" dashboard_valkey_role_changes "${baseline_role_changes}" 1
  fi
}

wait_for_dashboard_target_pod_recreated() {
  local target_pod="$1"
  local baseline_pod_created="$2"
  local deadline=$((SECONDS + $(timeout_seconds "${timeout}")))
  local current

  while (( SECONDS < deadline )); do
    current="$(dashboard_valkey_pod_created_timestamp "${target_pod}")"
    if [[ -n "${current}" ]] && float_gt "${current}" "${baseline_pod_created}"; then
      printf '%s dashboard_valkey_pod_created{%s}=%s threshold_gt=%s\n' \
        "$(date -u +%Y-%m-%dT%H:%M:%SZ)" "${target_pod}" "${current}" "${baseline_pod_created}"
      return 0
    fi
    printf '%s dashboard_valkey_pod_created{%s}=%s waiting_for_greater_than=%s\n' \
      "$(date -u +%Y-%m-%dT%H:%M:%SZ)" "${target_pod}" "${current:-empty}" "${baseline_pod_created}"
    sleep "${poll_seconds}"
  done

  echo "timed out waiting for dashboard signal kube_pod_created for ${target_pod} to change" >&2
  return 1
}

app_crashloops() {
  kubectl -n "${namespace}" get pods \
    -l "app.kubernetes.io/instance=${release},app.kubernetes.io/name=tbite-platform" \
    -o json \
    | jq -r --arg componentRegex "^(${app_component_regex})$" '
        .items[]
        | select((.metadata.labels["app.kubernetes.io/component"] // "") | test($componentRegex))
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
    echo "platform app pods entered CrashLoopBackOff during Valkey pod loss:" >&2
    printf 'pod\tcontainers\n' >&2
    printf '%s\n' "${crashloops}" >&2
    exit 1
  fi
}

assert_no_app_crashloops() {
  local deadline
  deadline=$((SECONDS + crashloop_observe_seconds))
  echo "==> watching platform app pods for CrashLoopBackOff for ${crashloop_observe_seconds}s"
  while (( SECONDS < deadline )); do
    fail_if_app_crashloops
    sleep "${poll_seconds}"
  done
}

desired="$(kubectl -n "${namespace}" get statefulset "${statefulset}" -o jsonpath='{.spec.replicas}')"
if [[ -z "${desired}" || "${desired}" == *[!0-9]* || "${desired}" == "0" ]]; then
  echo "statefulset ${namespace}/${statefulset} has invalid desired replicas: ${desired:-empty}" >&2
  exit 1
fi
expected_replicas="$((desired - 1))"

valkey_password="$(kubectl -n "${namespace}" get secret "${valkey_secret}" -o jsonpath='{.data.password}' | base64 -d)"

valkey_role() {
  local candidate="$1"
  kubectl -n "${namespace}" exec "${candidate}" -c valkey -- sh -lc \
    'VALKEYCLI_AUTH="$0" valkey-cli role | head -n 1' "${valkey_password}" 2>/dev/null || true
}

valkey_info_field() {
  local candidate="$1"
  local field="$2"
  kubectl -n "${namespace}" exec "${candidate}" -c valkey -- sh -lc \
    'VALKEYCLI_AUTH="$0" valkey-cli info replication' "${valkey_password}" 2>/dev/null \
    | awk -F: -v key="${field}" '$1 == key {gsub("\r", "", $2); print $2; exit}'
}

ready_status() {
  local candidate="$1"
  kubectl -n "${namespace}" get pod "${candidate}" -o jsonpath='{.status.conditions[?(@.type=="Ready")].status}' 2>/dev/null || true
}

print_topology() {
  local candidate role ready connected_slaves master_link_status
  printf 'pod\tready\trole\tconnected_slaves\tmaster_link_status\n'
  while IFS= read -r candidate; do
    [[ -n "${candidate}" ]] || continue
    role="$(valkey_role "${candidate}")"
    ready="$(ready_status "${candidate}")"
    connected_slaves=""
    master_link_status=""
    if [[ "${role}" == "master" ]]; then
      connected_slaves="$(valkey_info_field "${candidate}" connected_slaves)"
    elif [[ "${role}" == "slave" ]]; then
      master_link_status="$(valkey_info_field "${candidate}" master_link_status)"
    fi
    printf '%s\t%s\t%s\t%s\t%s\n' \
      "${candidate}" "${ready:-unknown}" "${role:-unknown}" \
      "${connected_slaves:-}" "${master_link_status:-}"
  done < <(kubectl -n "${namespace}" get pods -l app.kubernetes.io/name=valkey,app.kubernetes.io/component=node -o jsonpath='{range .items[*]}{.metadata.name}{"\n"}{end}' | sort)
}

current_master_pod() {
  local candidate role
  while IFS= read -r candidate; do
    [[ -n "${candidate}" ]] || continue
    role="$(valkey_role "${candidate}")"
    if [[ "${role}" == "master" ]]; then
      printf '%s\n' "${candidate}"
      return 0
    fi
  done < <(kubectl -n "${namespace}" get pods -l app.kubernetes.io/name=valkey,app.kubernetes.io/component=node -o jsonpath='{range .items[*]}{.metadata.name}{"\n"}{end}' | sort)
  return 1
}

verify_primary_service() {
  local master master_ip endpoint_count probe_pod key value set_result got
  local -a endpoint_ips

  master="$(current_master_pod)"
  if [[ -z "${master}" ]]; then
    echo "could not resolve current Valkey master pod" >&2
    exit 1
  fi

  master_ip="$(kubectl -n "${namespace}" get pod "${master}" -o jsonpath='{.status.podIP}')"
  mapfile -t endpoint_ips < <(kubectl -n "${namespace}" get endpoints "${primary_service}" -o jsonpath='{range .subsets[*].addresses[*]}{.ip}{"\n"}{end}' | sort -u)
  endpoint_count="${#endpoint_ips[@]}"
  if (( endpoint_count != 1 )); then
    printf 'primary service %s has %s endpoints, want exactly 1: %s\n' \
      "${primary_service}" "${endpoint_count}" "${endpoint_ips[*]:-none}" >&2
    exit 1
  fi
  if [[ "${endpoint_ips[0]}" != "${master_ip}" ]]; then
    printf 'primary service %s endpoint=%s does not match master=%s ip=%s\n' \
      "${primary_service}" "${endpoint_ips[0]}" "${master}" "${master_ip}" >&2
    exit 1
  fi

  probe_pod="${master}"
  key="local-ha:valkey-primary-service:$(date -u +%Y%m%dT%H%M%SZ)"
  value="ok-${master}"
  set_result="$(kubectl -n "${namespace}" exec "${probe_pod}" -c valkey -- \
    env "VALKEYCLI_AUTH=${valkey_password}" valkey-cli -h "${primary_service}" -p 6379 set "${key}" "${value}" 2>&1)" || {
    printf 'primary service write failed through %s: %s\n' "${primary_service}" "${set_result}" >&2
    exit 1
  }
  if [[ "${set_result//$'\r'/}" != "OK" ]]; then
    printf 'primary service write through %s returned %s, want OK\n' "${primary_service}" "${set_result}" >&2
    exit 1
  fi
  got="$(kubectl -n "${namespace}" exec "${probe_pod}" -c valkey -- \
    env "VALKEYCLI_AUTH=${valkey_password}" valkey-cli -h "${primary_service}" -p 6379 get "${key}" 2>/dev/null | tr -d '\r')"
  kubectl -n "${namespace}" exec "${probe_pod}" -c valkey -- \
    env "VALKEYCLI_AUTH=${valkey_password}" valkey-cli -h "${primary_service}" -p 6379 del "${key}" >/dev/null 2>&1 || true
  if [[ "${got}" != "${value}" ]]; then
    printf 'primary service read through %s returned %s, want %s\n' "${primary_service}" "${got}" "${value}" >&2
    exit 1
  fi

  printf 'primary_service=%s endpoint=%s master=%s write_ok=true\n' "${primary_service}" "${endpoint_ips[0]}" "${master}"
}

wait_for_topology() {
  local deadline pods expected_replicas topology ready_count master_count replica_count linked_replicas connected_replicas
  deadline=$((SECONDS + $(timeout_seconds "${timeout}")))
  while true; do
    pods="$(kubectl -n "${namespace}" get pods -l app.kubernetes.io/name=valkey,app.kubernetes.io/component=node -o jsonpath='{range .items[*]}{.metadata.name}{"\n"}{end}' | sort)"
    expected_replicas="$(grep -c . <<<"${pods}")"
    topology="$(print_topology)"
    ready_count="$(awk -F '\t' 'NR > 1 && $2 == "True" {count++} END {print count + 0}' <<<"${topology}")"
    master_count="$(awk -F '\t' 'NR > 1 && $3 == "master" {count++} END {print count + 0}' <<<"${topology}")"
    replica_count="$(awk -F '\t' 'NR > 1 && $3 == "slave" {count++} END {print count + 0}' <<<"${topology}")"
    linked_replicas="$(awk -F '\t' 'NR > 1 && $3 == "slave" && $5 == "up" {count++} END {print count + 0}' <<<"${topology}")"
    connected_replicas="$(awk -F '\t' 'NR > 1 && $3 == "master" {sum += $4} END {print sum + 0}' <<<"${topology}")"

    printf '%s ready=%s/%s masters=%s replicas=%s connected_replicas=%s linked_replicas=%s\n' \
      "$(date -u +%Y-%m-%dT%H:%M:%SZ)" "${ready_count}" "${expected_replicas}" \
      "${master_count}" "${replica_count}" "${connected_replicas}" "${linked_replicas}"
    printf '%s\n' "${topology}"

    if [[ "${ready_count}" -eq "${expected_replicas}" \
      && "${master_count}" -eq 1 \
      && "${replica_count}" -eq $((expected_replicas - 1)) \
      && "${connected_replicas}" -ge $((expected_replicas - 1)) \
      && "${linked_replicas}" -eq $((expected_replicas - 1)) ]]; then
      return 0
    fi

    if (( SECONDS > deadline )); then
      echo "timed out waiting for Valkey topology to recover" >&2
      printf '%s\n' "${topology}" >&2
      exit 1
    fi
    sleep "${poll_seconds}"
  done
}

if [[ -z "${pod}" ]]; then
  while IFS= read -r candidate; do
    role="$(valkey_role "${candidate}")"
    if [[ "${role}" == "${target_role}" ]]; then
      pod="${candidate}"
      break
    fi
  done < <(kubectl -n "${namespace}" get pods -l app.kubernetes.io/name=valkey,app.kubernetes.io/component=node -o jsonpath='{range .items[*]}{.metadata.name}{"\n"}{end}')
fi
if [[ -z "${pod}" ]]; then
  echo "could not find a Valkey pod with role ${target_role} in namespace ${namespace}" >&2
  exit 1
fi

old_uid="$(kubectl -n "${namespace}" get pod "${pod}" -o jsonpath='{.metadata.uid}')"
old_role="$(valkey_role "${pod}")"
wait_for_dashboard_baseline
baseline_role_changes="$(dashboard_valkey_role_changes)"
baseline_role_changes="${baseline_role_changes:-0}"
baseline_pod_recreations="$(dashboard_valkey_pod_recreations)"
baseline_pod_recreations="${baseline_pod_recreations:-0}"
baseline_degraded_seconds="$(dashboard_valkey_degraded_seconds_10m)"
baseline_degraded_seconds="${baseline_degraded_seconds:-0}"
baseline_pod_created="$(dashboard_valkey_pod_created_timestamp "${pod}")"
baseline_pod_created="${baseline_pod_created:-0}"
baseline_cache_client_readiness_changes="$(dashboard_cache_client_readiness_changes)"
baseline_cache_client_readiness_changes="${baseline_cache_client_readiness_changes:-0}"
printf 'baseline_dashboard_valkey_role_changes=%s\n' "${baseline_role_changes}"
printf 'baseline_dashboard_valkey_pod_recreations=%s\n' "${baseline_pod_recreations}"
printf 'baseline_dashboard_valkey_degraded_seconds_10m=%s\n' "${baseline_degraded_seconds}"
printf 'baseline_dashboard_valkey_pod_created{%s}=%s\n' "${pod}" "${baseline_pod_created}"
printf 'baseline_dashboard_cache_client_readiness_changes=%s\n' "${baseline_cache_client_readiness_changes}"

echo "==> deleting Valkey pod ${pod} role=${old_role:-unknown}"
kubectl -n "${namespace}" delete pod "${pod}" --wait=false

sleep "${poll_seconds}"
printf 'fault_sample_dashboard_valkey_unhealthy=%s\n' "$(dashboard_valkey_unhealthy)"
printf 'fault_sample_dashboard_data_cache_degraded=%s\n' "$(dashboard_data_cache_degraded)"
wait_for_dashboard_at_most "dashboard_data_database_degraded" dashboard_data_database_degraded 0
wait_for_dashboard_at_most "dashboard_data_messaging_degraded" dashboard_data_messaging_degraded 0
wait_for_dashboard_at_most "dashboard_data_object_storage_degraded" dashboard_data_object_storage_degraded 0
wait_for_dashboard_at_most "dashboard_data_app_dependency_clients_degraded" dashboard_data_app_dependency_clients_degraded 0
wait_for_dashboard_at_most "dashboard_cache_client_not_ready_pods" dashboard_cache_client_not_ready_pods 0
printf 'fault_dashboard_valkey_unhealthy=%s\n' "$(dashboard_valkey_unhealthy)"
printf 'fault_dashboard_data_cache_degraded=%s\n' "$(dashboard_data_cache_degraded)"
printf 'fault_dashboard_cache_client_not_ready_pods=%s\n' "$(dashboard_cache_client_not_ready_pods)"

deadline=$((SECONDS + $(timeout_seconds "${timeout}")))
while true; do
  new_uid="$(kubectl -n "${namespace}" get pod "${pod}" -o jsonpath='{.metadata.uid}' 2>/dev/null || true)"
  ready="$(kubectl -n "${namespace}" get pod "${pod}" -o jsonpath='{.status.conditions[?(@.type=="Ready")].status}' 2>/dev/null || true)"
  phase="$(kubectl -n "${namespace}" get pod "${pod}" -o jsonpath='{.status.phase}' 2>/dev/null || true)"
  role="$(valkey_role "${pod}")"
  printf '%s pod=%s phase=%s ready=%s role=%s uid_changed=%s\n' \
    "$(date -u +%Y-%m-%dT%H:%M:%SZ)" "${pod}" "${phase:-missing}" "${ready:-unknown}" "${role:-unknown}" \
    "$([[ -n "${new_uid}" && "${new_uid}" != "${old_uid}" ]] && echo true || echo false)"
  fail_if_app_crashloops
  if [[ -n "${new_uid}" && "${new_uid}" != "${old_uid}" && "${ready}" == "True" ]]; then
    break
  fi
  if (( SECONDS > deadline )); then
    kubectl -n "${namespace}" describe pod "${pod}" >&2 || true
    echo "timed out waiting for replacement Valkey pod ${pod}" >&2
    exit 1
  fi
  sleep "${poll_seconds}"
done

kubectl -n "${namespace}" rollout status "statefulset/${statefulset}" --timeout="${timeout}"
wait_for_topology
verify_primary_service
wait_for_dashboard_recovery "${baseline_pod_created}" "${pod}" "${old_role}" "${baseline_role_changes}" "${baseline_pod_recreations}" "${baseline_degraded_seconds}"
printf 'recovery_dashboard_valkey_degraded_seconds_10m=%s\n' "$(dashboard_valkey_degraded_seconds_10m)"
printf 'recovery_dashboard_data_cache_degraded_seconds_10m=%s\n' "$(dashboard_data_cache_degraded_seconds_10m)"
printf 'recovery_dashboard_data_failover_recreate_error_events_10m=%s\n' "$(dashboard_data_failover_recreate_error_events_10m)"
printf 'recovery_dashboard_data_cache_degraded=%s\n' "$(dashboard_data_cache_degraded)"
printf 'recovery_dashboard_cache_client_readiness_changes=%s\n' "$(dashboard_cache_client_readiness_changes)"
assert_no_app_crashloops
kubectl -n "${namespace}" get pods -l app.kubernetes.io/name=valkey,app.kubernetes.io/component=node -o wide
