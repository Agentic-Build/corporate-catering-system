#!/usr/bin/env bash
set -euo pipefail

cd "$(dirname "$0")/../.."

namespace="${NAMESPACE:-tbite}"
release="${RELEASE:-tbite}"
keda_metrics_deployment="${KEDA_METRICS_DEPLOYMENT:-keda-operator-metrics-apiserver}"
vm_service="${VM_SERVICE:-vmsingle-${release}-victoria-metrics-k8s-stack}"
vm_url="${VM_URL:-}"
vm_local_port="${VM_LOCAL_PORT:-18428}"
hold_seconds="${HOLD_SECONDS:-45}"
timeout_seconds="${TIMEOUT_SECONDS:-180}"
min_range_signal_seconds="${MIN_RANGE_SIGNAL_SECONDS:-15}"
restore="${RESTORE:-true}"
port_forward_pid=""
dashboard_file="chart/tbite-platform/dashboards/local-ha-drills.json"

keda_hpa_regex="^keda-hpa-${release}-tbite-platform-"

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

dashboard_autoscaler_condition_failures_top() {
  dashboard_target_value "Autoscaler availability" "autoscaler condition failures"
}

dashboard_cpu_metrics_path_unavailable() {
  dashboard_target_value "Autoscaler availability" "cpu metrics path unavailable"
}

dashboard_keda_metrics_path_unavailable() {
  dashboard_target_value "Autoscaler availability" "keda metrics path unavailable"
}

dashboard_keda_metrics_path_unavailable_seconds_10m() {
  dashboard_target_value "Autoscaler activity" "keda metrics path unavailable seconds / 10m"
}

dashboard_autoscaler_condition_failure_seconds_10m() {
  dashboard_target_value "Autoscaler activity" "autoscaler condition failure seconds / 10m"
}

dashboard_bad_scale_conditions() {
  promql_value "sum(kube_horizontalpodautoscaler_status_condition{namespace=\"${namespace}\",horizontalpodautoscaler=~\"(keda-hpa-)?${release}-tbite-platform-.*\",condition=~\"AbleToScale|ScalingActive\",status!=\"true\"} == 1) or vector(0)"
}

dashboard_cpu_hpas_inactive() {
  promql_value "sum(kube_horizontalpodautoscaler_status_condition{namespace=\"${namespace}\",horizontalpodautoscaler=~\"${release}-tbite-platform-(api|realtime|web-.*)\",condition=\"ScalingActive\",status=\"false\"} == 1) or vector(0)"
}

dashboard_keda_hpas_inactive() {
  promql_value "sum(kube_horizontalpodautoscaler_status_condition{namespace=\"${namespace}\",horizontalpodautoscaler=~\"keda-hpa-${release}-tbite-platform-.*\",condition=\"ScalingActive\",status=\"false\"} == 1) or vector(0)"
}

dashboard_keda_hpa_inactive_seconds_10m() {
  promql_value "(sum_over_time(((sum(kube_horizontalpodautoscaler_status_condition{namespace=\"${namespace}\",horizontalpodautoscaler=~\"keda-hpa-${release}-tbite-platform-.*\",condition=\"ScalingActive\",status=\"false\"} == 1) or vector(0)) > bool 0)[10m:15s]) or vector(0)) * 15"
}

dashboard_keda_operator_missing() {
  promql_value "((max(kube_deployment_status_replicas_available{namespace=\"${namespace}\",deployment=\"keda-operator\"}) or vector(0)) < bool 1)"
}

dashboard_keda_metrics_apiserver_missing() {
  promql_value "((max(kube_deployment_status_replicas_available{namespace=\"${namespace}\",deployment=\"${keda_metrics_deployment}\"}) or vector(0)) < bool 1)"
}

dashboard_keda_metrics_apiserver_unavailable_seconds_10m() {
  promql_value "(sum_over_time(((max(kube_deployment_status_replicas_available{namespace=\"${namespace}\",deployment=\"${keda_metrics_deployment}\"}) or vector(0)) < bool 1)[10m:15s]) or vector(0)) * 15"
}

dashboard_metrics_server_missing() {
  promql_value '((max(kube_deployment_status_replicas_available{namespace="kube-system",deployment="metrics-server"}) or vector(0)) < bool 1)'
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

print_dashboard_signals() {
  printf 'dashboard_autoscaler_condition_failures_top=%s\n' "$(dashboard_autoscaler_condition_failures_top)"
  printf 'dashboard_cpu_metrics_path_unavailable=%s\n' "$(dashboard_cpu_metrics_path_unavailable)"
  printf 'dashboard_keda_metrics_path_unavailable=%s\n' "$(dashboard_keda_metrics_path_unavailable)"
  printf 'dashboard_keda_metrics_path_unavailable_seconds_10m=%s\n' "$(dashboard_keda_metrics_path_unavailable_seconds_10m)"
  printf 'dashboard_autoscaler_condition_failure_seconds_10m=%s\n' "$(dashboard_autoscaler_condition_failure_seconds_10m)"
  printf 'dashboard_bad_scale_conditions=%s\n' "$(dashboard_bad_scale_conditions)"
  printf 'dashboard_cpu_hpas_inactive=%s\n' "$(dashboard_cpu_hpas_inactive)"
  printf 'dashboard_keda_hpas_inactive=%s\n' "$(dashboard_keda_hpas_inactive)"
  printf 'dashboard_keda_hpa_inactive_seconds_10m=%s\n' "$(dashboard_keda_hpa_inactive_seconds_10m)"
  printf 'dashboard_keda_operator_missing=%s\n' "$(dashboard_keda_operator_missing)"
  printf 'dashboard_keda_metrics_apiserver_missing=%s\n' "$(dashboard_keda_metrics_apiserver_missing)"
  printf 'dashboard_keda_metrics_apiserver_unavailable_seconds_10m=%s\n' "$(dashboard_keda_metrics_apiserver_unavailable_seconds_10m)"
  printf 'dashboard_metrics_server_missing=%s\n' "$(dashboard_metrics_server_missing)"
}

keda_hpa_count() {
  kubectl -n "${namespace}" get hpa -o json \
    | jq --arg re "${keda_hpa_regex}" '[.items[] | select(.metadata.name | test($re))] | length'
}

keda_hpa_inactive_count() {
  kubectl -n "${namespace}" get hpa -o json \
    | jq --arg re "${keda_hpa_regex}" '
        [
          .items[]
          | select(.metadata.name | test($re))
          | .status.conditions[]?
          | select(.type == "ScalingActive" and .status != "True" and (.reason // "") == "FailedGetExternalMetric")
        ]
        | length
      '
}

external_metrics_api_available() {
  kubectl get apiservice v1beta1.external.metrics.k8s.io -o json \
    | jq -r '.status.conditions[]? | select(.type == "Available") | .status + ":" + (.reason // "")'
}

print_keda_hpa_conditions() {
  kubectl -n "${namespace}" get hpa -o json \
    | jq -r --arg re "${keda_hpa_regex}" '
        .items[]
        | select(.metadata.name | test($re))
        | [
            .metadata.name,
            ([.status.conditions[]? | select(.type == "AbleToScale" or .type == "ScalingActive") | .type + ":" + .status + ":" + (.reason // "")] | join(","))
          ]
        | @tsv
      '
}

wait_for_keda_hpa_inactive() {
  local expected="$1"
  local deadline=$((SECONDS + timeout_seconds))
  local inactive
  while (( SECONDS < deadline )); do
    inactive="$(keda_hpa_inactive_count)"
    if (( inactive >= expected )); then
      return 0
    fi
    sleep 5
  done
  echo "timed out waiting for KEDA HPAs to report FailedGetExternalMetric" >&2
  kubectl -n "${namespace}" get hpa -o wide >&2 || true
  print_keda_hpa_conditions >&2 || true
  kubectl get apiservice v1beta1.external.metrics.k8s.io -o yaml >&2 || true
  return 1
}

wait_for_keda_hpa_active() {
  local deadline=$((SECONDS + timeout_seconds))
  local inactive
  while (( SECONDS < deadline )); do
    inactive="$(keda_hpa_inactive_count)"
    if (( inactive == 0 )); then
      return 0
    fi
    sleep 5
  done
  echo "timed out waiting for KEDA HPAs to recover valid external metrics" >&2
  kubectl -n "${namespace}" get hpa -o wide >&2 || true
  print_keda_hpa_conditions >&2 || true
  kubectl get apiservice v1beta1.external.metrics.k8s.io -o yaml >&2 || true
  return 1
}

original_replicas="$(
  kubectl -n "${namespace}" get deployment "${keda_metrics_deployment}" -o jsonpath='{.spec.replicas}'
)"
original_replicas="${original_replicas:-1}"

cleanup() {
  local status="$?"

  if [[ "${restore}" != "true" ]]; then
    echo "RESTORE=${restore}; leaving ${namespace}/${keda_metrics_deployment} scaled down"
  else
    echo "==> restoring ${namespace}/${keda_metrics_deployment} to replicas=${original_replicas}"
    kubectl -n "${namespace}" scale deployment "${keda_metrics_deployment}" --replicas="${original_replicas}" >/dev/null || status=1
    kubectl -n "${namespace}" rollout status "deployment/${keda_metrics_deployment}" --timeout="${timeout_seconds}s" || status=1
    wait_for_keda_hpa_active || status=1
    if [[ -n "${vm_url}" ]]; then
      wait_for_dashboard_at_most "dashboard_keda_operator_missing" dashboard_keda_operator_missing 0 || status=1
      wait_for_dashboard_at_most "dashboard_keda_metrics_apiserver_missing" dashboard_keda_metrics_apiserver_missing 0 || status=1
      wait_for_dashboard_at_most "dashboard_keda_hpas_inactive" dashboard_keda_hpas_inactive 0 || status=1
      wait_for_dashboard_at_most "dashboard_cpu_hpas_inactive" dashboard_cpu_hpas_inactive 0 || status=1
      wait_for_dashboard_at_most "dashboard_metrics_server_missing" dashboard_metrics_server_missing 0 || status=1
      wait_for_dashboard_at_most "dashboard_bad_scale_conditions" dashboard_bad_scale_conditions 0 || status=1
      wait_for_dashboard_at_most "dashboard_autoscaler_condition_failures_top" dashboard_autoscaler_condition_failures_top 0 || status=1
      wait_for_dashboard_at_most "dashboard_cpu_metrics_path_unavailable" dashboard_cpu_metrics_path_unavailable 0 || status=1
      wait_for_dashboard_at_most "dashboard_keda_metrics_path_unavailable" dashboard_keda_metrics_path_unavailable 0 || status=1
    fi
    kubectl -n "${namespace}" get hpa -o wide || true
    echo "external.metrics.k8s.io available: $(external_metrics_api_available)"
    if [[ -n "${vm_url}" ]]; then
      print_dashboard_signals || true
    fi
  fi

  if [[ -n "${port_forward_pid}" ]]; then
    kill "${port_forward_pid}" >/dev/null 2>&1 || true
  fi

  exit "${status}"
}

trap cleanup EXIT

expected_hpas="$(keda_hpa_count)"
if (( expected_hpas == 0 )); then
  echo "no KEDA-owned platform HPAs found in namespace ${namespace}" >&2
  exit 1
fi

baseline_inactive="$(keda_hpa_inactive_count)"
if (( baseline_inactive != 0 )); then
  echo "KEDA HPAs are already inactive before the drill; refusing to mask a pre-existing autoscaler failure" >&2
  kubectl -n "${namespace}" get hpa -o wide >&2 || true
  print_keda_hpa_conditions >&2 || true
  exit 1
fi

echo "==> baseline KEDA HPA conditions"
start_vm_port_forward
wait_for_dashboard_at_most "dashboard_keda_operator_missing" dashboard_keda_operator_missing 0
wait_for_dashboard_at_most "dashboard_keda_metrics_apiserver_missing" dashboard_keda_metrics_apiserver_missing 0
wait_for_dashboard_at_most "dashboard_keda_hpas_inactive" dashboard_keda_hpas_inactive 0
wait_for_dashboard_at_most "dashboard_cpu_hpas_inactive" dashboard_cpu_hpas_inactive 0
wait_for_dashboard_at_most "dashboard_metrics_server_missing" dashboard_metrics_server_missing 0
wait_for_dashboard_at_most "dashboard_bad_scale_conditions" dashboard_bad_scale_conditions 0
wait_for_dashboard_at_most "dashboard_autoscaler_condition_failures_top" dashboard_autoscaler_condition_failures_top 0
wait_for_dashboard_at_most "dashboard_cpu_metrics_path_unavailable" dashboard_cpu_metrics_path_unavailable 0
wait_for_dashboard_at_most "dashboard_keda_metrics_path_unavailable" dashboard_keda_metrics_path_unavailable 0
kubectl -n "${namespace}" get hpa -o wide
print_keda_hpa_conditions
echo "external.metrics.k8s.io available: $(external_metrics_api_available)"
print_dashboard_signals

echo "==> scaling ${namespace}/${keda_metrics_deployment} to zero"
kubectl -n "${namespace}" scale deployment "${keda_metrics_deployment}" --replicas=0

echo "==> waiting for ${expected_hpas} KEDA HPAs to report FailedGetExternalMetric"
wait_for_keda_hpa_inactive "${expected_hpas}"
kubectl -n "${namespace}" get hpa -o wide
print_keda_hpa_conditions
echo "external.metrics.k8s.io available: $(external_metrics_api_available)"

echo "==> waiting for dashboard autoscaler signals to reflect the KEDA metrics outage"
wait_for_dashboard_at_least "dashboard_keda_metrics_apiserver_missing" dashboard_keda_metrics_apiserver_missing 1
wait_for_dashboard_at_most "dashboard_keda_operator_missing" dashboard_keda_operator_missing 0
wait_for_dashboard_at_least "dashboard_keda_hpas_inactive" dashboard_keda_hpas_inactive "${expected_hpas}"
wait_for_dashboard_at_least "dashboard_bad_scale_conditions" dashboard_bad_scale_conditions "${expected_hpas}"
wait_for_dashboard_at_least "dashboard_autoscaler_condition_failures_top" dashboard_autoscaler_condition_failures_top "${expected_hpas}"
wait_for_dashboard_at_least "dashboard_keda_metrics_path_unavailable" dashboard_keda_metrics_path_unavailable 1
wait_for_dashboard_at_most "dashboard_cpu_metrics_path_unavailable" dashboard_cpu_metrics_path_unavailable 0
wait_for_dashboard_at_least "dashboard_keda_metrics_apiserver_unavailable_seconds_10m" dashboard_keda_metrics_apiserver_unavailable_seconds_10m "${min_range_signal_seconds}"
wait_for_dashboard_at_least "dashboard_keda_hpa_inactive_seconds_10m" dashboard_keda_hpa_inactive_seconds_10m "${min_range_signal_seconds}"
wait_for_dashboard_at_least "dashboard_keda_metrics_path_unavailable_seconds_10m" dashboard_keda_metrics_path_unavailable_seconds_10m "${min_range_signal_seconds}"
wait_for_dashboard_at_least "dashboard_autoscaler_condition_failure_seconds_10m" dashboard_autoscaler_condition_failure_seconds_10m "${min_range_signal_seconds}"
wait_for_dashboard_at_most "dashboard_cpu_hpas_inactive" dashboard_cpu_hpas_inactive 0
wait_for_dashboard_at_most "dashboard_metrics_server_missing" dashboard_metrics_server_missing 0
print_dashboard_signals

echo "==> holding KEDA metrics outage for ${hold_seconds}s after dashboard detection"
sleep "${hold_seconds}"

echo "==> KEDA metrics outage observed"
