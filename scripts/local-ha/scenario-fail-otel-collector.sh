#!/usr/bin/env bash
set -euo pipefail

cd "$(dirname "$0")/../.."

namespace="${NAMESPACE:-tbite}"
release="${RELEASE:-tbite}"
deployment="${OTEL_DEPLOYMENT:-${release}-opentelemetry-collector}"
vm_service="${VM_SERVICE:-vmsingle-${release}-victoria-metrics-k8s-stack}"
env_name="${ENV_NAME:-local-ha}"
vm_url="${VM_URL:-}"
vm_local_port="${VM_LOCAL_PORT:-18428}"
baseline_max_age_seconds="${BASELINE_MAX_AGE_SECONDS:-45}"
min_outage_age_seconds="${MIN_OUTAGE_AGE_SECONDS:-55}"
recovered_max_age_seconds="${RECOVERED_MAX_AGE_SECONDS:-45}"
max_k8s_metrics_age_seconds="${MAX_K8S_METRICS_AGE_SECONDS:-45}"
max_log_ingest_age_seconds="${MAX_LOG_INGEST_AGE_SECONDS:-45}"
timeout_seconds="${TIMEOUT_SECONDS:-240}"
restore="${RESTORE:-true}"
port_forward_pid=""

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
    if curl -fsS "${vm_url}/health" >/dev/null 2>&1; then
      return 0
    fi
    if ! kill -0 "${port_forward_pid}" >/dev/null 2>&1; then
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

vmagent_remote_write_fresh_query() {
  local max_age_seconds="$1"
  printf '(((time() - max(max_over_time(timestamp(vmagent_remotewrite_conn_bytes_written_total{namespace="%s"})[2m:15s]))) or vector(999999)) <= bool %s)' "${namespace}" "${max_age_seconds}"
}

telemetry_age_seconds() {
  promql_value "(time() - max(timestamp(http_server_request_duration_seconds_count{deployment_environment=\"${env_name}\"}))) or vector(999999)"
}

k8s_metrics_age_seconds() {
  promql_value "time() - max(timestamp(kube_pod_info{namespace=\"${namespace}\"}))"
}

log_ingest_age_seconds() {
  promql_value '(time() - max(timestamp(vl_rows_ingested_total{type="elasticsearch_bulk"}))) or vector(999999)'
}

dashboard_app_telemetry_stale() {
  promql_value "(((time() - max(timestamp(http_server_request_duration_seconds_count{deployment_environment=\"${env_name}\"}))) or vector(999999)) > bool ${baseline_max_age_seconds})"
}

dashboard_app_telemetry_stale_seconds_10m() {
  promql_value "(sum_over_time((((time() - max(timestamp(http_server_request_duration_seconds_count{deployment_environment=\"${env_name}\"}))) or vector(999999)) > bool ${baseline_max_age_seconds})[10m:15s]) or vector(0)) * 15"
}

dashboard_k8s_inventory_stale() {
  promql_value "(((time() - max(timestamp(kube_pod_info{namespace=\"${namespace}\"}))) or vector(999999)) > bool ${max_k8s_metrics_age_seconds})"
}

dashboard_log_ingest_stale() {
  local vmagent_fresh
  vmagent_fresh="$(vmagent_remote_write_fresh_query "${max_log_ingest_age_seconds}")"
  promql_value "(((time() - max(timestamp(vl_rows_ingested_total{type=\"elasticsearch_bulk\"}))) or vector(999999)) > bool ${max_log_ingest_age_seconds}) * on() ${vmagent_fresh} * on() (((sum(changes(kube_deployment_status_replicas_available{namespace=\"${namespace}\",deployment=~\"vmagent-.*\"}[2m])) or vector(0)) == bool 0))"
}

dashboard_otel_collector_missing() {
  promql_value "((sum(kube_deployment_status_replicas_available{namespace=\"${namespace}\",deployment=\"${deployment}\"}) or vector(0)) < bool 1)"
}

dashboard_otel_collector_missing_seconds_10m() {
  promql_value "(sum_over_time(((sum(kube_deployment_status_replicas_available{namespace=\"${namespace}\",deployment=\"${deployment}\"}) or vector(0)) < bool 1)[10m:15s]) or vector(0)) * 15"
}

export_failures_10m() {
  promql_value "(sum(increase(otelcol_exporter_send_failed_metric_points_total[10m])) or vector(0)) + (sum(increase(otelcol_exporter_send_failed_spans_total[10m])) or vector(0)) + (sum(increase(otelcol_exporter_send_failed_log_records_total[10m])) or vector(0))"
}

available_replicas() {
  kubectl -n "${namespace}" get deployment "${deployment}" -o jsonpath='{.status.availableReplicas}' 2>/dev/null || true
}

endpoint_count() {
  kubectl -n "${namespace}" get endpoints "${deployment}" -o json 2>/dev/null \
    | jq '[.subsets[]?.addresses[]?] | length'
}

wait_for_available_replicas() {
  local expected="$1"
  local deadline=$((SECONDS + timeout_seconds))
  local available
  while (( SECONDS < deadline )); do
    available="$(available_replicas)"
    available="${available:-0}"
    if (( available == expected )); then
      return 0
    fi
    sleep 2
  done

  echo "timed out waiting for ${deployment} availableReplicas=${expected}" >&2
  kubectl -n "${namespace}" get deploy "${deployment}" -o wide >&2 || true
  kubectl -n "${namespace}" get pods -l app.kubernetes.io/name=opentelemetry-collector -o wide >&2 || true
  return 1
}

wait_for_endpoint_count() {
  local expected="$1"
  local deadline=$((SECONDS + timeout_seconds))
  local endpoints
  while (( SECONDS < deadline )); do
    endpoints="$(endpoint_count)"
    if (( endpoints == expected )); then
      return 0
    fi
    sleep 2
  done

  echo "timed out waiting for ${deployment} endpoint count=${expected}" >&2
  kubectl -n "${namespace}" get endpoints "${deployment}" -o yaml >&2 || true
  return 1
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
    sleep 5
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
    sleep 5
  done

  echo "timed out waiting for dashboard signal ${name} <= ${threshold}" >&2
  return 1
}

wait_for_telemetry_age_at_least() {
  local threshold="$1"
  local deadline=$((SECONDS + timeout_seconds))
  local age
  while (( SECONDS < deadline )); do
    age="$(telemetry_age_seconds)"
    if [[ -n "${age}" ]] && float_ge "${age}" "${threshold}"; then
      printf '%s app_telemetry_age_seconds=%s\n' "$(date -u +%Y-%m-%dT%H:%M:%SZ)" "${age}"
      return 0
    fi
    printf '%s app_telemetry_age_seconds=%s waiting_for_age_at_least=%s\n' "$(date -u +%Y-%m-%dT%H:%M:%SZ)" "${age:-empty}" "${threshold}"
    sleep 5
  done

  echo "timed out waiting for app telemetry age to reach ${threshold}s" >&2
  return 1
}

wait_for_telemetry_age_below() {
  local threshold="$1"
  local deadline=$((SECONDS + timeout_seconds))
  local age
  while (( SECONDS < deadline )); do
    age="$(telemetry_age_seconds)"
    if [[ -n "${age}" ]] && float_le "${age}" "${threshold}"; then
      printf '%s app_telemetry_age_seconds=%s\n' "$(date -u +%Y-%m-%dT%H:%M:%SZ)" "${age}"
      return 0
    fi
    printf '%s app_telemetry_age_seconds=%s waiting_for_age_below=%s\n' "$(date -u +%Y-%m-%dT%H:%M:%SZ)" "${age:-empty}" "${threshold}"
    sleep 5
  done

  echo "timed out waiting for app telemetry age to recover below ${threshold}s" >&2
  return 1
}

cleanup() {
  local status="$?"
  if [[ "${restore}" == "true" && -n "${original_replicas:-}" ]]; then
    echo "==> restoring ${namespace}/${deployment} to replicas=${original_replicas}"
    kubectl -n "${namespace}" scale deployment "${deployment}" --replicas="${original_replicas}" >/dev/null || true
    kubectl -n "${namespace}" rollout status "deployment/${deployment}" --timeout="${timeout_seconds}s" || true
    wait_for_available_replicas "${original_replicas}" || true
    wait_for_endpoint_count "${original_replicas}" || true
    wait_for_telemetry_age_below "${recovered_max_age_seconds}" || true
    wait_for_dashboard_at_most "dashboard_otel_collector_missing" dashboard_otel_collector_missing 0 || true
    wait_for_dashboard_at_most "dashboard_app_telemetry_stale" dashboard_app_telemetry_stale 0 || true
    wait_for_dashboard_at_most "dashboard_k8s_inventory_stale" dashboard_k8s_inventory_stale 0 || true
    wait_for_dashboard_at_most "dashboard_log_ingest_stale" dashboard_log_ingest_stale 0 || true
  elif [[ "${restore}" != "true" ]]; then
    echo "RESTORE=${restore}; leaving ${namespace}/${deployment} scaled down"
  fi

  if [[ -n "${port_forward_pid}" ]]; then
    kill "${port_forward_pid}" >/dev/null 2>&1 || true
  fi
  exit "${status}"
}

trap cleanup EXIT

start_vm_port_forward

original_replicas="$(kubectl -n "${namespace}" get deployment "${deployment}" -o jsonpath='{.spec.replicas}')"
original_replicas="${original_replicas:-1}"
if (( original_replicas < 1 )); then
  echo "${namespace}/${deployment} has spec.replicas=${original_replicas}; refusing to mask a pre-existing OTel collector outage" >&2
  exit 1
fi

wait_for_available_replicas "${original_replicas}"
wait_for_endpoint_count "${original_replicas}"
wait_for_dashboard_at_most "dashboard_otel_collector_missing" dashboard_otel_collector_missing 0
wait_for_dashboard_at_most "dashboard_app_telemetry_stale" dashboard_app_telemetry_stale 0
wait_for_dashboard_at_most "dashboard_k8s_inventory_stale" dashboard_k8s_inventory_stale 0
wait_for_dashboard_at_most "dashboard_log_ingest_stale" dashboard_log_ingest_stale 0

baseline_age="$(telemetry_age_seconds)"
baseline_k8s_age="$(k8s_metrics_age_seconds)"
baseline_log_age="$(log_ingest_age_seconds)"
baseline_otel_missing_seconds="$(dashboard_otel_collector_missing_seconds_10m)"
baseline_app_stale_seconds="$(dashboard_app_telemetry_stale_seconds_10m)"
if [[ -z "${baseline_age}" || ! "${baseline_age}" =~ ^[0-9]+([.][0-9]+)?$ ]]; then
  echo "could not read baseline app telemetry age from VictoriaMetrics" >&2
  exit 1
fi
if ! float_le "${baseline_age}" "${baseline_max_age_seconds}"; then
  echo "app telemetry is already stale before the drill: age=${baseline_age}s max=${baseline_max_age_seconds}s" >&2
  exit 1
fi
if ! float_le "${baseline_k8s_age:-999999}" "${max_k8s_metrics_age_seconds}"; then
  echo "Kubernetes inventory metrics are already stale before the drill: age=${baseline_k8s_age:-empty}s" >&2
  exit 1
fi
if ! float_le "${baseline_log_age:-999999}" "${max_log_ingest_age_seconds}"; then
  echo "VictoriaLogs ingest is stale before the drill: age=${baseline_log_age:-empty}s" >&2
  exit 1
fi

echo "==> baseline OTel collector and telemetry state"
printf 'app_telemetry_age_seconds=%s\n' "${baseline_age}"
printf 'k8s_metrics_age_seconds=%s\n' "${baseline_k8s_age}"
printf 'vlogs_ingest_age_seconds=%s\n' "${baseline_log_age}"
printf 'dashboard_otel_collector_missing_seconds_10m=%s\n' "${baseline_otel_missing_seconds:-0}"
printf 'dashboard_app_telemetry_stale_seconds_10m=%s\n' "${baseline_app_stale_seconds:-0}"
printf 'otel_export_failures_10m=%s\n' "$(export_failures_10m)"
kubectl -n "${namespace}" get deploy "${deployment}" -o wide
kubectl -n "${namespace}" get endpoints "${deployment}" -o wide

echo "==> scaling ${namespace}/${deployment} to zero"
kubectl -n "${namespace}" scale deployment "${deployment}" --replicas=0 >/dev/null
wait_for_available_replicas 0
wait_for_endpoint_count 0

echo "==> waiting for app telemetry to become stale"
wait_for_telemetry_age_at_least "${min_outage_age_seconds}"
wait_for_dashboard_at_least "dashboard_otel_collector_missing" dashboard_otel_collector_missing 1
wait_for_dashboard_at_least "dashboard_app_telemetry_stale" dashboard_app_telemetry_stale 1
wait_for_dashboard_at_least "dashboard_otel_collector_missing_seconds_10m" dashboard_otel_collector_missing_seconds_10m 15
wait_for_dashboard_at_least "dashboard_app_telemetry_stale_seconds_10m" dashboard_app_telemetry_stale_seconds_10m 15
wait_for_dashboard_at_most "dashboard_k8s_inventory_stale" dashboard_k8s_inventory_stale 0
wait_for_dashboard_at_most "dashboard_log_ingest_stale" dashboard_log_ingest_stale 0

echo "==> OTel collector outage observed"
printf 'app_telemetry_age_seconds=%s\n' "$(telemetry_age_seconds)"
printf 'k8s_metrics_age_seconds=%s\n' "$(k8s_metrics_age_seconds)"
printf 'vlogs_ingest_age_seconds=%s\n' "$(log_ingest_age_seconds)"
printf 'otel_export_failures_10m=%s\n' "$(export_failures_10m)"
kubectl -n "${namespace}" get deploy "${deployment}" -o wide
kubectl -n "${namespace}" get endpoints "${deployment}" -o wide
