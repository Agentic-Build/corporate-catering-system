#!/usr/bin/env bash
set -euo pipefail

cd "$(dirname "$0")/../.."

namespace="${NAMESPACE:-tbite}"
release="${RELEASE:-tbite}"
deployment="${KUBE_STATE_METRICS_DEPLOYMENT:-${release}-kube-state-metrics}"
vm_service="${VM_SERVICE:-vmsingle-${release}-victoria-metrics-k8s-stack}"
env_name="${ENV_NAME:-local-ha}"
vm_url="${VM_URL:-}"
vm_local_port="${VM_LOCAL_PORT:-18428}"
baseline_max_k8s_age_seconds="${BASELINE_MAX_K8S_AGE_SECONDS:-45}"
min_outage_k8s_age_seconds="${MIN_OUTAGE_K8S_AGE_SECONDS:-55}"
recovered_max_k8s_age_seconds="${RECOVERED_MAX_K8S_AGE_SECONDS:-45}"
max_app_telemetry_age_seconds="${MAX_APP_TELEMETRY_AGE_SECONDS:-45}"
max_vmagent_telemetry_age_seconds="${MAX_VMAGENT_TELEMETRY_AGE_SECONDS:-45}"
max_log_ingest_age_seconds="${MAX_LOG_INGEST_AGE_SECONDS:-45}"
timeout_seconds="${TIMEOUT_SECONDS:-240}"
min_range_signal_seconds="${MIN_RANGE_SIGNAL_SECONDS:-15}"
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

vmagent_remote_write_age_query() {
  printf '(time() - max(max_over_time(timestamp(vmagent_remotewrite_conn_bytes_written_total{namespace="%s"})[2m:15s]))) or vector(999999)' "${namespace}"
}

vmagent_remote_write_fresh_query() {
  local max_age_seconds="$1"
  printf '(((time() - max(max_over_time(timestamp(vmagent_remotewrite_conn_bytes_written_total{namespace="%s"})[2m:15s]))) or vector(999999)) <= bool %s)' "${namespace}" "${max_age_seconds}"
}

vmagent_remote_write_stale_query() {
  local max_age_seconds="$1"
  printf '(((time() - max(max_over_time(timestamp(vmagent_remotewrite_conn_bytes_written_total{namespace="%s"})[2m:15s]))) or vector(999999)) > bool %s)' "${namespace}" "${max_age_seconds}"
}

k8s_metrics_age_seconds() {
  promql_value "time() - max(timestamp(kube_pod_info{namespace=\"${namespace}\"}))"
}

dashboard_k8s_inventory_stale() {
  promql_value "(((time() - max(timestamp(kube_pod_info{namespace=\"${namespace}\"}))) or vector(999999)) > bool ${baseline_max_k8s_age_seconds})"
}

ksm_scrape_up() {
  promql_value "sum(up{namespace=\"${namespace}\",job=\"kube-state-metrics\"}) or vector(0)"
}

dashboard_ksm_scrape_missing() {
  local vmagent_fresh
  vmagent_fresh="$(vmagent_remote_write_fresh_query "${max_vmagent_telemetry_age_seconds}")"
  promql_value "(((sum(up{namespace=\"${namespace}\",job=\"kube-state-metrics\"}) or vector(0)) < bool 1) * on() ${vmagent_fresh})"
}

dashboard_ksm_scrape_missing_seconds_10m() {
  local vmagent_fresh
  vmagent_fresh="$(vmagent_remote_write_fresh_query "${max_vmagent_telemetry_age_seconds}")"
  promql_value "(sum_over_time((((sum(up{namespace=\"${namespace}\",job=\"kube-state-metrics\"}) or vector(0)) < bool 1) * on() ${vmagent_fresh})[10m:15s]) or vector(0)) * 15"
}

app_telemetry_age_seconds() {
  promql_value "(time() - max(timestamp(http_server_request_duration_seconds_count{deployment_environment=\"${env_name}\"}))) or vector(999999)"
}

dashboard_app_telemetry_stale() {
  promql_value "(((time() - max(timestamp(http_server_request_duration_seconds_count{deployment_environment=\"${env_name}\"}))) or vector(999999)) > bool ${max_app_telemetry_age_seconds})"
}

vmagent_telemetry_age_seconds() {
  promql_value "$(vmagent_remote_write_age_query)"
}

dashboard_vmagent_telemetry_stale() {
  promql_value "$(vmagent_remote_write_stale_query "${max_vmagent_telemetry_age_seconds}")"
}

dashboard_observability_component_ready_gap() {
  promql_value "(((sum(kube_deployment_spec_replicas{namespace=\"${namespace}\",deployment=~\"vmagent-.*|vmsingle-.*|vmalert-.*|${release}-grafana|${release}-kube-state-metrics|${release}-opentelemetry-collector|${release}-victoria-metrics-operator\"} - kube_deployment_status_replicas_available{namespace=\"${namespace}\",deployment=~\"vmagent-.*|vmsingle-.*|vmalert-.*|${release}-grafana|${release}-kube-state-metrics|${release}-opentelemetry-collector|${release}-victoria-metrics-operator\"}) or vector(0)) + (sum(kube_statefulset_replicas{namespace=\"${namespace}\",statefulset=~\"${release}-victoria-logs-single-server|${release}-vt-single-server|vmalertmanager-.*\"} - kube_statefulset_status_replicas_ready{namespace=\"${namespace}\",statefulset=~\"${release}-victoria-logs-single-server|${release}-vt-single-server|vmalertmanager-.*\"}) or vector(0)) + (sum(kube_daemonset_status_desired_number_scheduled{namespace=\"${namespace}\",daemonset=~\"${release}-prometheus-node-exporter|${release}-vector\"} - kube_daemonset_status_number_ready{namespace=\"${namespace}\",daemonset=~\"${release}-prometheus-node-exporter|${release}-vector\"}) or vector(0))) * on() ((((time() - max(timestamp(kube_pod_info{namespace=\"${namespace}\"}))) or vector(999999)) <= bool ${baseline_max_k8s_age_seconds})))"
}

dashboard_otel_collector_missing() {
  promql_value "(((sum(kube_deployment_status_replicas_available{namespace=\"${namespace}\",deployment=\"${release}-opentelemetry-collector\"}) or vector(0)) < bool 1) * on() ((((time() - max(timestamp(kube_pod_info{namespace=\"${namespace}\"}))) or vector(999999)) <= bool ${baseline_max_k8s_age_seconds})))"
}

dashboard_traces_backend_missing() {
  promql_value "(((sum(kube_statefulset_status_replicas_ready{namespace=\"${namespace}\",statefulset=\"${release}-vt-single-server\"}) or vector(0)) < bool 1) * on() ((((time() - max(timestamp(kube_pod_info{namespace=\"${namespace}\"}))) or vector(999999)) <= bool ${baseline_max_k8s_age_seconds})))"
}

dashboard_logs_backend_missing() {
  promql_value "(((sum(kube_statefulset_status_replicas_ready{namespace=\"${namespace}\",statefulset=\"${release}-victoria-logs-single-server\"}) or vector(0)) < bool 1) * on() ((((time() - max(timestamp(kube_pod_info{namespace=\"${namespace}\"}))) or vector(999999)) <= bool ${baseline_max_k8s_age_seconds})))"
}

log_ingest_age_seconds() {
  promql_value '(time() - max(timestamp(vl_rows_ingested_total{type="elasticsearch_bulk"}))) or vector(999999)'
}

dashboard_log_ingest_stale() {
  local vmagent_fresh
  vmagent_fresh="$(vmagent_remote_write_fresh_query "${max_log_ingest_age_seconds}")"
  promql_value "(((time() - max(timestamp(vl_rows_ingested_total{type=\"elasticsearch_bulk\"}))) or vector(999999)) > bool ${max_log_ingest_age_seconds}) * on() ${vmagent_fresh} * on() (((sum(changes(kube_deployment_status_replicas_available{namespace=\"${namespace}\",deployment=~\"vmagent-.*\"}[2m])) or vector(0)) == bool 0))"
}

dashboard_k8s_inventory_stale_seconds_10m() {
  promql_value "(sum_over_time((((time() - max(timestamp(kube_pod_info{namespace=\"${namespace}\"}))) or vector(999999)) > bool ${baseline_max_k8s_age_seconds})[10m:15s]) or vector(0)) * 15"
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

  echo "timed out waiting for ${namespace}/${deployment} availableReplicas=${expected}" >&2
  kubectl -n "${namespace}" get deployment "${deployment}" -o wide >&2 || true
  kubectl -n "${namespace}" get pods -l app.kubernetes.io/name=kube-state-metrics -o wide >&2 || true
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

  echo "timed out waiting for ${namespace}/${deployment} endpoint count=${expected}" >&2
  kubectl -n "${namespace}" get endpoints "${deployment}" -o yaml >&2 || true
  return 1
}

wait_for_ksm_scrape_up_at_least() {
  local threshold="$1"
  local deadline=$((SECONDS + timeout_seconds))
  local current
  while (( SECONDS < deadline )); do
    current="$(ksm_scrape_up)"
    if [[ -n "${current}" ]] && float_ge "${current}" "${threshold}"; then
      printf '%s kube_state_metrics_scrape_up=%s threshold=%s\n' "$(date -u +%Y-%m-%dT%H:%M:%SZ)" "${current}" "${threshold}"
      return 0
    fi
    printf '%s kube_state_metrics_scrape_up=%s waiting_for_at_least=%s\n' "$(date -u +%Y-%m-%dT%H:%M:%SZ)" "${current:-empty}" "${threshold}"
    sleep 5
  done

  echo "timed out waiting for kube-state-metrics scrape availability" >&2
  return 1
}

wait_for_ksm_scrape_below() {
  local threshold="$1"
  local deadline=$((SECONDS + timeout_seconds))
  local current
  while (( SECONDS < deadline )); do
    current="$(ksm_scrape_up)"
    if [[ -n "${current}" ]] && float_le "${current}" "${threshold}"; then
      printf '%s kube_state_metrics_scrape_up=%s threshold=%s\n' "$(date -u +%Y-%m-%dT%H:%M:%SZ)" "${current}" "${threshold}"
      return 0
    fi
    printf '%s kube_state_metrics_scrape_up=%s waiting_for_below_or_equal=%s\n' "$(date -u +%Y-%m-%dT%H:%M:%SZ)" "${current:-empty}" "${threshold}"
    sleep 5
  done

  echo "timed out waiting for kube-state-metrics scrape to drop" >&2
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
    sleep 5
  done

  echo "timed out waiting for dashboard signal ${name} to increase by at least ${min_delta} from baseline ${baseline}" >&2
  return 1
}

wait_for_k8s_metrics_age_at_least() {
  local threshold="$1"
  local deadline=$((SECONDS + timeout_seconds))
  local age
  while (( SECONDS < deadline )); do
    age="$(k8s_metrics_age_seconds)"
    if [[ -n "${age}" ]] && float_ge "${age}" "${threshold}"; then
      printf '%s k8s_metrics_age_seconds=%s threshold=%s\n' "$(date -u +%Y-%m-%dT%H:%M:%SZ)" "${age}" "${threshold}"
      return 0
    fi
    printf '%s k8s_metrics_age_seconds=%s waiting_for_at_least=%s\n' "$(date -u +%Y-%m-%dT%H:%M:%SZ)" "${age:-empty}" "${threshold}"
    sleep 5
  done

  echo "timed out waiting for Kubernetes metric age to reach ${threshold}s" >&2
  return 1
}

wait_for_k8s_metrics_age_below() {
  local threshold="$1"
  local deadline=$((SECONDS + timeout_seconds))
  local age
  while (( SECONDS < deadline )); do
    age="$(k8s_metrics_age_seconds)"
    if [[ -n "${age}" ]] && float_le "${age}" "${threshold}"; then
      printf '%s k8s_metrics_age_seconds=%s threshold=%s\n' "$(date -u +%Y-%m-%dT%H:%M:%SZ)" "${age}" "${threshold}"
      return 0
    fi
    printf '%s k8s_metrics_age_seconds=%s waiting_for_at_most=%s\n' "$(date -u +%Y-%m-%dT%H:%M:%SZ)" "${age:-empty}" "${threshold}"
    sleep 5
  done

  echo "timed out waiting for Kubernetes metric age to recover below ${threshold}s" >&2
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
    wait_for_ksm_scrape_up_at_least 1 || true
    wait_for_k8s_metrics_age_below "${recovered_max_k8s_age_seconds}" || true
    wait_for_dashboard_at_most "dashboard_ksm_scrape_missing" dashboard_ksm_scrape_missing 0 || true
    wait_for_dashboard_at_most "dashboard_k8s_inventory_stale" dashboard_k8s_inventory_stale 0 || true
    wait_for_dashboard_at_most "dashboard_app_telemetry_stale" dashboard_app_telemetry_stale 0 || true
    wait_for_dashboard_at_most "dashboard_vmagent_telemetry_stale" dashboard_vmagent_telemetry_stale 0 || true
    wait_for_dashboard_at_most "dashboard_observability_component_ready_gap" dashboard_observability_component_ready_gap 0 || true
    wait_for_dashboard_at_most "dashboard_otel_collector_missing" dashboard_otel_collector_missing 0 || true
    wait_for_dashboard_at_most "dashboard_traces_backend_missing" dashboard_traces_backend_missing 0 || true
    wait_for_dashboard_at_most "dashboard_logs_backend_missing" dashboard_logs_backend_missing 0 || true
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
  echo "${namespace}/${deployment} has spec.replicas=${original_replicas}; refusing to mask a pre-existing kube-state-metrics outage" >&2
  exit 1
fi

wait_for_available_replicas "${original_replicas}"
wait_for_endpoint_count "${original_replicas}"
wait_for_ksm_scrape_up_at_least 1
wait_for_dashboard_at_most "dashboard_ksm_scrape_missing" dashboard_ksm_scrape_missing 0
wait_for_dashboard_at_most "dashboard_k8s_inventory_stale" dashboard_k8s_inventory_stale 0
wait_for_dashboard_at_most "dashboard_app_telemetry_stale" dashboard_app_telemetry_stale 0
wait_for_dashboard_at_most "dashboard_vmagent_telemetry_stale" dashboard_vmagent_telemetry_stale 0
wait_for_dashboard_at_most "dashboard_observability_component_ready_gap" dashboard_observability_component_ready_gap 0
wait_for_dashboard_at_most "dashboard_otel_collector_missing" dashboard_otel_collector_missing 0
wait_for_dashboard_at_most "dashboard_traces_backend_missing" dashboard_traces_backend_missing 0
wait_for_dashboard_at_most "dashboard_logs_backend_missing" dashboard_logs_backend_missing 0
wait_for_dashboard_at_most "dashboard_log_ingest_stale" dashboard_log_ingest_stale 0

baseline_k8s_age="$(k8s_metrics_age_seconds)"
baseline_app_age="$(app_telemetry_age_seconds)"
baseline_vmagent_age="$(vmagent_telemetry_age_seconds)"
baseline_log_age="$(log_ingest_age_seconds)"
baseline_ksm_missing_seconds="$(dashboard_ksm_scrape_missing_seconds_10m)"
baseline_k8s_stale_seconds="$(dashboard_k8s_inventory_stale_seconds_10m)"

if ! float_le "${baseline_k8s_age:-999999}" "${baseline_max_k8s_age_seconds}"; then
  echo "Kubernetes inventory metrics are already stale before the drill: age=${baseline_k8s_age:-empty}s" >&2
  exit 1
fi
if ! float_le "${baseline_app_age:-999999}" "${max_app_telemetry_age_seconds}"; then
  echo "app telemetry is stale before the drill: age=${baseline_app_age:-empty}s" >&2
  exit 1
fi
if ! float_le "${baseline_vmagent_age:-999999}" "${max_vmagent_telemetry_age_seconds}"; then
  echo "vmagent telemetry is stale before the drill: age=${baseline_vmagent_age:-empty}s" >&2
  exit 1
fi
if ! float_le "${baseline_log_age:-999999}" "${max_log_ingest_age_seconds}"; then
  echo "VictoriaLogs ingest is stale before the drill: age=${baseline_log_age:-empty}s" >&2
  exit 1
fi

echo "==> baseline kube-state-metrics and telemetry state"
printf 'kube_state_metrics_scrape_up=%s\n' "$(ksm_scrape_up)"
printf 'k8s_metrics_age_seconds=%s\n' "${baseline_k8s_age}"
printf 'app_telemetry_age_seconds=%s\n' "${baseline_app_age}"
printf 'vmagent_telemetry_age_seconds=%s\n' "${baseline_vmagent_age}"
printf 'vlogs_ingest_age_seconds=%s\n' "${baseline_log_age}"
printf 'dashboard_ksm_scrape_missing_seconds_10m=%s\n' "${baseline_ksm_missing_seconds:-0}"
printf 'dashboard_k8s_inventory_stale_seconds_10m=%s\n' "${baseline_k8s_stale_seconds:-0}"
kubectl -n "${namespace}" get deployment "${deployment}" -o wide
kubectl -n "${namespace}" get endpoints "${deployment}" -o wide

echo "==> scaling ${namespace}/${deployment} to zero"
kubectl -n "${namespace}" scale deployment "${deployment}" --replicas=0 >/dev/null
wait_for_available_replicas 0
wait_for_endpoint_count 0

echo "==> waiting for kube-state-metrics scrape and Kubernetes inventory staleness"
wait_for_ksm_scrape_below 0
wait_for_k8s_metrics_age_at_least "${min_outage_k8s_age_seconds}"
wait_for_dashboard_at_least "dashboard_ksm_scrape_missing" dashboard_ksm_scrape_missing 1
wait_for_dashboard_at_least "dashboard_k8s_inventory_stale" dashboard_k8s_inventory_stale 1
wait_for_dashboard_delta_at_least "dashboard_ksm_scrape_missing_seconds_10m" dashboard_ksm_scrape_missing_seconds_10m "${baseline_ksm_missing_seconds:-0}" "${min_range_signal_seconds}"
wait_for_dashboard_delta_at_least "dashboard_k8s_inventory_stale_seconds_10m" dashboard_k8s_inventory_stale_seconds_10m "${baseline_k8s_stale_seconds:-0}" "${min_range_signal_seconds}"
wait_for_dashboard_at_most "dashboard_app_telemetry_stale" dashboard_app_telemetry_stale 0
wait_for_dashboard_at_most "dashboard_vmagent_telemetry_stale" dashboard_vmagent_telemetry_stale 0
wait_for_dashboard_at_most "dashboard_observability_component_ready_gap" dashboard_observability_component_ready_gap 0
wait_for_dashboard_at_most "dashboard_otel_collector_missing" dashboard_otel_collector_missing 0
wait_for_dashboard_at_most "dashboard_traces_backend_missing" dashboard_traces_backend_missing 0
wait_for_dashboard_at_most "dashboard_logs_backend_missing" dashboard_logs_backend_missing 0
wait_for_dashboard_at_most "dashboard_log_ingest_stale" dashboard_log_ingest_stale 0

app_age_during_outage="$(app_telemetry_age_seconds)"
vmagent_age_during_outage="$(vmagent_telemetry_age_seconds)"
log_age_during_outage="$(log_ingest_age_seconds)"
if ! float_le "${app_age_during_outage:-999999}" "${max_app_telemetry_age_seconds}"; then
  echo "app telemetry became stale during kube-state-metrics-only outage: age=${app_age_during_outage}s" >&2
  exit 1
fi
if ! float_le "${vmagent_age_during_outage:-999999}" "${max_vmagent_telemetry_age_seconds}"; then
  echo "vmagent telemetry became stale during kube-state-metrics-only outage: age=${vmagent_age_during_outage}s" >&2
  exit 1
fi
if ! float_le "${log_age_during_outage:-999999}" "${max_log_ingest_age_seconds}"; then
  echo "VictoriaLogs ingest became stale during kube-state-metrics-only outage: age=${log_age_during_outage}s" >&2
  exit 1
fi

echo "==> kube-state-metrics outage observed"
printf 'kube_state_metrics_scrape_up=%s\n' "$(ksm_scrape_up)"
printf 'k8s_metrics_age_seconds=%s\n' "$(k8s_metrics_age_seconds)"
printf 'app_telemetry_age_seconds=%s\n' "${app_age_during_outage}"
printf 'vmagent_telemetry_age_seconds=%s\n' "${vmagent_age_during_outage}"
printf 'vlogs_ingest_age_seconds=%s\n' "${log_age_during_outage}"
kubectl -n "${namespace}" get deployment "${deployment}" -o wide
kubectl -n "${namespace}" get endpoints "${deployment}" -o wide
