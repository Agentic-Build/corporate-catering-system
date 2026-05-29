#!/usr/bin/env bash
set -euo pipefail

cd "$(dirname "$0")/../.."

namespace="${NAMESPACE:-tbite}"
release="${RELEASE:-tbite}"
statefulset="${VICTORIA_LOGS_STATEFULSET:-${release}-victoria-logs-single-server}"
vm_service="${VM_SERVICE:-vmsingle-${release}-victoria-metrics-k8s-stack}"
env_name="${ENV_NAME:-local-ha}"
vm_url="${VM_URL:-}"
vm_local_port="${VM_LOCAL_PORT:-18428}"
timeout_seconds="${TIMEOUT_SECONDS:-240}"
max_log_ingest_age_seconds="${MAX_LOG_INGEST_AGE_SECONDS:-45}"
max_app_telemetry_age_seconds="${MAX_APP_TELEMETRY_AGE_SECONDS:-45}"
max_k8s_metrics_age_seconds="${MAX_K8S_METRICS_AGE_SECONDS:-45}"
outage_log_ingest_age_seconds="${OUTAGE_LOG_INGEST_AGE_SECONDS:-20}"
min_range_signal_seconds="${MIN_RANGE_SIGNAL_SECONDS:-15}"
restore="${RESTORE:-true}"
log_generator_image="${LOG_GENERATOR_IMAGE:-natsio/nats-box:0.19.5}"
log_generator_pod="${LOG_GENERATOR_POD:-${release}-log-drill-$(date -u +%Y%m%d%H%M%S)}"
port_forward_pid=""

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

vlogs_scrape_up() {
  promql_value "sum(up{namespace=\"${namespace}\",pod=~\"${statefulset}-.*\"}) or vector(0)"
}

logs_ingested_total() {
  promql_value 'sum(vl_rows_ingested_total{type="elasticsearch_bulk"}) or vector(0)'
}

logs_ingest_rate() {
  promql_value 'sum(rate(vl_rows_ingested_total{type="elasticsearch_bulk"}[1m])) or vector(0)'
}

logs_ingest_age_seconds() {
  promql_value '(time() - max(timestamp(vl_rows_ingested_total{type="elasticsearch_bulk"}))) or vector(999999)'
}

app_telemetry_age_seconds() {
  promql_value "(time() - max(timestamp(http_server_request_duration_seconds_count{deployment_environment=\"${env_name}\"}))) or vector(999999)"
}

k8s_metrics_age_seconds() {
  promql_value "time() - max(timestamp(kube_pod_info{namespace=\"${namespace}\"}))"
}

dashboard_log_ingest_stale() {
  local vmagent_fresh
  vmagent_fresh="$(vmagent_remote_write_fresh_query "${max_log_ingest_age_seconds}")"
  promql_value "(((time() - max(timestamp(vl_rows_ingested_total{type=\"elasticsearch_bulk\"}))) or vector(999999)) > bool ${max_log_ingest_age_seconds}) * on() ${vmagent_fresh} * on() (((sum(changes(kube_deployment_status_replicas_available{namespace=\"${namespace}\",deployment=~\"vmagent-.*\"}[2m])) or vector(0)) == bool 0))"
}

dashboard_log_ingest_stale_seconds_10m() {
  local vmagent_fresh
  local log_ingest_stale
  vmagent_fresh="$(vmagent_remote_write_fresh_query "${max_log_ingest_age_seconds}")"
  log_ingest_stale="(((time() - max(timestamp(vl_rows_ingested_total{type=\"elasticsearch_bulk\"}))) or vector(999999)) > bool ${max_log_ingest_age_seconds}) * on() ${vmagent_fresh} * on() (((sum(changes(kube_deployment_status_replicas_available{namespace=\"${namespace}\",deployment=~\"vmagent-.*\"}[2m])) or vector(0)) == bool 0))"
  promql_value "(sum_over_time((${log_ingest_stale})[10m:15s]) or vector(0)) * 15"
}

dashboard_logs_backend_missing() {
  promql_value "((sum(kube_statefulset_status_replicas_ready{namespace=\"${namespace}\",statefulset=\"${release}-victoria-logs-single-server\"}) or vector(0)) < bool 1)"
}

dashboard_logs_backend_missing_seconds_10m() {
  promql_value "(sum_over_time(((sum(kube_statefulset_status_replicas_ready{namespace=\"${namespace}\",statefulset=\"${release}-victoria-logs-single-server\"}) or vector(0)) < bool 1)[10m:15s]) or vector(0)) * 15"
}

dashboard_traces_backend_missing() {
  promql_value "((sum(kube_statefulset_status_replicas_ready{namespace=\"${namespace}\",statefulset=\"${release}-vt-single-server\"}) or vector(0)) < bool 1)"
}

dashboard_app_telemetry_stale() {
  promql_value "(((time() - max(timestamp(http_server_request_duration_seconds_count{deployment_environment=\"${env_name}\"}))) or vector(999999)) > bool ${max_app_telemetry_age_seconds})"
}

dashboard_k8s_inventory_stale() {
  promql_value "(((time() - max(timestamp(kube_pod_info{namespace=\"${namespace}\"}))) or vector(999999)) > bool ${max_k8s_metrics_age_seconds})"
}

dashboard_otel_collector_missing() {
  promql_value "((sum(kube_deployment_status_replicas_available{namespace=\"${namespace}\",deployment=\"${release}-opentelemetry-collector\"}) or vector(0)) < bool 1)"
}

dashboard_trace_delivery_gap_active() {
  promql_value '(clamp_min((sum(rate(otelcol_receiver_accepted_spans_total[1m])) or vector(0)) - (sum(rate(otelcol_exporter_sent_spans_total{exporter="otlp_http/victoria"}[1m])) or vector(0)), 0) > bool 0.1)'
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
  kubectl -n "${namespace}" get pods -l app.kubernetes.io/name=victoria-logs-single -o wide >&2 || true
  return 1
}

wait_for_vlogs_scrape() {
  local expected="$1"
  local deadline=$((SECONDS + timeout_seconds))
  local current
  while (( SECONDS < deadline )); do
    current="$(vlogs_scrape_up)"
    if [[ -n "${current}" ]] && float_ge "${current}" "${expected}"; then
      printf '%s vlogs_scrape_up=%s expected_at_least=%s\n' "$(date -u +%Y-%m-%dT%H:%M:%SZ)" "${current}" "${expected}"
      return 0
    fi
    printf '%s vlogs_scrape_up=%s waiting_for_at_least=%s\n' "$(date -u +%Y-%m-%dT%H:%M:%SZ)" "${current:-empty}" "${expected}"
    sleep 5
  done

  echo "timed out waiting for VictoriaLogs scrape availability" >&2
  return 1
}

wait_for_logs_ingest() {
  local deadline=$((SECONDS + timeout_seconds))
  local current
  while (( SECONDS < deadline )); do
    current="$(logs_ingest_rate)"
    if [[ -n "${current}" ]] && float_gt "${current}" "0"; then
      printf '%s vlogs_ingest_rows_per_second=%s\n' "$(date -u +%Y-%m-%dT%H:%M:%SZ)" "${current}"
      return 0
    fi
    printf '%s vlogs_ingest_rows_per_second=%s waiting_for_above=0\n' "$(date -u +%Y-%m-%dT%H:%M:%SZ)" "${current:-empty}"
    sleep 5
  done

  echo "timed out waiting for VictoriaLogs ingest" >&2
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

wait_for_logs_ingest_age_at_least() {
  local threshold="$1"
  local deadline=$((SECONDS + timeout_seconds))
  local current
  while (( SECONDS < deadline )); do
    current="$(logs_ingest_age_seconds)"
    if [[ -n "${current}" ]] && float_ge "${current}" "${threshold}"; then
      printf '%s vlogs_ingest_age_seconds=%s threshold=%s\n' "$(date -u +%Y-%m-%dT%H:%M:%SZ)" "${current}" "${threshold}"
      return 0
    fi
    printf '%s vlogs_ingest_age_seconds=%s waiting_for_at_least=%s\n' "$(date -u +%Y-%m-%dT%H:%M:%SZ)" "${current:-empty}" "${threshold}"
    sleep 5
  done

  echo "timed out waiting for VictoriaLogs ingest age to reach ${threshold}s" >&2
  return 1
}

wait_for_logs_ingest_age_below() {
  local threshold="$1"
  local deadline=$((SECONDS + timeout_seconds))
  local current
  while (( SECONDS < deadline )); do
    current="$(logs_ingest_age_seconds)"
    if [[ -n "${current}" ]] && float_le "${current}" "${threshold}"; then
      printf '%s vlogs_ingest_age_seconds=%s threshold=%s\n' "$(date -u +%Y-%m-%dT%H:%M:%SZ)" "${current}" "${threshold}"
      return 0
    fi
    printf '%s vlogs_ingest_age_seconds=%s waiting_for_at_most=%s\n' "$(date -u +%Y-%m-%dT%H:%M:%SZ)" "${current:-empty}" "${threshold}"
    sleep 5
  done

  echo "timed out waiting for VictoriaLogs ingest age to recover below ${threshold}s" >&2
  return 1
}

wait_for_logs_ingested_total_above() {
  local baseline="$1"
  local deadline=$((SECONDS + timeout_seconds))
  local current
  while (( SECONDS < deadline )); do
    current="$(logs_ingested_total)"
    if [[ -n "${current}" ]] && float_gt "${current}" "${baseline}"; then
      printf '%s vlogs_rows_ingested_total=%s baseline=%s\n' "$(date -u +%Y-%m-%dT%H:%M:%SZ)" "${current}" "${baseline}"
      return 0
    fi
    printf '%s vlogs_rows_ingested_total=%s waiting_for_above=%s\n' "$(date -u +%Y-%m-%dT%H:%M:%SZ)" "${current:-empty}" "${baseline}"
    sleep 5
  done

  echo "timed out waiting for VictoriaLogs ingest to resume above ${baseline}" >&2
  return 1
}

start_log_generator() {
  kubectl -n "${namespace}" delete pod "${log_generator_pod}" --ignore-not-found >/dev/null
  kubectl -n "${namespace}" run "${log_generator_pod}" \
    --image="${log_generator_image}" \
    --restart=Never \
    --labels="app.kubernetes.io/instance=${release},app.kubernetes.io/name=local-ha-log-drill" \
    --command -- sh -c 'i=0; while [ "$i" -lt 360 ]; do echo "local-ha-log-drill tick=$i ts=$(date -u +%Y-%m-%dT%H:%M:%SZ)"; i=$((i+1)); sleep 1; done' >/dev/null
}

cleanup() {
  local status="$?"
  if [[ "${restore}" == "true" && -n "${original_replicas:-}" ]]; then
    echo "==> restoring ${namespace}/${statefulset} to replicas=${original_replicas}"
    kubectl -n "${namespace}" scale statefulset "${statefulset}" --replicas="${original_replicas}" >/dev/null || true
    kubectl -n "${namespace}" rollout status "statefulset/${statefulset}" --timeout="${timeout_seconds}s" || true
    wait_for_ready_replicas "${original_replicas}" || true
    wait_for_vlogs_scrape 1 || true
    wait_for_logs_ingested_total_above "${restore_ingested_baseline:-0}" || true
    wait_for_logs_ingest_age_below "${max_log_ingest_age_seconds}" || true
    wait_for_dashboard_at_most "dashboard_logs_backend_missing" dashboard_logs_backend_missing 0 || true
    wait_for_dashboard_at_most "dashboard_traces_backend_missing" dashboard_traces_backend_missing 0 || true
    wait_for_dashboard_at_most "dashboard_log_ingest_stale" dashboard_log_ingest_stale 0 || true
    wait_for_dashboard_at_most "dashboard_app_telemetry_stale" dashboard_app_telemetry_stale 0 || true
    wait_for_dashboard_at_most "dashboard_k8s_inventory_stale" dashboard_k8s_inventory_stale 0 || true
    wait_for_dashboard_at_most "dashboard_otel_collector_missing" dashboard_otel_collector_missing 0 || true
    wait_for_dashboard_at_most "dashboard_trace_delivery_gap_active" dashboard_trace_delivery_gap_active 0 || true
  elif [[ "${restore}" != "true" ]]; then
    echo "RESTORE=${restore}; leaving ${namespace}/${statefulset} scaled down"
  fi

  kubectl -n "${namespace}" delete pod "${log_generator_pod}" --ignore-not-found --wait=false >/dev/null || true

  if [[ -n "${port_forward_pid}" ]]; then
    kill "${port_forward_pid}" >/dev/null 2>&1 || true
  fi
  exit "${status}"
}

trap cleanup EXIT

start_vm_port_forward

original_replicas="$(kubectl -n "${namespace}" get statefulset "${statefulset}" -o jsonpath='{.spec.replicas}')"
original_replicas="${original_replicas:-1}"
if (( original_replicas < 1 )); then
  echo "${namespace}/${statefulset} has spec.replicas=${original_replicas}; refusing to mask a pre-existing logs backend outage" >&2
  exit 1
fi

wait_for_ready_replicas "${original_replicas}"
wait_for_vlogs_scrape 1
wait_for_dashboard_at_most "dashboard_logs_backend_missing" dashboard_logs_backend_missing 0
wait_for_dashboard_at_most "dashboard_traces_backend_missing" dashboard_traces_backend_missing 0
wait_for_dashboard_at_most "dashboard_log_ingest_stale" dashboard_log_ingest_stale 0
wait_for_dashboard_at_most "dashboard_app_telemetry_stale" dashboard_app_telemetry_stale 0
wait_for_dashboard_at_most "dashboard_k8s_inventory_stale" dashboard_k8s_inventory_stale 0
wait_for_dashboard_at_most "dashboard_otel_collector_missing" dashboard_otel_collector_missing 0
wait_for_dashboard_at_most "dashboard_trace_delivery_gap_active" dashboard_trace_delivery_gap_active 0
start_log_generator
wait_for_logs_ingest

baseline_ingested="$(logs_ingested_total)"
baseline_ingest_rate="$(logs_ingest_rate)"
baseline_ingest_age="$(logs_ingest_age_seconds)"
baseline_logs_backend_missing_seconds="$(dashboard_logs_backend_missing_seconds_10m)"
baseline_log_ingest_stale_seconds="$(dashboard_log_ingest_stale_seconds_10m)"
baseline_app_age="$(app_telemetry_age_seconds)"
baseline_k8s_age="$(k8s_metrics_age_seconds)"

if ! float_gt "${baseline_ingest_rate:-0}" "0"; then
  echo "VictoriaLogs ingest is not flowing before the drill: ingest_rate=${baseline_ingest_rate:-empty}" >&2
  exit 1
fi
if ! float_le "${baseline_ingest_age:-999999}" "${max_log_ingest_age_seconds}"; then
  echo "VictoriaLogs ingest is stale before the drill: age=${baseline_ingest_age:-empty}s" >&2
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

echo "==> baseline VictoriaLogs ingest state"
printf 'vlogs_scrape_up=%s\n' "$(vlogs_scrape_up)"
printf 'vlogs_rows_ingested_total=%s\n' "${baseline_ingested}"
printf 'vlogs_ingest_rows_per_second=%s\n' "${baseline_ingest_rate}"
printf 'vlogs_ingest_age_seconds=%s\n' "${baseline_ingest_age}"
printf 'logs_backend_missing_seconds_10m=%s\n' "${baseline_logs_backend_missing_seconds}"
printf 'log_ingest_stale_seconds_10m=%s\n' "${baseline_log_ingest_stale_seconds}"
printf 'app_telemetry_age_seconds=%s\n' "${baseline_app_age}"
printf 'k8s_metrics_age_seconds=%s\n' "${baseline_k8s_age}"
kubectl -n "${namespace}" get statefulset "${statefulset}" -o wide

echo "==> scaling ${namespace}/${statefulset} to zero"
kubectl -n "${namespace}" scale statefulset "${statefulset}" --replicas=0 >/dev/null
wait_for_ready_replicas 0

echo "==> waiting for VictoriaLogs ingest staleness"
wait_for_logs_ingest_age_at_least "${outage_log_ingest_age_seconds}"
wait_for_dashboard_at_least "dashboard_logs_backend_missing" dashboard_logs_backend_missing 1
wait_for_dashboard_at_most "dashboard_traces_backend_missing" dashboard_traces_backend_missing 0
wait_for_dashboard_at_least "dashboard_log_ingest_stale" dashboard_log_ingest_stale 1
wait_for_dashboard_at_least "dashboard_logs_backend_missing_seconds_10m" dashboard_logs_backend_missing_seconds_10m "${min_range_signal_seconds}"
wait_for_dashboard_at_least "dashboard_log_ingest_stale_seconds_10m" dashboard_log_ingest_stale_seconds_10m "${min_range_signal_seconds}"
wait_for_dashboard_at_most "dashboard_app_telemetry_stale" dashboard_app_telemetry_stale 0
wait_for_dashboard_at_most "dashboard_k8s_inventory_stale" dashboard_k8s_inventory_stale 0
wait_for_dashboard_at_most "dashboard_otel_collector_missing" dashboard_otel_collector_missing 0
wait_for_dashboard_at_most "dashboard_trace_delivery_gap_active" dashboard_trace_delivery_gap_active 0

app_age_during_outage="$(app_telemetry_age_seconds)"
k8s_age_during_outage="$(k8s_metrics_age_seconds)"
if ! float_le "${app_age_during_outage:-999999}" "${max_app_telemetry_age_seconds}"; then
  echo "app metrics became stale during logs-backend-only outage: age=${app_age_during_outage}s" >&2
  exit 1
fi
if ! float_le "${k8s_age_during_outage:-999999}" "${max_k8s_metrics_age_seconds}"; then
  echo "Kubernetes inventory metrics became stale during logs-backend-only outage: age=${k8s_age_during_outage}s" >&2
  exit 1
fi

echo "==> VictoriaLogs backend outage observed"
printf 'vlogs_scrape_up=%s\n' "$(vlogs_scrape_up)"
printf 'vlogs_ingest_age_seconds=%s\n' "$(logs_ingest_age_seconds)"
printf 'logs_backend_missing_seconds_10m=%s\n' "$(dashboard_logs_backend_missing_seconds_10m)"
printf 'log_ingest_stale_seconds_10m=%s\n' "$(dashboard_log_ingest_stale_seconds_10m)"
printf 'app_telemetry_age_seconds=%s\n' "${app_age_during_outage}"
printf 'k8s_metrics_age_seconds=%s\n' "${k8s_age_during_outage}"
kubectl -n "${namespace}" get statefulset "${statefulset}" -o wide

restore_ingested_baseline="$(logs_ingested_total)"
