#!/usr/bin/env bash
set -euo pipefail

cd "$(dirname "$0")/../.."

namespace="${NAMESPACE:-tbite}"
release="${RELEASE:-tbite}"
deployment="${KUBE_STATE_METRICS_DEPLOYMENT:-${release}-kube-state-metrics}"
env_name="${ENV_NAME:-local-ha}"
vm_service="${VM_SERVICE:-vmsingle-${release}-victoria-metrics-k8s-stack}"
vm_url="${VM_URL:-}"
vm_local_port="${VM_LOCAL_PORT:-18428}"
timeout_seconds="${TIMEOUT_SECONDS:-240}"
poll_seconds="${POLL_SECONDS:-5}"
port_forward_pid=""
original_replicas=""
restore_replicas=false

cleanup() {
  if [[ -n "${port_forward_pid}" ]]; then
    kill "${port_forward_pid}" >/dev/null 2>&1 || true
  fi
  if [[ "${restore_replicas}" == "true" && -n "${original_replicas}" ]]; then
    kubectl -n "${namespace}" scale "deployment/${deployment}" --replicas="${original_replicas}" >/dev/null 2>&1 || true
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

kube_state_metrics_selector() {
  printf 'app.kubernetes.io/name=kube-state-metrics'
}

ready_ksm_pods() {
  kubectl -n "${namespace}" get pods -l "$(kube_state_metrics_selector)" -o json \
    | jq -r '
        .items
        | map(select(.metadata.deletionTimestamp == null))
        | map(select(.status.phase == "Running"))
        | map(select([.status.conditions[]? | select(.type == "Ready" and .status == "True")] | length > 0))
        | sort_by(.metadata.creationTimestamp)
        | .[].metadata.name
      '
}

ready_ksm_pod() {
  ready_ksm_pods | tail -n 1
}

ready_ksm_pod_count() {
  ready_ksm_pods | wc -l | tr -d '[:space:]'
}

wait_for_ready_ksm_pod() {
  local forbidden_pod="${1:-}"
  local deadline pod
  deadline=$((SECONDS + timeout_seconds))
  while (( SECONDS < deadline )); do
    pod="$(ready_ksm_pod)"
    if [[ -n "${pod}" && "${pod}" != "${forbidden_pod}" ]]; then
      printf '%s ready_kube_state_metrics_pod=%s\n' "$(date -u +%Y-%m-%dT%H:%M:%SZ)" "${pod}" >&2
      printf '%s\n' "${pod}"
      return 0
    fi
    printf '%s ready_kube_state_metrics_pod=%s waiting_for_new_pod\n' "$(date -u +%Y-%m-%dT%H:%M:%SZ)" "${pod:-empty}" >&2
    sleep "${poll_seconds}"
  done

  echo "timed out waiting for a Ready replacement kube-state-metrics pod" >&2
  kubectl -n "${namespace}" get pods -l "$(kube_state_metrics_selector)" -o wide >&2 || true
  return 1
}

wait_for_ready_ksm_pod_count() {
  local minimum_count="$1"
  local deadline count
  deadline=$((SECONDS + timeout_seconds))
  while (( SECONDS < deadline )); do
    count="$(ready_ksm_pod_count)"
    if (( count >= minimum_count )); then
      printf '%s ready_kube_state_metrics_pods=%s minimum=%s\n' "$(date -u +%Y-%m-%dT%H:%M:%SZ)" "${count}" "${minimum_count}"
      return 0
    fi
    printf '%s ready_kube_state_metrics_pods=%s waiting_for_minimum=%s\n' "$(date -u +%Y-%m-%dT%H:%M:%SZ)" "${count}" "${minimum_count}"
    sleep "${poll_seconds}"
  done

  echo "timed out waiting for at least ${minimum_count} Ready kube-state-metrics pods" >&2
  kubectl -n "${namespace}" get pods -l "$(kube_state_metrics_selector)" -o wide >&2 || true
  return 1
}

wait_for_dashboard_at_least() {
  local name="$1"
  local query="$2"
  local threshold="$3"
  local deadline current
  deadline=$((SECONDS + timeout_seconds))
  while (( SECONDS < deadline )); do
    current="$(promql_value "${query}")"
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
  local query="$2"
  local threshold="$3"
  local deadline current
  deadline=$((SECONDS + timeout_seconds))
  while (( SECONDS < deadline )); do
    current="$(promql_value "${query}")"
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

dashboard_promql_smoke() {
  NAMESPACE="${namespace}" RELEASE="${release}" ENV_NAME="${env_name}" VM_URL="${vm_url}" node --input-type=module <<'NODE'
import fs from "node:fs";

const namespace = process.env.NAMESPACE;
const release = process.env.RELEASE;
const envName = process.env.ENV_NAME;
const vmUrl = process.env.VM_URL;
const dashboard = JSON.parse(fs.readFileSync("chart/tbite-platform/dashboards/local-ha-drills.json", "utf8"));
const queries = [];

for (const panel of dashboard.panels || []) {
  for (const target of panel.targets || []) {
    if (!target.expr) continue;
    queries.push({
      title: panel.title || "(untitled)",
      refId: target.refId || "?",
      expr: target.expr
        .replace(/\$namespace\b/g, namespace)
        .replace(/\$release\b/g, release)
        .replace(/\$env\b/g, envName),
    });
  }
}

const failures = [];
for (const query of queries) {
  const url = new URL(`${vmUrl}/api/v1/query`);
  url.searchParams.set("query", query.expr);
  let response;
  try {
    response = await fetch(url);
  } catch (error) {
    failures.push({ ...query, error: String(error) });
    continue;
  }
  const body = await response.text();
  let parsed;
  try {
    parsed = JSON.parse(body);
  } catch {
    failures.push({ ...query, status: response.status, error: `non-json response: ${body.slice(0, 200)}` });
    continue;
  }
  if (!response.ok || parsed.status !== "success") {
    failures.push({ ...query, status: response.status, error: parsed.error || body.slice(0, 300) });
  }
}

console.log(`dashboard_promql_queries=${queries.length}`);
console.log(`dashboard_promql_failures=${failures.length}`);
if (failures.length) {
  for (const failure of failures.slice(0, 20)) {
    console.log(`${failure.title} ${failure.refId}: ${failure.error}`);
    console.log(failure.expr);
  }
  process.exit(1);
}
NODE
}

duplicate_node_labels_query='max(count by (node) (kube_node_labels{label_topology_kubernetes_io_zone!=""}) > bool 1) or vector(0)'
duplicate_pod_owner_query="max(count by (namespace, pod, owner_kind, owner_name) (kube_pod_owner{namespace=\"${namespace}\",owner_is_controller=\"true\"}) > bool 1) or vector(0)"
ksm_scrape_up_query="sum(up{namespace=\"${namespace}\",job=\"kube-state-metrics\"}) or vector(0)"
k8s_inventory_stale_query="(((time() - max(timestamp(kube_pod_info{namespace=\"${namespace}\"}))) or vector(999999)) > bool 45)"
observability_ready_gap_query="(((sum((max by (namespace, deployment) (kube_deployment_spec_replicas{namespace=\"${namespace}\",deployment=~\"vmagent-.*|vmsingle-.*|vmalert-.*|${release}-grafana|${release}-kube-state-metrics|${release}-opentelemetry-collector|${release}-victoria-metrics-operator\"}) - max by (namespace, deployment) (kube_deployment_status_replicas_available{namespace=\"${namespace}\",deployment=~\"vmagent-.*|vmsingle-.*|vmalert-.*|${release}-grafana|${release}-kube-state-metrics|${release}-opentelemetry-collector|${release}-victoria-metrics-operator\"}))) or vector(0)) + (sum((max by (namespace, statefulset) (kube_statefulset_replicas{namespace=\"${namespace}\",statefulset=~\"${release}-victoria-logs-single-server|${release}-vt-single-server|vmalertmanager-.*\"}) - max by (namespace, statefulset) (kube_statefulset_status_replicas_ready{namespace=\"${namespace}\",statefulset=~\"${release}-victoria-logs-single-server|${release}-vt-single-server|vmalertmanager-.*\"}))) or vector(0)) + (sum((max by (namespace, daemonset) (kube_daemonset_status_desired_number_scheduled{namespace=\"${namespace}\",daemonset=~\"${release}-prometheus-node-exporter|${release}-vector\"}) - max by (namespace, daemonset) (kube_daemonset_status_number_ready{namespace=\"${namespace}\",daemonset=~\"${release}-prometheus-node-exporter|${release}-vector\"}))) or vector(0))) * on() ((((time() - max(timestamp(kube_pod_info{namespace=\"${namespace}\"}))) or vector(999999)) <= bool 45)))"

start_vm_port_forward

original_replicas="$(kubectl -n "${namespace}" get "deployment/${deployment}" -o jsonpath='{.spec.replicas}')"
original_replicas="${original_replicas:-1}"
overlap_replicas=$((original_replicas + 1))

kubectl -n "${namespace}" rollout status "deployment/${deployment}" --timeout="${timeout_seconds}s"
old_pod="$(wait_for_ready_ksm_pod)"
wait_for_dashboard_at_least "kube_state_metrics_scrape_up" "${ksm_scrape_up_query}" 1
wait_for_dashboard_at_most "dashboard_k8s_inventory_stale" "${k8s_inventory_stale_query}" 0
wait_for_dashboard_at_most "dashboard_observability_component_ready_gap" "${observability_ready_gap_query}" 0

echo "==> baseline dashboard smoke before kube-state-metrics replacement"
dashboard_promql_smoke
printf 'old_kube_state_metrics_pod=%s\n' "${old_pod}"
kubectl -n "${namespace}" get pods -l "$(kube_state_metrics_selector)" -o wide

echo "==> scaling kube-state-metrics from ${original_replicas} to ${overlap_replicas} replicas to force overlapping scrape targets"
restore_replicas=true
kubectl -n "${namespace}" scale "deployment/${deployment}" --replicas="${overlap_replicas}"
wait_for_ready_ksm_pod_count "${overlap_replicas}"
new_pod="$(wait_for_ready_ksm_pod "${old_pod}")"
kubectl -n "${namespace}" rollout status "deployment/${deployment}" --timeout="${timeout_seconds}s"
wait_for_dashboard_at_least "kube_state_metrics_scrape_up" "${ksm_scrape_up_query}" 1

echo "==> waiting for duplicate KSM object series in VictoriaMetrics"
wait_for_dashboard_at_least "duplicate_kube_node_labels_by_node" "${duplicate_node_labels_query}" 1
wait_for_dashboard_at_least "duplicate_kube_pod_owner_by_object" "${duplicate_pod_owner_query}" 1

echo "==> dashboard smoke during duplicate KSM series"
dashboard_promql_smoke
wait_for_dashboard_at_most "dashboard_observability_component_ready_gap" "${observability_ready_gap_query}" 0

echo "==> deleting old kube-state-metrics pod ${old_pod} and restoring ${deployment} to ${original_replicas} replicas"
kubectl -n "${namespace}" delete pod "${old_pod}" --wait=false
kubectl -n "${namespace}" wait --for=delete "pod/${old_pod}" --timeout="${timeout_seconds}s"
kubectl -n "${namespace}" scale "deployment/${deployment}" --replicas="${original_replicas}"
kubectl -n "${namespace}" rollout status "deployment/${deployment}" --timeout="${timeout_seconds}s"
restore_replicas=false
wait_for_dashboard_at_most "dashboard_observability_component_ready_gap" "${observability_ready_gap_query}" 0

printf 'old_kube_state_metrics_pod=%s\n' "${old_pod}"
printf 'new_kube_state_metrics_pod=%s\n' "${new_pod}"
printf 'duplicate_kube_node_labels_by_node=%s\n' "$(promql_value "${duplicate_node_labels_query}")"
printf 'duplicate_kube_pod_owner_by_object=%s\n' "$(promql_value "${duplicate_pod_owner_query}")"
printf 'dashboard_k8s_inventory_stale=%s\n' "$(promql_value "${k8s_inventory_stale_query}")"
kubectl -n "${namespace}" get pods -l "$(kube_state_metrics_selector)" -o wide
