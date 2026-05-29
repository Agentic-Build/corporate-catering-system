#!/usr/bin/env bash
set -euo pipefail

cd "$(dirname "$0")/../.."

namespace="${NAMESPACE:-tbite}"
release="${RELEASE:-tbite}"
component="${PDB_BLOCKER_COMPONENT:-web-admin}"
deployment="${DEPLOYMENT:-${release}-tbite-platform-${component}}"
pdb="${PDB:-${deployment}}"
drain_timeout="${DRAIN_TIMEOUT:-45s}"
timeout_seconds="${TIMEOUT_SECONDS:-240}"
poll_seconds="${POLL_SECONDS:-5}"
min_range_signal_seconds="${MIN_RANGE_SIGNAL_SECONDS:-15}"
crashloop_observe_seconds="${CRASHLOOP_OBSERVE_SECONDS:-45}"
vm_service="${VM_SERVICE:-vmsingle-${release}-victoria-metrics-k8s-stack}"
vm_url="${VM_URL:-}"
vm_local_port="${VM_LOCAL_PORT:-18428}"
app_component_regex="${APP_COMPONENT_REGEX:-api|realtime|web-employee|web-merchant|web-admin|worker-outbox-relay|worker-payroll-settler|worker-on-time-evaluator|scheduler-cutoff|scheduler-no-show|scheduler-doc-expiry|scheduler-feedback}"
dashboard_file="chart/tbite-platform/dashboards/local-ha-drills.json"
port_forward_pid=""
target_pod=""
target_node=""
avoid_node=""
node_cordoned=false
pdb_patched=false
original_min_available=""
original_max_unavailable=""
baseline_cordoned_worker_seconds=0
baseline_pdb_policy_blocker_seconds=0
baseline_voluntary_disruption_blocked_seconds=0

cleanup() {
  if [[ "${node_cordoned}" == "true" && -n "${target_node}" ]]; then
    kubectl uncordon "${target_node}" >/dev/null 2>&1 || true
  fi
  if [[ "${pdb_patched}" == "true" ]]; then
    restore_pdb >/dev/null 2>&1 || true
  fi
  if [[ -n "${port_forward_pid}" ]]; then
    kill "${port_forward_pid}" >/dev/null 2>&1 || true
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

dashboard_voluntary_disruption_blocked() {
  dashboard_target_value "Drain/disruption availability" "voluntary disruption blocked"
}

dashboard_voluntary_disruption_blocked_seconds_10m() {
  dashboard_target_value "Drain/disruption activity" "voluntary disruption blocked seconds / 10m"
}

pdb_policy_blockers_query() {
  cat <<QUERY
((sum(kube_pod_info{namespace="${namespace}"} * on(namespace,pod) group_left(owner_kind,owner_name) kube_pod_owner{namespace="${namespace}",owner_kind="StatefulSet",owner_is_controller="true"} * on(node) group_left() kube_node_spec_unschedulable * on(namespace,owner_name) group_left() label_replace(kube_poddisruptionbudget_status_pod_disruptions_allowed{namespace="${namespace}"} == bool 0, "owner_name", "\$1", "poddisruptionbudget", "(.*)")) or vector(0)) + (sum(kube_pod_info{namespace="${namespace}",pod=~"${release}-tbite-platform-.*"} * on(namespace,pod) group_left(label_app_kubernetes_io_component) kube_pod_labels{namespace="${namespace}",label_app_kubernetes_io_instance="${release}",label_app_kubernetes_io_name="tbite-platform",label_app_kubernetes_io_component=~"${app_component_regex}"} * on(node) group_left() kube_node_spec_unschedulable * on(namespace,label_app_kubernetes_io_component) group_left() label_replace(kube_poddisruptionbudget_status_pod_disruptions_allowed{namespace="${namespace}",poddisruptionbudget=~"${release}-tbite-platform-.*"} == bool 0, "label_app_kubernetes_io_component", "\$1", "poddisruptionbudget", "${release}-tbite-platform-(.*)")) or vector(0)))
QUERY
}

dashboard_node_unschedulable() {
  promql_value "max(kube_node_spec_unschedulable{node=\"${target_node}\"}) or vector(0)"
}

dashboard_cordoned_workers() {
  promql_value 'sum(kube_node_spec_unschedulable * on(node) group_left(label_topology_kubernetes_io_zone) kube_node_labels{label_topology_kubernetes_io_zone!=""}) or vector(0)'
}

dashboard_cordoned_worker_seconds_10m() {
  promql_value '(sum_over_time(((sum(kube_node_spec_unschedulable * on(node) group_left(label_topology_kubernetes_io_zone) kube_node_labels{label_topology_kubernetes_io_zone!=""}) or vector(0)) > bool 0)[10m:15s]) or vector(0)) * 15'
}

dashboard_pdb_allowed() {
  promql_value "sum(kube_poddisruptionbudget_status_pod_disruptions_allowed{namespace=\"${namespace}\",poddisruptionbudget=\"${pdb}\"}) or vector(0)"
}

dashboard_pdb_policy_blockers() {
  promql_value "$(pdb_policy_blockers_query)"
}

dashboard_pdb_policy_blocker_seconds_10m() {
  promql_value "(sum_over_time((($(pdb_policy_blockers_query)) > bool 0)[10m:15s]) or vector(0)) * 15"
}

dashboard_pending_pods() {
  promql_value "sum(kube_pod_status_phase{namespace=\"${namespace}\",phase=\"Pending\"} == 1) or vector(0)"
}

dashboard_unschedulable_pods() {
  promql_value "sum(kube_pod_status_unschedulable{namespace=\"${namespace}\"}) or vector(0)"
}

dashboard_stateful_ready_gap() {
  promql_value "sum(kube_statefulset_replicas{namespace=\"${namespace}\"} - kube_statefulset_status_replicas_ready{namespace=\"${namespace}\"}) or vector(0)"
}

dashboard_unavailable_app_replicas() {
  promql_value "sum(kube_deployment_status_replicas_unavailable{namespace=\"${namespace}\",deployment=~\"${release}-tbite-platform-.*\"}) or vector(0)"
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

restore_pdb() {
  if [[ -n "${original_min_available}" ]]; then
    kubectl -n "${namespace}" patch pdb "${pdb}" --type=merge -p "{\"spec\":{\"minAvailable\":${original_min_available}}}"
  elif [[ -n "${original_max_unavailable}" ]]; then
    kubectl -n "${namespace}" patch pdb "${pdb}" --type=merge -p "{\"spec\":{\"maxUnavailable\":${original_max_unavailable}}}"
  else
    echo "cannot restore ${namespace}/${pdb}: original minAvailable and maxUnavailable were both empty" >&2
    return 1
  fi
  pdb_patched=false
}

wait_for_dashboard_baseline() {
  start_vm_port_forward
  wait_for_dashboard_at_most "dashboard_node_unschedulable" dashboard_node_unschedulable 0
  wait_for_dashboard_at_most "dashboard_cordoned_workers" dashboard_cordoned_workers 0
  wait_for_dashboard_at_most "dashboard_pdb_policy_blockers" dashboard_pdb_policy_blockers 0
  wait_for_dashboard_at_most "dashboard_voluntary_disruption_blocked" dashboard_voluntary_disruption_blocked 0
  wait_for_dashboard_at_most "dashboard_pending_pods" dashboard_pending_pods 0
  wait_for_dashboard_at_most "dashboard_unschedulable_pods" dashboard_unschedulable_pods 0
  wait_for_dashboard_at_most "dashboard_stateful_ready_gap" dashboard_stateful_ready_gap 0
  wait_for_dashboard_at_most "dashboard_unavailable_app_replicas" dashboard_unavailable_app_replicas 0

  baseline_cordoned_worker_seconds="$(dashboard_cordoned_worker_seconds_10m)"
  baseline_cordoned_worker_seconds="${baseline_cordoned_worker_seconds:-0}"
  baseline_pdb_policy_blocker_seconds="$(dashboard_pdb_policy_blocker_seconds_10m)"
  baseline_pdb_policy_blocker_seconds="${baseline_pdb_policy_blocker_seconds:-0}"
  baseline_voluntary_disruption_blocked_seconds="$(dashboard_voluntary_disruption_blocked_seconds_10m)"
  baseline_voluntary_disruption_blocked_seconds="${baseline_voluntary_disruption_blocked_seconds:-0}"
  printf 'baseline_dashboard_cordoned_worker_seconds_10m=%s\n' "${baseline_cordoned_worker_seconds}"
  printf 'baseline_dashboard_pdb_policy_blocker_seconds_10m=%s\n' "${baseline_pdb_policy_blocker_seconds}"
  printf 'baseline_dashboard_voluntary_disruption_blocked_seconds_10m=%s\n' "${baseline_voluntary_disruption_blocked_seconds}"
}

wait_for_pdb_allowed() {
  local threshold="$1"
  local deadline=$((SECONDS + timeout_seconds))
  local current
  while (( SECONDS < deadline )); do
    current="$(dashboard_pdb_allowed)"
    if [[ -n "${current}" ]] && float_le "${current}" "${threshold}" && float_ge "${current}" "${threshold}"; then
      printf '%s dashboard_pdb_allowed=%s expected=%s\n' "$(date -u +%Y-%m-%dT%H:%M:%SZ)" "${current}" "${threshold}"
      return 0
    fi
    printf '%s dashboard_pdb_allowed=%s waiting_for=%s\n' "$(date -u +%Y-%m-%dT%H:%M:%SZ)" "${current:-empty}" "${threshold}"
    sleep "${poll_seconds}"
  done
  echo "timed out waiting for ${pdb} allowed disruptions to equal ${threshold}" >&2
  return 1
}

wait_for_dashboard_blocker() {
  wait_for_dashboard_at_least "dashboard_node_unschedulable" dashboard_node_unschedulable 1
  wait_for_dashboard_at_least "dashboard_cordoned_workers" dashboard_cordoned_workers 1
  wait_for_dashboard_at_least "dashboard_pdb_policy_blockers" dashboard_pdb_policy_blockers 1
  wait_for_dashboard_at_least "dashboard_voluntary_disruption_blocked" dashboard_voluntary_disruption_blocked 1
  wait_for_dashboard_delta_at_least "dashboard_cordoned_worker_seconds_10m" dashboard_cordoned_worker_seconds_10m "${baseline_cordoned_worker_seconds}" "${min_range_signal_seconds}"
  wait_for_dashboard_delta_at_least "dashboard_pdb_policy_blocker_seconds_10m" dashboard_pdb_policy_blocker_seconds_10m "${baseline_pdb_policy_blocker_seconds}" "${min_range_signal_seconds}"
  wait_for_dashboard_delta_at_least "dashboard_voluntary_disruption_blocked_seconds_10m" dashboard_voluntary_disruption_blocked_seconds_10m "${baseline_voluntary_disruption_blocked_seconds}" "${min_range_signal_seconds}"
  wait_for_dashboard_at_most "dashboard_pending_pods" dashboard_pending_pods 0
  wait_for_dashboard_at_most "dashboard_unschedulable_pods" dashboard_unschedulable_pods 0
  wait_for_dashboard_at_most "dashboard_stateful_ready_gap" dashboard_stateful_ready_gap 0
  wait_for_dashboard_at_most "dashboard_unavailable_app_replicas" dashboard_unavailable_app_replicas 0
}

wait_for_dashboard_recovery() {
  wait_for_dashboard_at_most "dashboard_node_unschedulable" dashboard_node_unschedulable 0
  wait_for_dashboard_at_most "dashboard_cordoned_workers" dashboard_cordoned_workers 0
  wait_for_dashboard_at_most "dashboard_pdb_policy_blockers" dashboard_pdb_policy_blockers 0
  wait_for_dashboard_at_most "dashboard_voluntary_disruption_blocked" dashboard_voluntary_disruption_blocked 0
  wait_for_dashboard_at_most "dashboard_pending_pods" dashboard_pending_pods 0
  wait_for_dashboard_at_most "dashboard_unschedulable_pods" dashboard_unschedulable_pods 0
  wait_for_dashboard_at_most "dashboard_stateful_ready_gap" dashboard_stateful_ready_gap 0
  wait_for_dashboard_at_most "dashboard_unavailable_app_replicas" dashboard_unavailable_app_replicas 0
}

assert_target_pod_unchanged() {
  local current_node current_phase
  current_node="$(kubectl -n "${namespace}" get pod "${target_pod}" -o jsonpath='{.spec.nodeName}')"
  current_phase="$(kubectl -n "${namespace}" get pod "${target_pod}" -o jsonpath='{.status.phase}')"
  if [[ "${current_node}" != "${target_node}" || "${current_phase}" != "Running" ]]; then
    echo "target pod moved or stopped during expected PDB blocker." >&2
    printf 'target_pod=%s target_node=%s current_node=%s current_phase=%s\n' \
      "${target_pod}" "${target_node}" "${current_node}" "${current_phase}" >&2
    exit 1
  fi
}

assert_no_app_crashloops() {
  local deadline crashing
  deadline=$((SECONDS + crashloop_observe_seconds))
  echo "==> watching platform app pods for CrashLoopBackOff for ${crashloop_observe_seconds}s"
  while (( SECONDS < deadline )); do
    crashing="$(
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
              | select($containers | length > 0)
              | [$pod, ($containers | join(","))]
              | @tsv
          '
    )"
    if [[ -n "${crashing}" ]]; then
      echo "platform app pods entered CrashLoopBackOff during the PDB blocker:" >&2
      printf 'pod\tcontainers\n' >&2
      printf '%s\n' "${crashing}" >&2
      return 1
    fi
    sleep "${poll_seconds}"
  done
}

cnpg_primary="$(kubectl -n "${namespace}" get cluster "${release}-pg" -o jsonpath='{.status.currentPrimary}' 2>/dev/null || true)"
if [[ -n "${cnpg_primary}" ]]; then
  avoid_node="$(kubectl -n "${namespace}" get pod "${cnpg_primary}" -o jsonpath='{.spec.nodeName}' 2>/dev/null || true)"
fi

target="$(
  kubectl -n "${namespace}" get pods \
    -l "app.kubernetes.io/instance=${release},app.kubernetes.io/name=tbite-platform,app.kubernetes.io/component=${component}" \
    -o json \
    | jq -r --arg avoidNode "${avoid_node}" '
        [.items[]
         | select(.status.phase == "Running")
         | select(.spec.nodeName != null)
         | [.metadata.name, .spec.nodeName]
         | @tsv]
        | sort_by((split("\t")[1] == $avoidNode), split("\t")[0])
        | .[0] // empty
      '
)"
if [[ -z "${target}" ]]; then
  echo "no running target pod found for component ${component}" >&2
  exit 2
fi
IFS=$'\t' read -r target_pod target_node <<<"${target}"

replicas="$(kubectl -n "${namespace}" get deploy "${deployment}" -o jsonpath='{.spec.replicas}')"
ready_replicas="$(kubectl -n "${namespace}" get deploy "${deployment}" -o jsonpath='{.status.readyReplicas}')"
if [[ -z "${ready_replicas}" || "${ready_replicas}" != "${replicas}" ]]; then
  echo "${deployment} is not fully ready: replicas=${replicas} ready=${ready_replicas:-0}" >&2
  exit 2
fi

pdb_json="$(kubectl -n "${namespace}" get pdb "${pdb}" -o json)"
original_min_available="$(jq -r '.spec.minAvailable // empty' <<<"${pdb_json}")"
original_max_unavailable="$(jq -r '.spec.maxUnavailable // empty' <<<"${pdb_json}")"

printf 'target_component=%s\n' "${component}"
printf 'target_deployment=%s\n' "${deployment}"
printf 'target_pod=%s\n' "${target_pod}"
printf 'target_node=%s\n' "${target_node}"
printf 'target_pdb=%s\n' "${pdb}"
printf 'target_replicas=%s\n' "${replicas}"
printf 'avoided_node=%s\n' "${avoid_node:-empty}"
printf 'original_min_available=%s\n' "${original_min_available:-empty}"
printf 'original_max_unavailable=%s\n' "${original_max_unavailable:-empty}"

echo "==> before PDB blocker drill"
kubectl get nodes -L topology.kubernetes.io/zone
kubectl -n "${namespace}" get pod "${target_pod}" -o wide
kubectl -n "${namespace}" get pdb "${pdb}" -o wide
wait_for_dashboard_baseline

echo "==> tightening ${pdb} to minAvailable=${replicas}"
kubectl -n "${namespace}" patch pdb "${pdb}" --type=merge -p "{\"spec\":{\"minAvailable\":${replicas}}}"
pdb_patched=true
wait_for_pdb_allowed 0

echo "==> cordoning ${target_node}"
kubectl cordon "${target_node}"
node_cordoned=true

echo "==> attempting voluntary eviction of ${target_pod}; PDB should reject it"
set +e
drain_output="$(
  kubectl drain "${target_node}" \
    --ignore-daemonsets \
    --delete-emptydir-data \
    --pod-selector="app.kubernetes.io/instance=${release},app.kubernetes.io/name=tbite-platform,app.kubernetes.io/component=${component}" \
    --timeout="${drain_timeout}" 2>&1
)"
drain_status=$?
set -e
printf '%s\n' "${drain_output}"

if [[ "${drain_status}" -eq 0 ]]; then
  echo "expected PDB-protected drain to fail, but kubectl drain succeeded" >&2
  exit 1
fi

if ! grep -Eiq 'disruption budget|poddisruptionbudget|cannot evict' <<<"${drain_output}"; then
  echo "kubectl drain failed, but not with an identifiable PDB eviction rejection" >&2
  exit 1
fi

assert_target_pod_unchanged
wait_for_dashboard_blocker
assert_no_app_crashloops

printf 'dashboard_cordoned_worker_seconds_10m=%s\n' "$(dashboard_cordoned_worker_seconds_10m)"
printf 'dashboard_pdb_policy_blockers=%s\n' "$(dashboard_pdb_policy_blockers)"
printf 'dashboard_pdb_policy_blocker_seconds_10m=%s\n' "$(dashboard_pdb_policy_blocker_seconds_10m)"
printf 'dashboard_voluntary_disruption_blocked=%s\n' "$(dashboard_voluntary_disruption_blocked)"
printf 'dashboard_voluntary_disruption_blocked_seconds_10m=%s\n' "$(dashboard_voluntary_disruption_blocked_seconds_10m)"
printf 'dashboard_pdb_allowed=%s\n' "$(dashboard_pdb_allowed)"

echo "==> restoring ${target_node} and ${pdb}"
kubectl uncordon "${target_node}"
node_cordoned=false
restore_pdb
wait_for_dashboard_recovery

echo "==> PDB blocker recovered"
kubectl get nodes -L topology.kubernetes.io/zone
kubectl -n "${namespace}" get pod "${target_pod}" -o wide
kubectl -n "${namespace}" get pdb "${pdb}" -o wide
