#!/usr/bin/env bash
set -euo pipefail

cd "$(dirname "$0")/../.."

namespace="${NAMESPACE:-tbite}"
release="${RELEASE:-tbite}"
statefulset="${MINIO_STATEFULSET:-${release}-pool-0}"
target_pod="${POD:-}"
minio_service="${MINIO_SERVICE:-minio}"
minio_bucket="${MINIO_BUCKET:-tbite-dev}"
s3_secret="${S3_SECRET:-tbite-s3}"
timeout_seconds="${TIMEOUT_SECONDS:-240}"
poll_seconds="${POLL_SECONDS:-5}"
vm_service="${VM_SERVICE:-vmsingle-${release}-victoria-metrics-k8s-stack}"
vm_url="${VM_URL:-}"
vm_local_port="${VM_LOCAL_PORT:-18428}"
object_storage_client_regex="${OBJECT_STORAGE_CLIENT_COMPONENT_REGEX:-api|worker-payroll-settler}"
app_component_regex="${APP_COMPONENT_REGEX:-api|realtime|web-employee|web-merchant|web-admin|worker-outbox-relay|worker-payroll-settler|worker-on-time-evaluator|scheduler-cutoff|scheduler-no-show|scheduler-doc-expiry|scheduler-feedback}"
crashloop_observe_seconds="${CRASHLOOP_OBSERVE_SECONDS:-45}"
min_range_signal_seconds="${MIN_RANGE_SIGNAL_SECONDS:-15}"
max_s3_5xx_delta="${MAX_MINIO_S3_5XX_DELTA:-0}"
dashboard_file="chart/tbite-platform/dashboards/local-ha-drills.json"
port_forward_pid=""
baseline_data_object_storage_degraded_seconds=0

minio_selector="v1.min.io/tenant=${release},v1.min.io/pool=pool-0"

float_ge() {
  awk -v left="$1" -v right="$2" 'BEGIN { exit !(left >= right) }'
}

float_gt() {
  awk -v left="$1" -v right="$2" 'BEGIN { exit !(left > right) }'
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

dashboard_data_object_storage_degraded_seconds_10m() {
  dashboard_target_value "Data service activity" "object storage service degraded seconds / 10m"
}

dashboard_minio_ready_pods() {
  promql_value "sum(kube_pod_status_ready{namespace=\"${namespace}\",pod=~\"${statefulset}-[0-9]+\",condition=\"true\"}) or vector(0)"
}

dashboard_minio_ready_gap() {
  promql_value "sum(kube_statefulset_replicas{namespace=\"${namespace}\",statefulset=\"${statefulset}\"} - kube_statefulset_status_replicas_ready{namespace=\"${namespace}\",statefulset=\"${statefulset}\"}) or vector(0)"
}

dashboard_minio_not_ready_pods() {
  promql_value "sum(kube_pod_status_ready{namespace=\"${namespace}\",pod=~\"${statefulset}-[0-9]+\",condition=\"false\"} == 1) or vector(0)"
}

dashboard_minio_pod_recreations() {
  promql_value "sum(changes(kube_pod_created{namespace=\"${namespace}\",pod=~\"${statefulset}-[0-9]+\"}[10m])) or vector(0)"
}

dashboard_minio_pod_created_timestamp() {
  local pod="$1"
  promql_value "max(kube_pod_created{namespace=\"${namespace}\",pod=\"${pod}\"}) or vector(0)"
}

dashboard_object_storage_client_not_ready_pods() {
  promql_value "sum(kube_pod_status_ready{namespace=\"${namespace}\",pod=~\"${release}-tbite-platform-(${object_storage_client_regex}).*\",condition=\"false\"} == 1) or vector(0)"
}

dashboard_object_storage_client_readiness_changes() {
  promql_value "sum(changes(kube_pod_status_ready{namespace=\"${namespace}\",pod=~\"${release}-tbite-platform-(${object_storage_client_regex}).*\",condition=\"true\"}[10m])) or vector(0)"
}

dashboard_minio_api_scrape_up() {
  promql_value "sum(up{namespace=\"${namespace}\",service=\"${minio_service}\",job=~\".*minio-tenant\"}) or vector(0)"
}

dashboard_minio_cluster_health() {
  promql_value "max(minio_cluster_health_status{namespace=\"${namespace}\",service=\"${minio_service}\"}) or vector(0)"
}

dashboard_minio_nodes_online() {
  promql_value "max(minio_cluster_nodes_online_total{namespace=\"${namespace}\",service=\"${minio_service}\"}) or vector(0)"
}

dashboard_minio_nodes_offline() {
  promql_value "max(minio_cluster_nodes_offline_total{namespace=\"${namespace}\",service=\"${minio_service}\"}) or vector(0)"
}

dashboard_minio_drives_offline() {
  promql_value "max(minio_cluster_drive_offline_total{namespace=\"${namespace}\",service=\"${minio_service}\"}) or vector(0)"
}

dashboard_minio_s3_5xx_10m() {
  promql_value "sum(increase(minio_s3_requests_5xx_errors_total{namespace=\"${namespace}\",service=\"${minio_service}\"}[10m])) or vector(0)"
}

dashboard_minio_unhealthy() {
  promql_value "(((sum(kube_pod_status_ready{namespace=\"${namespace}\",pod=~\"${statefulset}-[0-9]+\",condition=\"true\"}) or vector(0)) < bool ${desired}) + ((sum(kube_statefulset_replicas{namespace=\"${namespace}\",statefulset=\"${statefulset}\"} - kube_statefulset_status_replicas_ready{namespace=\"${namespace}\",statefulset=\"${statefulset}\"}) or vector(0)) > bool 0) + ((sum(kube_pod_status_ready{namespace=\"${namespace}\",pod=~\"${statefulset}-[0-9]+\",condition=\"false\"} == 1) or vector(0)) > bool 0) + ((sum(up{namespace=\"${namespace}\",service=\"${minio_service}\",job=~\".*minio-tenant\"}) or vector(0)) < bool 1) + ((max(minio_cluster_health_status{namespace=\"${namespace}\",service=\"${minio_service}\"}) or vector(0)) < bool 1) + ((max(minio_cluster_nodes_online_total{namespace=\"${namespace}\",service=\"${minio_service}\"}) or vector(0)) < bool ${desired}) + ((max(minio_cluster_nodes_offline_total{namespace=\"${namespace}\",service=\"${minio_service}\"}) or vector(0)) > bool 0) + ((max(minio_cluster_drive_offline_total{namespace=\"${namespace}\",service=\"${minio_service}\"}) or vector(0)) > bool 0))"
}

dashboard_minio_degraded_seconds_10m() {
  promql_value "(sum_over_time(((((sum(kube_pod_status_ready{namespace=\"${namespace}\",pod=~\"${statefulset}-[0-9]+\",condition=\"true\"}) or vector(0)) < bool ${desired}) + ((sum(kube_statefulset_replicas{namespace=\"${namespace}\",statefulset=\"${statefulset}\"} - kube_statefulset_status_replicas_ready{namespace=\"${namespace}\",statefulset=\"${statefulset}\"}) or vector(0)) > bool 0) + ((sum(kube_pod_status_ready{namespace=\"${namespace}\",pod=~\"${statefulset}-[0-9]+\",condition=\"false\"} == 1) or vector(0)) > bool 0) + ((sum(up{namespace=\"${namespace}\",service=\"${minio_service}\",job=~\".*minio-tenant\"}) or vector(0)) < bool 1) + ((max(minio_cluster_health_status{namespace=\"${namespace}\",service=\"${minio_service}\"}) or vector(0)) < bool 1) + ((max(minio_cluster_nodes_online_total{namespace=\"${namespace}\",service=\"${minio_service}\"}) or vector(0)) < bool ${desired}) + ((max(minio_cluster_nodes_offline_total{namespace=\"${namespace}\",service=\"${minio_service}\"}) or vector(0)) > bool 0) + ((max(minio_cluster_drive_offline_total{namespace=\"${namespace}\",service=\"${minio_service}\"}) or vector(0)) > bool 0)) > bool 0)[10m:15s]) or vector(0)) * 15"
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

wait_for_dashboard_greater_than() {
  local name="$1"
  local metric_func="$2"
  local threshold="$3"
  local deadline=$((SECONDS + timeout_seconds))
  local current

  while (( SECONDS < deadline )); do
    fail_if_app_crashloops
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

wait_for_dashboard_delta_at_most() {
  local name="$1"
  local metric_func="$2"
  local baseline="$3"
  local max_delta="$4"
  local deadline=$((SECONDS + timeout_seconds))
  local current

  while (( SECONDS < deadline )); do
    fail_if_app_crashloops
    current="$("${metric_func}")"
    if [[ -n "${current}" ]] && awk -v current="${current}" -v baseline="${baseline}" -v max_delta="${max_delta}" 'BEGIN { exit !((current - baseline) <= max_delta) }'; then
      printf '%s %s=%s baseline=%s max_delta=%s\n' "$(date -u +%Y-%m-%dT%H:%M:%SZ)" "${name}" "${current}" "${baseline}" "${max_delta}"
      return 0
    fi
    printf '%s %s=%s waiting_for_delta_at_most=%s baseline=%s\n' "$(date -u +%Y-%m-%dT%H:%M:%SZ)" "${name}" "${current:-empty}" "${max_delta}" "${baseline}"
    sleep "${poll_seconds}"
  done

  echo "timed out waiting for dashboard signal ${name} to stay within ${max_delta} of baseline ${baseline}" >&2
  return 1
}

wait_for_dashboard_delta_or_present() {
  local name="$1"
  local metric_func="$2"
  local baseline="$3"
  local min_delta="$4"
  local min_present="$5"
  local deadline=$((SECONDS + timeout_seconds))
  local current

  while (( SECONDS < deadline )); do
    fail_if_app_crashloops
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
  wait_for_dashboard_at_least "dashboard_minio_ready_pods" dashboard_minio_ready_pods "${desired}"
  wait_for_dashboard_at_most "dashboard_minio_ready_pods" dashboard_minio_ready_pods "${desired}"
  wait_for_dashboard_at_most "dashboard_minio_ready_gap" dashboard_minio_ready_gap 0
  wait_for_dashboard_at_most "dashboard_minio_not_ready_pods" dashboard_minio_not_ready_pods 0
  wait_for_dashboard_at_least "dashboard_minio_api_scrape_up" dashboard_minio_api_scrape_up 1
  wait_for_dashboard_at_least "dashboard_minio_cluster_health" dashboard_minio_cluster_health 1
  wait_for_dashboard_at_least "dashboard_minio_nodes_online" dashboard_minio_nodes_online "${desired}"
  wait_for_dashboard_at_most "dashboard_minio_nodes_offline" dashboard_minio_nodes_offline 0
  wait_for_dashboard_at_most "dashboard_minio_drives_offline" dashboard_minio_drives_offline 0
  wait_for_dashboard_at_most "dashboard_minio_unhealthy" dashboard_minio_unhealthy 0
  wait_for_dashboard_at_most "dashboard_object_storage_client_not_ready_pods" dashboard_object_storage_client_not_ready_pods 0
  baseline_data_object_storage_degraded_seconds="$(dashboard_data_object_storage_degraded_seconds_10m)"
  baseline_data_object_storage_degraded_seconds="${baseline_data_object_storage_degraded_seconds:-0}"
  printf 'baseline_dashboard_data_object_storage_degraded_seconds_10m=%s\n' "${baseline_data_object_storage_degraded_seconds}"
}

wait_for_dashboard_recovery() {
  local baseline_pod_created="$1"
  local recreated_pod="$2"
  local baseline_recreations="$3"
  local baseline_degraded_seconds="$4"
  local baseline_s3_5xx="$5"
  wait_for_dashboard_at_least "dashboard_minio_ready_pods" dashboard_minio_ready_pods "${desired}"
  wait_for_dashboard_at_most "dashboard_minio_ready_pods" dashboard_minio_ready_pods "${desired}"
  wait_for_dashboard_at_most "dashboard_minio_ready_gap" dashboard_minio_ready_gap 0
  wait_for_dashboard_at_most "dashboard_minio_not_ready_pods" dashboard_minio_not_ready_pods 0
  wait_for_dashboard_at_least "dashboard_minio_api_scrape_up" dashboard_minio_api_scrape_up 1
  wait_for_dashboard_at_least "dashboard_minio_cluster_health" dashboard_minio_cluster_health 1
  wait_for_dashboard_at_least "dashboard_minio_nodes_online" dashboard_minio_nodes_online "${desired}"
  wait_for_dashboard_at_most "dashboard_minio_nodes_offline" dashboard_minio_nodes_offline 0
  wait_for_dashboard_at_most "dashboard_minio_drives_offline" dashboard_minio_drives_offline 0
  wait_for_dashboard_at_most "dashboard_minio_unhealthy" dashboard_minio_unhealthy 0
  wait_for_dashboard_at_most "dashboard_data_database_degraded" dashboard_data_database_degraded 0
  wait_for_dashboard_at_most "dashboard_data_messaging_degraded" dashboard_data_messaging_degraded 0
  wait_for_dashboard_at_most "dashboard_data_cache_degraded" dashboard_data_cache_degraded 0
  wait_for_dashboard_at_most "dashboard_data_object_storage_degraded" dashboard_data_object_storage_degraded 0
  wait_for_dashboard_at_most "dashboard_data_app_dependency_clients_degraded" dashboard_data_app_dependency_clients_degraded 0
  wait_for_dashboard_at_most "dashboard_object_storage_client_not_ready_pods" dashboard_object_storage_client_not_ready_pods 0
  wait_for_dashboard_delta_at_least "dashboard_minio_pod_recreations" dashboard_minio_pod_recreations "${baseline_recreations}" 1
  wait_for_dashboard_delta_or_present "dashboard_minio_degraded_seconds_10m" dashboard_minio_degraded_seconds_10m "${baseline_degraded_seconds}" "${min_range_signal_seconds}" "${min_range_signal_seconds}"
  wait_for_dashboard_delta_or_present "dashboard_data_object_storage_degraded_seconds_10m" dashboard_data_object_storage_degraded_seconds_10m "${baseline_data_object_storage_degraded_seconds}" "${min_range_signal_seconds}" "${min_range_signal_seconds}"
  wait_for_dashboard_delta_at_most "dashboard_minio_s3_5xx_10m" dashboard_minio_s3_5xx_10m "${baseline_s3_5xx}" "${max_s3_5xx_delta}"
  wait_for_dashboard_target_pod_recreated "${recreated_pod}" "${baseline_pod_created}"
}

wait_for_dashboard_target_pod_recreated() {
  local recreated_pod="$1"
  local baseline_pod_created="$2"
  local deadline=$((SECONDS + timeout_seconds))
  local current

  while (( SECONDS < deadline )); do
    current="$(dashboard_minio_pod_created_timestamp "${recreated_pod}")"
    if [[ -n "${current}" ]] && float_gt "${current}" "${baseline_pod_created}"; then
      printf '%s dashboard_minio_pod_created{%s}=%s threshold_gt=%s\n' \
        "$(date -u +%Y-%m-%dT%H:%M:%SZ)" "${recreated_pod}" "${current}" "${baseline_pod_created}"
      return 0
    fi
    printf '%s dashboard_minio_pod_created{%s}=%s waiting_for_greater_than=%s\n' \
      "$(date -u +%Y-%m-%dT%H:%M:%SZ)" "${recreated_pod}" "${current:-empty}" "${baseline_pod_created}"
    sleep "${poll_seconds}"
  done

  echo "timed out waiting for dashboard signal kube_pod_created for ${recreated_pod} to change" >&2
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
    echo "platform app pods entered CrashLoopBackOff during MinIO pod loss:" >&2
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

ready_count() {
  kubectl -n "${namespace}" get pods -l "${minio_selector}" -o json \
    | jq '[.items[] | select(.status.phase == "Running") | select([.status.conditions[]? | select(.type == "Ready" and .status == "True")] | length > 0)] | length'
}

desired_count() {
  kubectl -n "${namespace}" get statefulset "${statefulset}" -o jsonpath='{.spec.replicas}'
}

wait_for_ready_count() {
  local expected="$1"
  local deadline=$((SECONDS + timeout_seconds))
  local ready
  while (( SECONDS < deadline )); do
    fail_if_app_crashloops
    ready="$(ready_count)"
    if (( ready == expected )); then
      return 0
    fi
    sleep 5
  done
  echo "timed out waiting for ${statefulset} ready pods to return to ${expected}" >&2
  kubectl -n "${namespace}" get pods -l "${minio_selector}" -o wide >&2 || true
  kubectl -n "${namespace}" get statefulset "${statefulset}" -o wide >&2 || true
  return 1
}

choose_target_pod() {
  kubectl -n "${namespace}" get pods -l "${minio_selector}" -o json \
    | jq -r '
        .items
        | sort_by(.metadata.creationTimestamp)
        | .[0].metadata.name // empty
      '
}

choose_probe_pod() {
  kubectl -n "${namespace}" get pods -l "${minio_selector}" -o json \
    | jq -r '
        .items
        | map(select(.status.phase == "Running"))
        | map(select([.status.conditions[]? | select(.type == "Ready" and .status == "True")] | length > 0))
        | sort_by(.metadata.creationTimestamp)
        | .[0].metadata.name // empty
      '
}

verify_minio_object_api() {
  local phase="$1"
  local access_key secret_key probe_pod key value got
  access_key="$(kubectl -n "${namespace}" get secret "${s3_secret}" -o jsonpath='{.data.accessKeyID}' | base64 -d)"
  secret_key="$(kubectl -n "${namespace}" get secret "${s3_secret}" -o jsonpath='{.data.secretAccessKey}' | base64 -d)"
  probe_pod="$(choose_probe_pod)"
  if [[ -z "${probe_pod}" ]]; then
    echo "could not find a Ready MinIO pod to run the object API probe" >&2
    exit 1
  fi

  key="local-ha/minio-pod-loss-$(date -u +%Y%m%dT%H%M%SZ).txt"
  value="ok-${phase}-${probe_pod}"
  got="$(kubectl -n "${namespace}" exec "${probe_pod}" -c minio -- sh -lc '
    set -e
    export MC_CONFIG_DIR=/tmp/mc-local-ha-probe
    rm -rf "${MC_CONFIG_DIR}"
    mc alias set local "http://'"${minio_service}.${namespace}.svc.cluster.local"'" "$0" "$1" >/dev/null
    printf "%s" "$2" > /tmp/local-ha-minio-probe.txt
    mc cp /tmp/local-ha-minio-probe.txt "local/'"${minio_bucket}"'/$3" >/dev/null
    mc cat "local/'"${minio_bucket}"'/$3"
    mc rm --force "local/'"${minio_bucket}"'/$3" >/dev/null
  ' "${access_key}" "${secret_key}" "${value}" "${key}" | tail -n 1 | tr -d '\r')"

  if [[ "${got}" != "${value}" ]]; then
    printf 'MinIO object API probe failed during %s: got %s want %s\n' "${phase}" "${got}" "${value}" >&2
    exit 1
  fi
  printf 'minio_object_api_%s=ok bucket=%s key=%s probe_pod=%s\n' "${phase}" "${minio_bucket}" "${key}" "${probe_pod}"
}

if [[ -z "${target_pod}" ]]; then
  target_pod="$(choose_target_pod)"
fi

if [[ -z "${target_pod}" ]]; then
  echo "no MinIO tenant pods found with selector ${minio_selector}" >&2
  exit 1
fi

desired="$(desired_count)"
if [[ -z "${desired}" || "${desired}" == *[!0-9]* || "${desired}" == "0" ]]; then
  echo "statefulset ${namespace}/${statefulset} has invalid desired replicas: ${desired:-empty}" >&2
  exit 1
fi

baseline_ready="$(ready_count)"
if (( baseline_ready != desired )); then
  echo "MinIO tenant is not fully ready before the drill: ready=${baseline_ready} desired=${desired}" >&2
  kubectl -n "${namespace}" get pods -l "${minio_selector}" -o wide >&2 || true
  exit 1
fi

wait_for_dashboard_baseline
verify_minio_object_api "baseline"
baseline_pod_created="$(dashboard_minio_pod_created_timestamp "${target_pod}")"
if [[ -z "${baseline_pod_created}" ]] || ! float_gt "${baseline_pod_created}" 0; then
  echo "dashboard kube_pod_created baseline for ${target_pod} is missing" >&2
  exit 1
fi
baseline_recreations="$(dashboard_minio_pod_recreations)"
baseline_recreations="${baseline_recreations:-0}"
baseline_degraded_seconds="$(dashboard_minio_degraded_seconds_10m)"
baseline_degraded_seconds="${baseline_degraded_seconds:-0}"
baseline_object_storage_client_readiness_changes="$(dashboard_object_storage_client_readiness_changes)"
baseline_object_storage_client_readiness_changes="${baseline_object_storage_client_readiness_changes:-0}"
baseline_s3_5xx="$(dashboard_minio_s3_5xx_10m)"
baseline_s3_5xx="${baseline_s3_5xx:-0}"
printf 'baseline_dashboard_minio_pod_created{%s}=%s\n' "${target_pod}" "${baseline_pod_created}"
printf 'baseline_dashboard_minio_pod_recreations=%s\n' "${baseline_recreations}"
printf 'baseline_dashboard_minio_degraded_seconds_10m=%s\n' "${baseline_degraded_seconds}"
printf 'baseline_dashboard_object_storage_client_readiness_changes_10m=%s\n' "${baseline_object_storage_client_readiness_changes}"
printf 'baseline_dashboard_minio_s3_5xx_10m=%s\n' "${baseline_s3_5xx}"

old_uid="$(kubectl -n "${namespace}" get pod "${target_pod}" -o jsonpath='{.metadata.uid}')"
old_node="$(kubectl -n "${namespace}" get pod "${target_pod}" -o jsonpath='{.spec.nodeName}')"

echo "==> deleting MinIO tenant pod ${target_pod} on ${old_node}"
kubectl -n "${namespace}" delete pod "${target_pod}" --wait=false

wait_for_dashboard_at_least "dashboard_minio_unhealthy" dashboard_minio_unhealthy 1
wait_for_dashboard_at_least "dashboard_data_object_storage_degraded" dashboard_data_object_storage_degraded 1
wait_for_dashboard_at_most "dashboard_data_database_degraded" dashboard_data_database_degraded 0
wait_for_dashboard_at_most "dashboard_data_messaging_degraded" dashboard_data_messaging_degraded 0
wait_for_dashboard_at_most "dashboard_data_cache_degraded" dashboard_data_cache_degraded 0
wait_for_dashboard_at_most "dashboard_data_app_dependency_clients_degraded" dashboard_data_app_dependency_clients_degraded 0
wait_for_dashboard_at_most "dashboard_object_storage_client_not_ready_pods" dashboard_object_storage_client_not_ready_pods 0
printf 'fault_dashboard_minio_unhealthy=%s\n' "$(dashboard_minio_unhealthy)"
printf 'fault_dashboard_data_object_storage_degraded=%s\n' "$(dashboard_data_object_storage_degraded)"
printf 'fault_dashboard_object_storage_client_not_ready_pods=%s\n' "$(dashboard_object_storage_client_not_ready_pods)"

echo "==> waiting for ${target_pod} to be replaced"
deadline=$((SECONDS + timeout_seconds))
while (( SECONDS < deadline )); do
  fail_if_app_crashloops
  new_uid="$(kubectl -n "${namespace}" get pod "${target_pod}" -o jsonpath='{.metadata.uid}' 2>/dev/null || true)"
  if [[ -n "${new_uid}" && "${new_uid}" != "${old_uid}" ]]; then
    break
  fi
  sleep 2
done

if [[ -z "${new_uid:-}" || "${new_uid}" == "${old_uid}" ]]; then
  echo "timed out waiting for ${target_pod} replacement UID" >&2
  kubectl -n "${namespace}" get pods -l "${minio_selector}" -o wide >&2 || true
  exit 1
fi

wait_for_ready_count "${desired}"
kubectl -n "${namespace}" rollout status "statefulset/${statefulset}" --timeout="${timeout_seconds}s"
wait_for_dashboard_recovery "${baseline_pod_created}" "${target_pod}" "${baseline_recreations}" "${baseline_degraded_seconds}" "${baseline_s3_5xx}"
verify_minio_object_api "recovery"
recovery_object_storage_client_readiness_changes="$(dashboard_object_storage_client_readiness_changes)"
recovery_object_storage_client_readiness_changes="${recovery_object_storage_client_readiness_changes:-0}"
recovery_s3_5xx="$(dashboard_minio_s3_5xx_10m)"
recovery_s3_5xx="${recovery_s3_5xx:-0}"
printf 'recovery_dashboard_minio_degraded_seconds_10m=%s\n' "$(dashboard_minio_degraded_seconds_10m)"
printf 'recovery_dashboard_data_object_storage_degraded_seconds_10m=%s\n' "$(dashboard_data_object_storage_degraded_seconds_10m)"
printf 'recovery_dashboard_data_object_storage_degraded=%s\n' "$(dashboard_data_object_storage_degraded)"
printf 'recovery_dashboard_object_storage_client_readiness_changes_10m=%s\n' "${recovery_object_storage_client_readiness_changes}"
printf 'recovery_dashboard_minio_s3_5xx_10m=%s\n' "${recovery_s3_5xx}"
assert_no_app_crashloops

new_node="$(kubectl -n "${namespace}" get pod "${target_pod}" -o jsonpath='{.spec.nodeName}')"

echo "==> MinIO pod replacement observed"
printf 'pod\told_uid\tnew_uid\told_node\tnew_node\n'
printf '%s\t%s\t%s\t%s\t%s\n' "${target_pod}" "${old_uid}" "${new_uid}" "${old_node}" "${new_node}"
kubectl -n "${namespace}" get pods -l "${minio_selector}" -o wide
