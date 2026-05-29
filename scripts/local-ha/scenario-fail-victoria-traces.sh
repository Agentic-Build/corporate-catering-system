#!/usr/bin/env bash
set -euo pipefail

cd "$(dirname "$0")/../.."

namespace="${NAMESPACE:-tbite}"
release="${RELEASE:-tbite}"
statefulset="${VICTORIA_TRACES_STATEFULSET:-${release}-vt-single-server}"
vm_service="${VM_SERVICE:-vmsingle-${release}-victoria-metrics-k8s-stack}"
env_name="${ENV_NAME:-local-ha}"
api_service="${API_SERVICE:-${release}-tbite-platform-api}"
api_url="${API_URL:-}"
api_local_port="${API_LOCAL_PORT:-18080}"
trace_traffic_requests="${TRACE_TRAFFIC_REQUESTS:-60}"
trace_traffic_sleep="${TRACE_TRAFFIC_SLEEP:-0.5}"
vm_url="${VM_URL:-}"
vm_local_port="${VM_LOCAL_PORT:-18428}"
timeout_seconds="${TIMEOUT_SECONDS:-240}"
min_trace_delivery_gap_per_second="${MIN_TRACE_DELIVERY_GAP_PER_SECOND:-0.1}"
min_range_signal_seconds="${MIN_RANGE_SIGNAL_SECONDS:-15}"
max_app_telemetry_age_seconds="${MAX_APP_TELEMETRY_AGE_SECONDS:-45}"
max_k8s_metrics_age_seconds="${MAX_K8S_METRICS_AGE_SECONDS:-45}"
max_log_ingest_age_seconds="${MAX_LOG_INGEST_AGE_SECONDS:-45}"
restore="${RESTORE:-true}"
port_forward_pid=""
api_port_forward_pid=""

float_gt() {
  awk -v left="$1" -v right="$2" 'BEGIN { exit !(left > right) }'
}

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

start_api_port_forward() {
  if [[ -n "${api_url}" ]]; then
    return 0
  fi

  api_url="http://127.0.0.1:${api_local_port}"
  local log_file
  log_file="$(mktemp -t tbite-api-port-forward.XXXXXX.log)"
  kubectl -n "${namespace}" port-forward "svc/${api_service}" "${api_local_port}:80" >"${log_file}" 2>&1 &
  api_port_forward_pid="$!"

  local deadline=$((SECONDS + 20))
  while (( SECONDS < deadline )); do
    if curl -fsS "${api_url}/readyz" >/dev/null 2>&1; then
      return 0
    fi
    if ! kill -0 "${api_port_forward_pid}" >/dev/null 2>&1; then
      cat "${log_file}" >&2 || true
      echo "API port-forward exited before becoming ready" >&2
      exit 1
    fi
    sleep 1
  done

  cat "${log_file}" >&2 || true
  echo "timed out waiting for API port-forward on ${api_url}" >&2
  exit 1
}

generate_trace_traffic() {
  start_api_port_forward

  local success=0
  for _ in $(seq 1 "${trace_traffic_requests}"); do
    if curl -fsS "${api_url}/readyz" >/dev/null 2>&1; then
      success=$((success + 1))
    fi
    sleep "${trace_traffic_sleep}"
  done

  if (( success == 0 )); then
    echo "trace traffic generator could not reach ${api_url}/readyz" >&2
    return 1
  fi
  printf 'trace_traffic_readyz_success=%s\n' "${success}"
}

trace_sent_total() {
  promql_value 'sum(otelcol_exporter_sent_spans_total{exporter="otlp_http/victoria"}) or vector(0)'
}

trace_failed_total() {
  promql_value 'sum(otelcol_exporter_send_failed_spans_total{exporter="otlp_http/victoria"}) or vector(0)'
}

trace_sent_rate() {
  promql_value 'sum(rate(otelcol_exporter_sent_spans_total{exporter="otlp_http/victoria"}[1m])) or vector(0)'
}

trace_delivery_gap_rate() {
  promql_value 'clamp_min((sum(rate(otelcol_receiver_accepted_spans_total[1m])) or vector(0)) - (sum(rate(otelcol_exporter_sent_spans_total{exporter="otlp_http/victoria"}[1m])) or vector(0)), 0)'
}

app_telemetry_age_seconds() {
  promql_value "(time() - max(timestamp(http_server_request_duration_seconds_count{deployment_environment=\"${env_name}\"}))) or vector(999999)"
}

k8s_metrics_age_seconds() {
  promql_value "time() - max(timestamp(kube_pod_info{namespace=\"${namespace}\"}))"
}

log_ingest_age_seconds() {
  promql_value '(time() - max(timestamp(vl_rows_ingested_total{type="elasticsearch_bulk"}))) or vector(999999)'
}

dashboard_trace_delivery_gap_active() {
  promql_value "(clamp_min((sum(rate(otelcol_receiver_accepted_spans_total[1m])) or vector(0)) - (sum(rate(otelcol_exporter_sent_spans_total{exporter=\"otlp_http/victoria\"}[1m])) or vector(0)), 0) > bool ${min_trace_delivery_gap_per_second})"
}

dashboard_trace_delivery_gap_active_seconds_10m() {
  promql_value "(sum_over_time((clamp_min((sum(rate(otelcol_receiver_accepted_spans_total[1m])) or vector(0)) - (sum(rate(otelcol_exporter_sent_spans_total{exporter=\"otlp_http/victoria\"}[1m])) or vector(0)), 0) > bool ${min_trace_delivery_gap_per_second})[10m:15s]) or vector(0)) * 15"
}

dashboard_logs_backend_missing() {
  promql_value "((sum(kube_statefulset_status_replicas_ready{namespace=\"${namespace}\",statefulset=\"${release}-victoria-logs-single-server\"}) or vector(0)) < bool 1)"
}

dashboard_traces_backend_missing() {
  promql_value "((sum(kube_statefulset_status_replicas_ready{namespace=\"${namespace}\",statefulset=\"${release}-vt-single-server\"}) or vector(0)) < bool 1)"
}

dashboard_traces_backend_missing_seconds_10m() {
  promql_value "(sum_over_time(((sum(kube_statefulset_status_replicas_ready{namespace=\"${namespace}\",statefulset=\"${release}-vt-single-server\"}) or vector(0)) < bool 1)[10m:15s]) or vector(0)) * 15"
}

dashboard_app_telemetry_stale() {
  promql_value "(((time() - max(timestamp(http_server_request_duration_seconds_count{deployment_environment=\"${env_name}\"}))) or vector(999999)) > bool ${max_app_telemetry_age_seconds})"
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
  promql_value "((sum(kube_deployment_status_replicas_available{namespace=\"${namespace}\",deployment=\"${release}-opentelemetry-collector\"}) or vector(0)) < bool 1)"
}

statefulset_ready_replicas() {
  kubectl -n "${namespace}" get statefulset "${statefulset}" -o jsonpath='{.status.readyReplicas}' 2>/dev/null || true
}

wait_for_ready_replicas() {
  local expected="$1"
  local deadline=$((SECONDS + timeout_seconds))
  local ready
  while (( SECONDS < deadline )); do
    ready="$(statefulset_ready_replicas)"
    ready="${ready:-0}"
    if (( ready == expected )); then
      return 0
    fi
    sleep 2
  done

  echo "timed out waiting for ${namespace}/${statefulset} readyReplicas=${expected}" >&2
  kubectl -n "${namespace}" get statefulset "${statefulset}" -o wide >&2 || true
  kubectl -n "${namespace}" get pods -l app.kubernetes.io/name=vt-single -o wide >&2 || true
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

wait_for_trace_delivery_gap_at_least() {
  local threshold="$1"
  local deadline=$((SECONDS + timeout_seconds))
  local current
  while (( SECONDS < deadline )); do
    current="$(trace_delivery_gap_rate)"
    if [[ -n "${current}" ]] && float_ge "${current}" "${threshold}"; then
      printf '%s trace_delivery_gap_spans_per_second=%s threshold=%s\n' "$(date -u +%Y-%m-%dT%H:%M:%SZ)" "${current}" "${threshold}"
      return 0
    fi
    printf '%s trace_delivery_gap_spans_per_second=%s waiting_for_at_least=%s\n' "$(date -u +%Y-%m-%dT%H:%M:%SZ)" "${current:-empty}" "${threshold}"
    sleep 5
  done

  echo "timed out waiting for trace delivery gap to reach ${threshold} spans/s" >&2
  return 1
}

wait_for_trace_sent_total_above() {
  local baseline="$1"
  local deadline=$((SECONDS + timeout_seconds))
  local current
  while (( SECONDS < deadline )); do
    current="$(trace_sent_total)"
    if [[ -n "${current}" ]] && float_gt "${current}" "${baseline}"; then
      printf '%s trace_export_sent_spans_total=%s baseline=%s\n' "$(date -u +%Y-%m-%dT%H:%M:%SZ)" "${current}" "${baseline}"
      return 0
    fi
    printf '%s trace_export_sent_spans_total=%s waiting_for_above=%s\n' "$(date -u +%Y-%m-%dT%H:%M:%SZ)" "${current:-empty}" "${baseline}"
    sleep 5
  done

  echo "timed out waiting for trace exports to resume above ${baseline}" >&2
  return 1
}

cleanup() {
  local status="$?"
  if [[ "${restore}" == "true" && -n "${original_replicas:-}" ]]; then
    echo "==> restoring ${namespace}/${statefulset} to replicas=${original_replicas}"
    kubectl -n "${namespace}" scale statefulset "${statefulset}" --replicas="${original_replicas}" >/dev/null || true
    kubectl -n "${namespace}" rollout status "statefulset/${statefulset}" --timeout="${timeout_seconds}s" || true
    wait_for_ready_replicas "${original_replicas}" || true
    generate_trace_traffic || true
    wait_for_trace_sent_total_above "${restore_sent_baseline:-0}" || true
    wait_for_dashboard_at_most "dashboard_logs_backend_missing" dashboard_logs_backend_missing 0 || true
    wait_for_dashboard_at_most "dashboard_traces_backend_missing" dashboard_traces_backend_missing 0 || true
    wait_for_dashboard_at_most "dashboard_trace_delivery_gap_active" dashboard_trace_delivery_gap_active 0 || true
    wait_for_dashboard_at_most "dashboard_app_telemetry_stale" dashboard_app_telemetry_stale 0 || true
    wait_for_dashboard_at_most "dashboard_k8s_inventory_stale" dashboard_k8s_inventory_stale 0 || true
    wait_for_dashboard_at_most "dashboard_log_ingest_stale" dashboard_log_ingest_stale 0 || true
    wait_for_dashboard_at_most "dashboard_otel_collector_missing" dashboard_otel_collector_missing 0 || true
  elif [[ "${restore}" != "true" ]]; then
    echo "RESTORE=${restore}; leaving ${namespace}/${statefulset} scaled down"
  fi

  if [[ -n "${port_forward_pid}" ]]; then
    kill "${port_forward_pid}" >/dev/null 2>&1 || true
  fi
  if [[ -n "${api_port_forward_pid}" ]]; then
    kill "${api_port_forward_pid}" >/dev/null 2>&1 || true
  fi
  exit "${status}"
}

trap cleanup EXIT

start_vm_port_forward

original_replicas="$(kubectl -n "${namespace}" get statefulset "${statefulset}" -o jsonpath='{.spec.replicas}')"
original_replicas="${original_replicas:-1}"
if (( original_replicas < 1 )); then
  echo "${namespace}/${statefulset} has spec.replicas=${original_replicas}; refusing to mask a pre-existing trace backend outage" >&2
  exit 1
fi

wait_for_ready_replicas "${original_replicas}"
generate_trace_traffic
wait_for_dashboard_at_most "dashboard_logs_backend_missing" dashboard_logs_backend_missing 0
wait_for_dashboard_at_most "dashboard_traces_backend_missing" dashboard_traces_backend_missing 0
wait_for_dashboard_at_most "dashboard_trace_delivery_gap_active" dashboard_trace_delivery_gap_active 0
wait_for_dashboard_at_most "dashboard_app_telemetry_stale" dashboard_app_telemetry_stale 0
wait_for_dashboard_at_most "dashboard_k8s_inventory_stale" dashboard_k8s_inventory_stale 0
wait_for_dashboard_at_most "dashboard_log_ingest_stale" dashboard_log_ingest_stale 0
wait_for_dashboard_at_most "dashboard_otel_collector_missing" dashboard_otel_collector_missing 0

baseline_sent="$(trace_sent_total)"
baseline_failed="$(trace_failed_total)"
baseline_sent_rate="$(trace_sent_rate)"
baseline_delivery_gap="$(trace_delivery_gap_rate)"
baseline_traces_backend_missing_seconds="$(dashboard_traces_backend_missing_seconds_10m)"
baseline_trace_gap_active_seconds="$(dashboard_trace_delivery_gap_active_seconds_10m)"
baseline_app_age="$(app_telemetry_age_seconds)"
baseline_k8s_age="$(k8s_metrics_age_seconds)"
baseline_log_age="$(log_ingest_age_seconds)"

if ! float_gt "${baseline_sent_rate:-0}" "0"; then
  echo "trace exports are not flowing before the drill: sent_rate=${baseline_sent_rate:-empty}" >&2
  exit 1
fi
if ! float_le "${baseline_delivery_gap:-999999}" "${min_trace_delivery_gap_per_second}"; then
  echo "trace delivery gap is already elevated before the drill: gap=${baseline_delivery_gap:-empty} spans/s" >&2
  exit 1
fi
if ! float_le "${baseline_app_age:-999999}" "${max_app_telemetry_age_seconds}"; then
  echo "app telemetry is stale before the drill: age=${baseline_app_age:-empty}s" >&2
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

echo "==> baseline VictoriaTraces and trace export state"
printf 'trace_export_sent_spans_total=%s\n' "${baseline_sent}"
printf 'trace_export_failed_spans_total=%s\n' "${baseline_failed}"
printf 'trace_export_sent_spans_per_second=%s\n' "${baseline_sent_rate}"
printf 'trace_delivery_gap_spans_per_second=%s\n' "${baseline_delivery_gap}"
printf 'traces_backend_missing_seconds_10m=%s\n' "${baseline_traces_backend_missing_seconds}"
printf 'trace_delivery_gap_active_seconds_10m=%s\n' "${baseline_trace_gap_active_seconds}"
printf 'app_telemetry_age_seconds=%s\n' "${baseline_app_age}"
printf 'k8s_metrics_age_seconds=%s\n' "${baseline_k8s_age}"
printf 'vlogs_ingest_age_seconds=%s\n' "${baseline_log_age}"
kubectl -n "${namespace}" get statefulset "${statefulset}" -o wide

echo "==> scaling ${namespace}/${statefulset} to zero"
kubectl -n "${namespace}" scale statefulset "${statefulset}" --replicas=0 >/dev/null
wait_for_ready_replicas 0
generate_trace_traffic

echo "==> waiting for OTel trace delivery gap"
wait_for_trace_delivery_gap_at_least "${min_trace_delivery_gap_per_second}"
wait_for_dashboard_at_least "dashboard_traces_backend_missing" dashboard_traces_backend_missing 1
wait_for_dashboard_at_most "dashboard_logs_backend_missing" dashboard_logs_backend_missing 0
wait_for_dashboard_at_least "dashboard_trace_delivery_gap_active" dashboard_trace_delivery_gap_active 1
wait_for_dashboard_at_least "dashboard_traces_backend_missing_seconds_10m" dashboard_traces_backend_missing_seconds_10m "${min_range_signal_seconds}"
wait_for_dashboard_at_least "dashboard_trace_delivery_gap_active_seconds_10m" dashboard_trace_delivery_gap_active_seconds_10m "${min_range_signal_seconds}"
wait_for_dashboard_at_most "dashboard_app_telemetry_stale" dashboard_app_telemetry_stale 0
wait_for_dashboard_at_most "dashboard_k8s_inventory_stale" dashboard_k8s_inventory_stale 0
wait_for_dashboard_at_most "dashboard_log_ingest_stale" dashboard_log_ingest_stale 0
wait_for_dashboard_at_most "dashboard_otel_collector_missing" dashboard_otel_collector_missing 0

app_age_during_outage="$(app_telemetry_age_seconds)"
k8s_age_during_outage="$(k8s_metrics_age_seconds)"
log_age_during_outage="$(log_ingest_age_seconds)"
if ! float_le "${app_age_during_outage:-999999}" "${max_app_telemetry_age_seconds}"; then
  echo "app metrics became stale during trace-backend-only outage: age=${app_age_during_outage}s" >&2
  exit 1
fi
if ! float_le "${k8s_age_during_outage:-999999}" "${max_k8s_metrics_age_seconds}"; then
  echo "Kubernetes inventory metrics became stale during trace-backend-only outage: age=${k8s_age_during_outage}s" >&2
  exit 1
fi
if ! float_le "${log_age_during_outage:-999999}" "${max_log_ingest_age_seconds}"; then
  echo "VictoriaLogs ingest became stale during trace-backend-only outage: age=${log_age_during_outage}s" >&2
  exit 1
fi

echo "==> VictoriaTraces backend outage observed"
printf 'trace_delivery_gap_spans_per_second=%s\n' "$(trace_delivery_gap_rate)"
printf 'trace_export_failed_spans_total=%s\n' "$(trace_failed_total)"
printf 'traces_backend_missing_seconds_10m=%s\n' "$(dashboard_traces_backend_missing_seconds_10m)"
printf 'trace_delivery_gap_active_seconds_10m=%s\n' "$(dashboard_trace_delivery_gap_active_seconds_10m)"
printf 'app_telemetry_age_seconds=%s\n' "${app_age_during_outage}"
printf 'k8s_metrics_age_seconds=%s\n' "${k8s_age_during_outage}"
printf 'vlogs_ingest_age_seconds=%s\n' "${log_age_during_outage}"
kubectl -n "${namespace}" get statefulset "${statefulset}" -o wide

restore_sent_baseline="$(trace_sent_total)"
