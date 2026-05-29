#!/usr/bin/env bash
set -euo pipefail

cd "$(dirname "$0")/../.."

namespace="${NAMESPACE:-tbite}"
release="${RELEASE:-tbite}"
deployment="${VMALERT_DEPLOYMENT:-vmalert-${release}-victoria-metrics-k8s-stack}"
alertmanager_statefulset="${VMALERTMANAGER_STATEFULSET:-vmalertmanager-${release}-victoria-metrics-k8s-stack}"
env_name="${ENV_NAME:-local-ha}"
vm_service="${VM_SERVICE:-vmsingle-${release}-victoria-metrics-k8s-stack}"
vm_url="${VM_URL:-}"
vm_local_port="${VM_LOCAL_PORT:-18428}"
timeout_seconds="${TIMEOUT_SECONDS:-240}"
poll_seconds="${POLL_SECONDS:-5}"
stale_threshold_seconds="${STALE_THRESHOLD_SECONDS:-45}"
min_range_signal_seconds="${MIN_RANGE_SIGNAL_SECONDS:-15}"
restore="${RESTORE:-true}"
port_forward_pid=""
original_replicas=""

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

restore_vmalert() {
  if [[ "${restore}" != "true" || -z "${original_replicas}" ]]; then
    return 0
  fi

  echo "==> restoring ${namespace}/${deployment} to replicas=${original_replicas}"
  kubectl -n "${namespace}" scale "deployment/${deployment}" --replicas="${original_replicas}" >/dev/null || true
  kubectl -n "${namespace}" rollout status "deployment/${deployment}" --timeout="${timeout_seconds}s" || true
}

cleanup() {
  local status="$?"
  restore_vmalert
  if [[ -n "${port_forward_pid}" ]]; then
    kill "${port_forward_pid}" >/dev/null 2>&1 || true
  fi
  exit "${status}"
}
trap cleanup EXIT

promql_value() {
  local query="$1"
  curl -fsS --get "${vm_url}/api/v1/query" --data-urlencode "query=${query}" \
    | jq -r '.data.result[0].value[1] // empty'
}

k8s_fresh_query() {
  printf '(((time() - max(timestamp(kube_pod_info{namespace="%s"}))) or vector(999999)) <= bool %s)' \
    "${namespace}" "${stale_threshold_seconds}"
}

vmagent_fresh_query() {
  printf '(((time() - max(max_over_time(timestamp(vmagent_remotewrite_conn_bytes_written_total{namespace="%s"})[2m:15s]))) or vector(999999)) <= bool %s)' \
    "${namespace}" "${stale_threshold_seconds}"
}

dashboard_vmalert_available() {
  promql_value "sum(max by (namespace, deployment) (kube_deployment_status_replicas_available{namespace=\"${namespace}\",deployment=\"${deployment}\"})) or vector(0)"
}

dashboard_alertmanager_ready() {
  promql_value "sum(max by (namespace, statefulset) (kube_statefulset_status_replicas_ready{namespace=\"${namespace}\",statefulset=\"${alertmanager_statefulset}\"})) or vector(0)"
}

dashboard_alerting_unavailable() {
  local k8s_fresh
  k8s_fresh="$(k8s_fresh_query)"
  promql_value "(((((sum(max by (namespace, deployment) (kube_deployment_status_replicas_available{namespace=\"${namespace}\",deployment=~\"vmalert-.*\"})) or vector(0)) < bool 1) + ((sum(max by (namespace, statefulset) (kube_statefulset_status_replicas_ready{namespace=\"${namespace}\",statefulset=~\"vmalertmanager-.*\"})) or vector(0)) < bool 1)) > bool 0) * on() ${k8s_fresh})"
}

dashboard_alerting_unavailable_seconds_10m() {
  local k8s_fresh
  k8s_fresh="$(k8s_fresh_query)"
  promql_value "(sum_over_time(((((((sum(max by (namespace, deployment) (kube_deployment_status_replicas_available{namespace=\"${namespace}\",deployment=~\"vmalert-.*\"})) or vector(0)) < bool 1) + ((sum(max by (namespace, statefulset) (kube_statefulset_status_replicas_ready{namespace=\"${namespace}\",statefulset=~\"vmalertmanager-.*\"})) or vector(0)) < bool 1)) > bool 0) * on() ${k8s_fresh}))[10m:15s]) or vector(0)) * 15"
}

dashboard_metrics_ingestion_degraded() {
  local vmagent_fresh
  vmagent_fresh="$(vmagent_fresh_query)"
  promql_value "(((((time() - max(timestamp(kube_pod_info{namespace=\"${namespace}\"}))) or vector(999999)) > bool ${stale_threshold_seconds}) + (((time() - max(max_over_time(timestamp(vmagent_remotewrite_conn_bytes_written_total{namespace=\"${namespace}\"})[2m:15s]))) or vector(999999)) > bool ${stale_threshold_seconds}) + (((sum(up{namespace=\"${namespace}\",job=\"kube-state-metrics\"}) or vector(0)) < bool 1) * on() ${vmagent_fresh})) > bool 0)"
}

dashboard_app_telemetry_stale() {
  promql_value "(((time() - max(timestamp(http_server_request_duration_seconds_count{deployment_environment=~\"${env_name}\"}))) or vector(999999)) > bool ${stale_threshold_seconds})"
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

start_vm_port_forward

original_replicas="$(kubectl -n "${namespace}" get "deployment/${deployment}" -o jsonpath='{.spec.replicas}')"
original_replicas="${original_replicas:-1}"

kubectl -n "${namespace}" rollout status "deployment/${deployment}" --timeout="${timeout_seconds}s"
wait_for_dashboard_at_least "dashboard_vmalert_available" dashboard_vmalert_available "${original_replicas}"
wait_for_dashboard_at_least "dashboard_alertmanager_ready" dashboard_alertmanager_ready 1
wait_for_dashboard_at_most "dashboard_alerting_unavailable" dashboard_alerting_unavailable 0
wait_for_dashboard_at_most "dashboard_metrics_ingestion_degraded" dashboard_metrics_ingestion_degraded 0
wait_for_dashboard_at_most "dashboard_app_telemetry_stale" dashboard_app_telemetry_stale 0
baseline_alerting_unavailable_seconds="$(dashboard_alerting_unavailable_seconds_10m)"

echo "==> scaling ${namespace}/${deployment} to zero"
kubectl -n "${namespace}" scale "deployment/${deployment}" --replicas=0
wait_for_dashboard_at_most "dashboard_vmalert_available" dashboard_vmalert_available 0
wait_for_dashboard_at_least "dashboard_alerting_unavailable" dashboard_alerting_unavailable 1
wait_for_dashboard_delta_at_least "dashboard_alerting_unavailable_seconds_10m" dashboard_alerting_unavailable_seconds_10m "${baseline_alerting_unavailable_seconds}" "${min_range_signal_seconds}"
wait_for_dashboard_at_most "dashboard_metrics_ingestion_degraded" dashboard_metrics_ingestion_degraded 0
wait_for_dashboard_at_most "dashboard_app_telemetry_stale" dashboard_app_telemetry_stale 0

echo "==> restoring ${namespace}/${deployment}"
kubectl -n "${namespace}" scale "deployment/${deployment}" --replicas="${original_replicas}"
kubectl -n "${namespace}" rollout status "deployment/${deployment}" --timeout="${timeout_seconds}s"
wait_for_dashboard_at_least "dashboard_vmalert_available" dashboard_vmalert_available "${original_replicas}"
wait_for_dashboard_at_most "dashboard_alerting_unavailable" dashboard_alerting_unavailable 0

restore=false
printf 'dashboard_alerting_unavailable_seconds_10m=%s\n' "$(dashboard_alerting_unavailable_seconds_10m)"
printf 'dashboard_metrics_ingestion_degraded=%s\n' "$(dashboard_metrics_ingestion_degraded)"
printf 'dashboard_app_telemetry_stale=%s\n' "$(dashboard_app_telemetry_stale)"
kubectl -n "${namespace}" get deploy "${deployment}" -o wide
kubectl -n "${namespace}" get statefulset "${alertmanager_statefulset}" -o wide
