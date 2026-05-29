#!/usr/bin/env bash
set -euo pipefail

cd "$(dirname "$0")/../.."

namespace="${NAMESPACE:-tbite}"
release="${RELEASE:-tbite}"
deployment_environment="${DEPLOYMENT_ENVIRONMENT:-local-ha}"
deployment="${VMAGENT_DEPLOYMENT:-}"
vmagent_cr="${VMAGENT_CR:-${release}-victoria-metrics-k8s-stack}"
target_pod="${POD:-}"
failure_mode="${FAILURE_MODE:-ingestion-outage}"
timeout_seconds="${TIMEOUT_SECONDS:-240}"
poll_seconds="${POLL_SECONDS:-2}"
vm_service="${VM_SERVICE:-vmsingle-${release}-victoria-metrics-k8s-stack}"
vm_url="${VM_URL:-}"
vm_local_port="${VM_LOCAL_PORT:-18428}"
max_k8s_metrics_age="${MAX_K8S_METRICS_AGE_SECONDS:-90}"
max_vmagent_telemetry_age="${MAX_VMAGENT_TELEMETRY_AGE_SECONDS:-90}"
max_app_telemetry_age="${MAX_APP_TELEMETRY_AGE_SECONDS:-90}"
max_log_ingest_age="${MAX_LOG_INGEST_AGE_SECONDS:-90}"
stale_threshold_seconds="${STALE_THRESHOLD_SECONDS:-45}"
min_range_signal_seconds="${MIN_RANGE_SIGNAL_SECONDS:-15}"
port_forward_pid=""
original_replica_count=""
restore_vmagent_on_exit="false"

selector="app.kubernetes.io/name=vmagent,managed-by=vm-operator"
observability_pod_regex="vmagent-.*|vmsingle-.*|vmalert-.*|vmalertmanager-.*|${release}-grafana-.*|${release}-kube-state-metrics-.*|${release}-opentelemetry-collector-.*|${release}-victoria-metrics-operator-.*|${release}-victoria-logs-single-server-.*|${release}-vt-single-server-.*|${release}-prometheus-node-exporter-.*|${release}-vector-.*"

if [[ "${timeout_seconds}" == *[!0-9]* || "${poll_seconds}" == *[!0-9]* ]]; then
  echo "TIMEOUT_SECONDS and POLL_SECONDS must be integer seconds" >&2
  exit 2
fi

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

restore_vmagent_replica_count() {
  if [[ "${restore_vmagent_on_exit}" != "true" ]]; then
    return 0
  fi
  if ! kubectl -n "${namespace}" get vmagent "${vmagent_cr}" >/dev/null 2>&1; then
    return 0
  fi

  if [[ -n "${original_replica_count}" ]]; then
    kubectl -n "${namespace}" patch vmagent "${vmagent_cr}" --type=merge \
      -p "{\"spec\":{\"replicaCount\":${original_replica_count}}}" >/dev/null || true
  else
    kubectl -n "${namespace}" patch vmagent "${vmagent_cr}" --type=json \
      -p '[{"op":"remove","path":"/spec/replicaCount"}]' >/dev/null 2>&1 || true
  fi
}

cleanup() {
  if [[ -n "${port_forward_pid}" ]]; then
    kill "${port_forward_pid}" >/dev/null 2>&1 || true
  fi
  restore_vmagent_replica_count
}
trap cleanup EXIT

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

desired_replicas() {
  kubectl -n "${namespace}" get deploy "${deployment}" -o jsonpath='{.spec.replicas}'
}

dashboard_k8s_metrics_age() {
  promql_value "(time() - max(timestamp(kube_pod_info{namespace=\"${namespace}\"}))) or vector(999999)"
}

dashboard_k8s_inventory_stale() {
  promql_value "(((time() - max(timestamp(kube_pod_info{namespace=\"${namespace}\"}))) or vector(999999)) > bool ${stale_threshold_seconds})"
}

dashboard_vmagent_telemetry_age() {
  promql_value "$(vmagent_remote_write_age_query)"
}

dashboard_vmagent_telemetry_stale() {
  promql_value "$(vmagent_remote_write_stale_query "${stale_threshold_seconds}")"
}

dashboard_app_telemetry_age() {
  promql_value "(time() - max(timestamp(http_server_request_duration_seconds_count{deployment_environment=~\"${deployment_environment}\"}))) or vector(999999)"
}

dashboard_app_telemetry_stale() {
  promql_value "(((time() - max(timestamp(http_server_request_duration_seconds_count{deployment_environment=~\"${deployment_environment}\"}))) or vector(999999)) > bool ${stale_threshold_seconds})"
}

dashboard_log_ingest_age() {
  promql_value "(time() - max(timestamp(vl_rows_ingested_total{type=\"elasticsearch_bulk\"}))) or vector(999999)"
}

dashboard_log_ingest_stale() {
  local vmagent_fresh
  vmagent_fresh="$(vmagent_remote_write_fresh_query "${stale_threshold_seconds}")"
  promql_value "(((time() - max(timestamp(vl_rows_ingested_total{type=\"elasticsearch_bulk\"}))) or vector(999999)) > bool ${stale_threshold_seconds}) * on() ${vmagent_fresh} * on() (((sum(changes(kube_deployment_status_replicas_available{namespace=\"${namespace}\",deployment=~\"vmagent-.*\"}[2m])) or vector(0)) == bool 0))"
}

dashboard_vmagent_available() {
  promql_value "sum(kube_deployment_status_replicas_available{namespace=\"${namespace}\",deployment=\"${deployment}\"}) or vector(0)"
}

dashboard_observability_ready_gap() {
  promql_value "(((sum(kube_deployment_spec_replicas{namespace=\"${namespace}\",deployment=~\"vmagent-.*|vmsingle-.*|vmalert-.*|${release}-grafana|${release}-kube-state-metrics|${release}-opentelemetry-collector|${release}-victoria-metrics-operator\"} - kube_deployment_status_replicas_available{namespace=\"${namespace}\",deployment=~\"vmagent-.*|vmsingle-.*|vmalert-.*|${release}-grafana|${release}-kube-state-metrics|${release}-opentelemetry-collector|${release}-victoria-metrics-operator\"}) or vector(0)) + (sum(kube_statefulset_replicas{namespace=\"${namespace}\",statefulset=~\"${release}-victoria-logs-single-server|${release}-vt-single-server|vmalertmanager-.*\"} - kube_statefulset_status_replicas_ready{namespace=\"${namespace}\",statefulset=~\"${release}-victoria-logs-single-server|${release}-vt-single-server|vmalertmanager-.*\"}) or vector(0)) + (sum(kube_daemonset_status_desired_number_scheduled{namespace=\"${namespace}\",daemonset=~\"${release}-prometheus-node-exporter|${release}-vector\"} - kube_daemonset_status_number_ready{namespace=\"${namespace}\",daemonset=~\"${release}-prometheus-node-exporter|${release}-vector\"}) or vector(0))) * on() (((time() - max(timestamp(kube_pod_info{namespace=\"${namespace}\"}))) or vector(999999)) <= bool ${stale_threshold_seconds}))"
}

dashboard_kube_state_metrics_scrape_up() {
  promql_value "sum(up{namespace=\"${namespace}\",job=\"kube-state-metrics\"}) or vector(0)"
}

dashboard_kube_state_metrics_scrape_missing() {
  local vmagent_fresh
  vmagent_fresh="$(vmagent_remote_write_fresh_query "${stale_threshold_seconds}")"
  promql_value "(((sum(up{namespace=\"${namespace}\",job=\"kube-state-metrics\"}) or vector(0)) < bool 1) * on() ${vmagent_fresh})"
}

dashboard_otel_collector_missing() {
  promql_value "(((sum(kube_deployment_status_replicas_available{namespace=\"${namespace}\",deployment=\"${release}-opentelemetry-collector\"}) or vector(0)) < bool 1) * on() (((time() - max(timestamp(kube_pod_info{namespace=\"${namespace}\"}))) or vector(999999)) <= bool ${stale_threshold_seconds}))"
}

dashboard_logs_backend_missing() {
  promql_value "(((sum(kube_statefulset_status_replicas_ready{namespace=\"${namespace}\",statefulset=\"${release}-victoria-logs-single-server\"}) or vector(0)) < bool 1) * on() (((time() - max(timestamp(kube_pod_info{namespace=\"${namespace}\"}))) or vector(999999)) <= bool ${stale_threshold_seconds}))"
}

dashboard_traces_backend_missing() {
  promql_value "(((sum(kube_statefulset_status_replicas_ready{namespace=\"${namespace}\",statefulset=\"${release}-vt-single-server\"}) or vector(0)) < bool 1) * on() (((time() - max(timestamp(kube_pod_info{namespace=\"${namespace}\"}))) or vector(999999)) <= bool ${stale_threshold_seconds}))"
}

dashboard_observability_pod_recreations() {
  promql_value "sum(changes(kube_pod_created{namespace=\"${namespace}\",pod=~\"${observability_pod_regex}\"}[10m])) or vector(0)"
}

dashboard_vmagent_telemetry_stale_seconds_10m() {
  promql_value "(sum_over_time(($(vmagent_remote_write_stale_query "${stale_threshold_seconds}"))[10m:15s]) or vector(0)) * 15"
}

dashboard_k8s_inventory_stale_seconds_10m() {
  promql_value "(sum_over_time((((time() - max(timestamp(kube_pod_info{namespace=\"${namespace}\"}))) or vector(999999)) > bool ${stale_threshold_seconds})[10m:15s]) or vector(0)) * 15"
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

wait_for_dashboard_baseline() {
  local desired="$1"
  start_vm_port_forward
  wait_for_dashboard_at_most "dashboard_vmagent_telemetry_age" dashboard_vmagent_telemetry_age "${max_vmagent_telemetry_age}"
  wait_for_dashboard_at_most "dashboard_k8s_metrics_age" dashboard_k8s_metrics_age "${max_k8s_metrics_age}"
  wait_for_dashboard_at_most "dashboard_app_telemetry_age" dashboard_app_telemetry_age "${max_app_telemetry_age}"
  wait_for_dashboard_at_most "dashboard_log_ingest_age" dashboard_log_ingest_age "${max_log_ingest_age}"
  wait_for_dashboard_at_least "dashboard_vmagent_available" dashboard_vmagent_available "${desired}"
  wait_for_dashboard_at_most "dashboard_observability_ready_gap" dashboard_observability_ready_gap 0
  wait_for_dashboard_at_least "dashboard_kube_state_metrics_scrape_up" dashboard_kube_state_metrics_scrape_up 1
}

wait_for_dashboard_recovery() {
  local desired="$1"
  local baseline_vmagent_stale_seconds="$2"
  local baseline_k8s_stale_seconds="$3"
  wait_for_dashboard_at_most "dashboard_vmagent_telemetry_age" dashboard_vmagent_telemetry_age "${max_vmagent_telemetry_age}"
  wait_for_dashboard_at_most "dashboard_vmagent_telemetry_stale" dashboard_vmagent_telemetry_stale 0
  wait_for_dashboard_at_most "dashboard_k8s_metrics_age" dashboard_k8s_metrics_age "${max_k8s_metrics_age}"
  wait_for_dashboard_at_most "dashboard_k8s_inventory_stale" dashboard_k8s_inventory_stale 0
  wait_for_dashboard_at_most "dashboard_app_telemetry_stale" dashboard_app_telemetry_stale 0
  wait_for_dashboard_at_most "dashboard_log_ingest_age" dashboard_log_ingest_age "${max_log_ingest_age}"
  wait_for_dashboard_at_most "dashboard_log_ingest_stale" dashboard_log_ingest_stale 0
  wait_for_dashboard_at_least "dashboard_vmagent_available" dashboard_vmagent_available "${desired}"
  wait_for_dashboard_at_most "dashboard_observability_ready_gap" dashboard_observability_ready_gap 0
  wait_for_dashboard_at_least "dashboard_kube_state_metrics_scrape_up" dashboard_kube_state_metrics_scrape_up 1
  wait_for_dashboard_delta_at_least "dashboard_vmagent_telemetry_stale_seconds_10m" dashboard_vmagent_telemetry_stale_seconds_10m "${baseline_vmagent_stale_seconds}" "${min_range_signal_seconds}"
  wait_for_dashboard_delta_at_least "dashboard_k8s_inventory_stale_seconds_10m" dashboard_k8s_inventory_stale_seconds_10m "${baseline_k8s_stale_seconds}" "${min_range_signal_seconds}"
}

wait_for_dashboard_ingestion_outage() {
  wait_for_dashboard_at_least "dashboard_vmagent_telemetry_stale" dashboard_vmagent_telemetry_stale 1
  wait_for_dashboard_at_least "dashboard_k8s_inventory_stale" dashboard_k8s_inventory_stale 1
  wait_for_dashboard_at_most "dashboard_kube_state_metrics_scrape_missing" dashboard_kube_state_metrics_scrape_missing 0
  wait_for_dashboard_at_most "dashboard_otel_collector_missing" dashboard_otel_collector_missing 0
  wait_for_dashboard_at_most "dashboard_logs_backend_missing" dashboard_logs_backend_missing 0
  wait_for_dashboard_at_most "dashboard_traces_backend_missing" dashboard_traces_backend_missing 0
  wait_for_dashboard_at_most "dashboard_app_telemetry_stale" dashboard_app_telemetry_stale 0
  wait_for_dashboard_at_most "dashboard_log_ingest_stale" dashboard_log_ingest_stale 0
  printf 'fault_dashboard_vmagent_telemetry_stale=%s\n' "$(dashboard_vmagent_telemetry_stale)"
  printf 'fault_dashboard_k8s_inventory_stale=%s\n' "$(dashboard_k8s_inventory_stale)"
  printf 'fault_dashboard_kube_state_metrics_scrape_missing=%s\n' "$(dashboard_kube_state_metrics_scrape_missing)"
  printf 'fault_dashboard_app_telemetry_stale=%s\n' "$(dashboard_app_telemetry_stale)"
  printf 'fault_dashboard_log_ingest_stale=%s\n' "$(dashboard_log_ingest_stale)"
}

choose_deployment() {
  kubectl -n "${namespace}" get deploy -l "${selector}" -o json \
    | jq -r '.items | sort_by(.metadata.name) | .[0].metadata.name // empty'
}

choose_ready_pod() {
  kubectl -n "${namespace}" get pods -l "${selector}" -o json \
    | jq -r '
        .items
        | map(select(.status.phase == "Running"))
        | map(select([.status.conditions[]? | select(.type == "Ready" and .status == "True")] | length > 0))
        | sort_by(.metadata.creationTimestamp)
        | .[0].metadata.name // empty
      '
}

available_replicas() {
  kubectl -n "${namespace}" get deploy "${deployment}" -o jsonpath='{.status.availableReplicas}' 2>/dev/null || true
}

wait_for_deployment_available_count() {
  local expected="$1"
  local deadline=$((SECONDS + timeout_seconds))
  local available

  while (( SECONDS < deadline )); do
    available="$(available_replicas)"
    available="${available:-0}"
    if [[ "${available}" == "${expected}" ]]; then
      printf '%s vmagent_available_replicas=%s expected=%s\n' "$(date -u +%Y-%m-%dT%H:%M:%SZ)" "${available}" "${expected}"
      return 0
    fi
    printf '%s vmagent_available_replicas=%s waiting_for=%s\n' "$(date -u +%Y-%m-%dT%H:%M:%SZ)" "${available}" "${expected}"
    sleep "${poll_seconds}"
  done

  echo "timed out waiting for ${namespace}/${deployment} available replicas to equal ${expected}" >&2
  kubectl -n "${namespace}" get deploy "${deployment}" -o wide >&2 || true
  kubectl -n "${namespace}" get pods -l "${selector}" -o wide >&2 || true
  return 1
}

wait_for_deployment_available() {
  local deadline=$((SECONDS + timeout_seconds))
  local available
  while (( SECONDS < deadline )); do
    available="$(available_replicas)"
    if [[ -n "${available}" && "${available}" != "0" ]]; then
      return 0
    fi
    sleep "${poll_seconds}"
  done

  echo "timed out waiting for ${namespace}/${deployment} to have available replicas" >&2
  kubectl -n "${namespace}" get deploy "${deployment}" -o wide >&2 || true
  kubectl -n "${namespace}" get pods -l "${selector}" -o wide >&2 || true
  return 1
}

wait_for_replacement_pod() {
  local old_uid="$1"
  local deadline=$((SECONDS + timeout_seconds))
  local replacement_json

  while (( SECONDS < deadline )); do
    replacement_json="$(
      kubectl -n "${namespace}" get pods -l "${selector}" -o json \
        | jq -r --arg old_uid "${old_uid}" '
            .items
            | map(select(.metadata.uid != $old_uid))
            | map(select(.status.phase == "Running"))
            | map(select([.status.conditions[]? | select(.type == "Ready" and .status == "True")] | length > 0))
            | sort_by(.metadata.creationTimestamp)
            | .[0] // empty
            | [.metadata.name, .metadata.uid, .spec.nodeName]
            | @tsv
          '
    )"
    if [[ -n "${replacement_json}" ]]; then
      printf '%s\n' "${replacement_json}"
      return 0
    fi
    sleep "${poll_seconds}"
  done

  echo "timed out waiting for a Ready replacement vmagent pod" >&2
  kubectl -n "${namespace}" get pods -l "${selector}" -o wide >&2 || true
  return 1
}

run_ingestion_outage_drill() {
  local desired="$1"
  local baseline_vmagent_stale_seconds="$2"
  local baseline_k8s_stale_seconds="$3"

  original_replica_count="$(kubectl -n "${namespace}" get vmagent "${vmagent_cr}" -o json | jq -r '.spec.replicaCount // empty')"
  restore_vmagent_on_exit="true"

  echo "==> setting VMAgent ${vmagent_cr} replicaCount to 0"
  kubectl -n "${namespace}" patch vmagent "${vmagent_cr}" --type=merge -p '{"spec":{"replicaCount":0}}'
  wait_for_deployment_available_count 0

  echo "==> waiting for vmagent telemetry and Kubernetes inventory metrics to become stale"
  wait_for_dashboard_ingestion_outage

  echo "==> restoring VMAgent ${vmagent_cr} replicaCount"
  restore_vmagent_replica_count
  restore_vmagent_on_exit="false"
  kubectl -n "${namespace}" rollout status "deployment/${deployment}" --timeout="${timeout_seconds}s"
  wait_for_dashboard_recovery "${desired}" "${baseline_vmagent_stale_seconds}" "${baseline_k8s_stale_seconds}"

  kubectl -n "${namespace}" get vmagent "${vmagent_cr}" -o wide
  kubectl -n "${namespace}" get deploy "${deployment}" -o wide
  kubectl -n "${namespace}" get pods -l "${selector}" -o wide
}

run_pod_loss_drill() {
  local desired="$1"
  local baseline_vmagent_stale_seconds="$2"
  local baseline_k8s_stale_seconds="$3"

  if [[ -z "${target_pod}" ]]; then
    target_pod="$(choose_ready_pod)"
  fi

  if [[ -z "${target_pod}" ]]; then
    echo "could not find a Ready vmagent pod with selector ${selector} in namespace ${namespace}" >&2
    kubectl -n "${namespace}" get pods -l "${selector}" -o wide >&2 || true
    exit 1
  fi

  old_uid="$(kubectl -n "${namespace}" get pod "${target_pod}" -o jsonpath='{.metadata.uid}')"
  old_node="$(kubectl -n "${namespace}" get pod "${target_pod}" -o jsonpath='{.spec.nodeName}')"

  echo "==> deleting vmagent pod ${target_pod} on ${old_node}"
  kubectl -n "${namespace}" delete pod "${target_pod}" --wait=false

  echo "==> waiting for a Ready replacement vmagent pod"
  replacement="$(wait_for_replacement_pod "${old_uid}")"
  new_pod="$(cut -f1 <<<"${replacement}")"
  new_uid="$(cut -f2 <<<"${replacement}")"
  new_node="$(cut -f3 <<<"${replacement}")"

  kubectl -n "${namespace}" rollout status "deployment/${deployment}" --timeout="${timeout_seconds}s"
  wait_for_dashboard_recovery "${desired}" "${baseline_vmagent_stale_seconds}" "${baseline_k8s_stale_seconds}"

  echo "==> vmagent pod replacement observed"
  printf 'old_pod\told_uid\told_node\tnew_pod\tnew_uid\tnew_node\n'
  printf '%s\t%s\t%s\t%s\t%s\t%s\n' "${target_pod}" "${old_uid}" "${old_node}" "${new_pod}" "${new_uid}" "${new_node}"
  kubectl -n "${namespace}" get deploy "${deployment}" -o wide
  kubectl -n "${namespace}" get pods -l "${selector}" -o wide
}

if [[ -z "${deployment}" ]]; then
  deployment="$(choose_deployment)"
fi

if [[ -z "${deployment}" ]]; then
  echo "could not find vmagent deployment with selector ${selector} in namespace ${namespace}" >&2
  exit 1
fi

wait_for_deployment_available
desired="$(desired_replicas)"
if [[ -z "${desired}" || "${desired}" == *[!0-9]* || "${desired}" == "0" ]]; then
  echo "deployment ${namespace}/${deployment} has invalid desired replicas: ${desired:-empty}" >&2
  exit 1
fi

wait_for_dashboard_baseline "${desired}"
baseline_recreations="$(dashboard_observability_pod_recreations)"
baseline_recreations="${baseline_recreations:-0}"
baseline_vmagent_stale_seconds="$(dashboard_vmagent_telemetry_stale_seconds_10m)"
baseline_vmagent_stale_seconds="${baseline_vmagent_stale_seconds:-0}"
baseline_k8s_stale_seconds="$(dashboard_k8s_inventory_stale_seconds_10m)"
baseline_k8s_stale_seconds="${baseline_k8s_stale_seconds:-0}"
printf 'baseline_dashboard_observability_pod_recreations=%s\n' "${baseline_recreations}"
printf 'baseline_dashboard_vmagent_telemetry_stale_seconds_10m=%s\n' "${baseline_vmagent_stale_seconds}"
printf 'baseline_dashboard_k8s_inventory_stale_seconds_10m=%s\n' "${baseline_k8s_stale_seconds}"

case "${failure_mode}" in
  ingestion-outage)
    run_ingestion_outage_drill "${desired}" "${baseline_vmagent_stale_seconds}" "${baseline_k8s_stale_seconds}"
    ;;
  pod)
    run_pod_loss_drill "${desired}" "${baseline_vmagent_stale_seconds}" "${baseline_k8s_stale_seconds}"
    ;;
  *)
    echo "FAILURE_MODE must be ingestion-outage or pod, got ${failure_mode}" >&2
    exit 2
    ;;
esac
