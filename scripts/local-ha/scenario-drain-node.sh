#!/usr/bin/env bash
set -euo pipefail

cd "$(dirname "$0")/../.."

namespace="${NAMESPACE:-tbite}"
release="${RELEASE:-tbite}"
node="${NODE:-}"
uncordon="${UNCORDON:-true}"
pod_selector="${DRAIN_POD_SELECTOR:-}"
allow_pinned_pvc_drain="${ALLOW_PINNED_PVC_DRAIN:-false}"
expect_blocked="${EXPECT_BLOCKED:-}"
restore_after_blocker="${RESTORE_AFTER_BLOCKER:-false}"
app_component_regex="${APP_COMPONENT_REGEX:-api|realtime|web-employee|web-merchant|web-admin|worker-outbox-relay|worker-payroll-settler|worker-on-time-evaluator|scheduler-cutoff|scheduler-no-show|scheduler-doc-expiry|scheduler-feedback}"
blocked_timeout="${BLOCKED_TIMEOUT:-120}"
crashloop_observe_seconds="${CRASHLOOP_OBSERVE_SECONDS:-75}"
poll_seconds="${POLL_SECONDS:-5}"
timeout_seconds="${TIMEOUT_SECONDS:-240}"
min_range_signal_seconds="${MIN_RANGE_SIGNAL_SECONDS:-15}"
vm_service="${VM_SERVICE:-vmsingle-${release}-victoria-metrics-k8s-stack}"
vm_url="${VM_URL:-}"
vm_local_port="${VM_LOCAL_PORT:-18428}"
port_forward_pid=""
node_cordoned=false
baseline_cordoned_worker_seconds=0
baseline_unschedulable_pod_seconds=0
baseline_stateful_scheduling_blocker_seconds=0
baseline_stateful_ready_gap_seconds=0

if [[ -z "${expect_blocked}" ]]; then
  expect_blocked="false"
  if [[ -z "${pod_selector}" && "${allow_pinned_pvc_drain}" == "true" && "${uncordon}" == "false" ]]; then
    expect_blocked="true"
  fi
fi

if [[ "${restore_after_blocker}" == "true" ]]; then
  uncordon="true"
fi

if [[ "${expect_blocked}" == "true" && "${uncordon}" == "true" && "${restore_after_blocker}" != "true" ]]; then
  echo "EXPECT_BLOCKED=true with UNCORDON=true requires RESTORE_AFTER_BLOCKER=true." >&2
  echo "Use UNCORDON=false when you intentionally want to keep the blocker observable after the script exits." >&2
  exit 2
fi

default_pinned_pvc_node() {
  local candidate
  candidate="$(
    kubectl get pv -o json \
      | jq -r --arg namespace "${namespace}" '
          [.items[]
           | select(.spec.claimRef.namespace == $namespace)
           | ([.spec.nodeAffinity.required.nodeSelectorTerms[]?.matchExpressions[]?
               | select(.key == "kubernetes.io/hostname")
               | .values[]?][0] // "") as $node
           | select($node != "")
           | $node]
          | group_by(.)
          | map({node: .[0], count: length})
          | sort_by(.count, .node)
          | .[0].node // empty
        '
  )"
  if [[ -n "${candidate}" ]]; then
    printf '%s\n' "${candidate}"
    return 0
  fi

  kubectl get nodes --selector='!node-role.kubernetes.io/control-plane' -o jsonpath='{.items[0].metadata.name}'
}

default_node() {
  if [[ "${expect_blocked}" == "true" ]]; then
    default_pinned_pvc_node
    return 0
  fi

  if [[ -n "${pod_selector}" ]]; then
    local candidate
    candidate="$(
      kubectl -n "${namespace}" get pods -l "${pod_selector}" -o json \
        | jq -r '
            [.items[]
             | select(.status.phase == "Running")
             | select(.spec.nodeName != null)
             | .spec.nodeName]
            | group_by(.)
            | map({node: .[0], count: length})
            | sort_by(.count, .node)
            | reverse
            | .[0].node // empty
          '
    )"
    if [[ -n "${candidate}" ]]; then
      printf '%s\n' "${candidate}"
      return 0
    fi
  fi

  kubectl get nodes --selector='!node-role.kubernetes.io/control-plane' -o jsonpath='{.items[0].metadata.name}'
}

if [[ -z "${node}" ]]; then
  node="$(default_node)"
fi

cleanup() {
  if [[ "${uncordon}" == "true" && "${node_cordoned}" == "true" ]]; then
    kubectl uncordon "${node}" >/dev/null 2>&1 || true
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

dashboard_node_unschedulable() {
  promql_value "max(kube_node_spec_unschedulable{node=\"${node}\"}) or vector(0)"
}

dashboard_min_schedulable_workers_per_zone() {
  promql_value 'min(sum by (label_topology_kubernetes_io_zone) ((max by (node) (kube_node_status_condition{condition="Ready",status="true"})) * on(node) group_left() (1 - (max by (node) (kube_node_spec_unschedulable))) * on(node) group_left(label_topology_kubernetes_io_zone) (max by (node, label_topology_kubernetes_io_zone) (kube_node_labels{label_topology_kubernetes_io_zone!=""}))))'
}

dashboard_zone_capacity_depleted() {
  promql_value '((min(sum by (label_topology_kubernetes_io_zone) ((max by (node) (kube_node_status_condition{condition="Ready",status="true"})) * on(node) group_left() (1 - (max by (node) (kube_node_spec_unschedulable))) * on(node) group_left(label_topology_kubernetes_io_zone) (max by (node, label_topology_kubernetes_io_zone) (kube_node_labels{label_topology_kubernetes_io_zone!=""})))) or vector(0)) < bool 1)'
}

dashboard_cordoned_workers() {
  promql_value 'sum((max by (node) (kube_node_spec_unschedulable)) * on(node) group_left(label_topology_kubernetes_io_zone) (max by (node, label_topology_kubernetes_io_zone) (kube_node_labels{label_topology_kubernetes_io_zone!=""}))) or vector(0)'
}

dashboard_cordoned_worker_seconds_10m() {
  promql_value '(sum_over_time(((sum((max by (node) (kube_node_spec_unschedulable)) * on(node) group_left(label_topology_kubernetes_io_zone) (max by (node, label_topology_kubernetes_io_zone) (kube_node_labels{label_topology_kubernetes_io_zone!=""}))) or vector(0)) > bool 0)[10m:15s]) or vector(0)) * 15'
}

dashboard_zone_coverage_gaps() {
  promql_value "$(cat <<QUERY
count((count by (label_app_kubernetes_io_component) (count by (label_app_kubernetes_io_component, label_topology_kubernetes_io_zone) (kube_pod_status_phase{namespace="${namespace}",phase="Running"} * on(namespace,pod) group_left(node) (max by (namespace, pod, node) (kube_pod_info{namespace="${namespace}"})) * on(node) group_left(label_topology_kubernetes_io_zone) (max by (node, label_topology_kubernetes_io_zone) (kube_node_labels{label_topology_kubernetes_io_zone!=""})) * on(namespace,pod) group_left(label_app_kubernetes_io_component) (max by (namespace, pod, label_app_kubernetes_io_component) (kube_pod_labels{namespace="${namespace}",label_app_kubernetes_io_instance="${release}",label_app_kubernetes_io_name="tbite-platform",label_app_kubernetes_io_component=~"${app_component_regex}"}))))) < on(label_app_kubernetes_io_component) (clamp_max(max by (label_app_kubernetes_io_component) (label_replace((max by (namespace, deployment) (kube_deployment_spec_replicas{namespace="${namespace}",deployment=~"${release}-tbite-platform-.*"})), "label_app_kubernetes_io_component", "\$1", "deployment", "${release}-tbite-platform-(.*)")), 3))) or vector(0)
QUERY
)"
}

dashboard_unavailable_app_replicas() {
  promql_value "sum(kube_deployment_status_replicas_unavailable{namespace=\"${namespace}\",deployment=~\"${release}-tbite-platform-.*\"}) or vector(0)"
}

dashboard_unavailable_app_seconds_10m() {
  promql_value "(sum_over_time(((sum(kube_deployment_status_replicas_unavailable{namespace=\"${namespace}\",deployment=~\"${release}-tbite-platform-.*\"}) or vector(0)) > bool 0)[10m:15s]) or vector(0)) * 15"
}

dashboard_pending_pods() {
  promql_value "sum(kube_pod_status_phase{namespace=\"${namespace}\",phase=\"Pending\"} == 1) or vector(0)"
}

dashboard_unschedulable_pods() {
  promql_value "sum((max by (namespace, pod) (kube_pod_status_unschedulable{namespace=\"${namespace}\"}))) or vector(0)"
}

dashboard_stateful_scheduling_blockers() {
  promql_value "sum((max by (namespace, pod) (kube_pod_status_unschedulable{namespace=\"${namespace}\"})) * on(namespace,pod) group_left(owner_kind,owner_name) (max by (namespace, pod, owner_kind, owner_name) (kube_pod_owner{namespace=\"${namespace}\",owner_kind=~\"StatefulSet|Cluster\",owner_is_controller=\"true\"}))) or vector(0)"
}

dashboard_unschedulable_pod_seconds_10m() {
  promql_value "(sum_over_time(((sum((max by (namespace, pod) (kube_pod_status_unschedulable{namespace=\"${namespace}\"}))) or vector(0)) > bool 0)[10m:15s]) or vector(0)) * 15"
}

dashboard_stateful_scheduling_blocker_seconds_10m() {
  promql_value "(sum_over_time(((sum((max by (namespace, pod) (kube_pod_status_unschedulable{namespace=\"${namespace}\"})) * on(namespace,pod) group_left(owner_kind,owner_name) (max by (namespace, pod, owner_kind, owner_name) (kube_pod_owner{namespace=\"${namespace}\",owner_kind=~\"StatefulSet|Cluster\",owner_is_controller=\"true\"}))) or vector(0)) > bool 0)[10m:15s]) or vector(0)) * 15"
}

dashboard_stateful_ready_gap() {
  promql_value "sum(kube_statefulset_replicas{namespace=\"${namespace}\"} - kube_statefulset_status_replicas_ready{namespace=\"${namespace}\"}) or vector(0)"
}

dashboard_stateful_ready_gap_seconds_10m() {
  promql_value "(sum_over_time(((sum(kube_statefulset_replicas{namespace=\"${namespace}\"} - kube_statefulset_status_replicas_ready{namespace=\"${namespace}\"}) or vector(0)) > bool 0)[10m:15s]) or vector(0)) * 15"
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
  start_vm_port_forward
  wait_for_dashboard_at_most "dashboard_node_unschedulable" dashboard_node_unschedulable 0
  wait_for_dashboard_at_least "dashboard_min_schedulable_workers_per_zone" dashboard_min_schedulable_workers_per_zone 1
  wait_for_dashboard_at_most "dashboard_zone_capacity_depleted" dashboard_zone_capacity_depleted 0
  wait_for_dashboard_at_most "dashboard_cordoned_workers" dashboard_cordoned_workers 0
  wait_for_dashboard_at_most "dashboard_zone_coverage_gaps" dashboard_zone_coverage_gaps 0
  wait_for_dashboard_at_most "dashboard_unavailable_app_replicas" dashboard_unavailable_app_replicas 0
  wait_for_dashboard_at_most "dashboard_pending_pods" dashboard_pending_pods 0
  wait_for_dashboard_at_most "dashboard_unschedulable_pods" dashboard_unschedulable_pods 0
  wait_for_dashboard_at_most "dashboard_stateful_scheduling_blockers" dashboard_stateful_scheduling_blockers 0
  wait_for_dashboard_at_most "dashboard_stateful_ready_gap" dashboard_stateful_ready_gap 0
}

record_dashboard_blocker_baseline() {
  if [[ "${expect_blocked}" != "true" ]]; then
    return 0
  fi

  baseline_cordoned_worker_seconds="$(dashboard_cordoned_worker_seconds_10m)"
  baseline_cordoned_worker_seconds="${baseline_cordoned_worker_seconds:-0}"
  baseline_unschedulable_pod_seconds="$(dashboard_unschedulable_pod_seconds_10m)"
  baseline_unschedulable_pod_seconds="${baseline_unschedulable_pod_seconds:-0}"
  baseline_stateful_scheduling_blocker_seconds="$(dashboard_stateful_scheduling_blocker_seconds_10m)"
  baseline_stateful_scheduling_blocker_seconds="${baseline_stateful_scheduling_blocker_seconds:-0}"
  baseline_stateful_ready_gap_seconds="$(dashboard_stateful_ready_gap_seconds_10m)"
  baseline_stateful_ready_gap_seconds="${baseline_stateful_ready_gap_seconds:-0}"

  printf 'baseline_dashboard_cordoned_worker_seconds_10m=%s\n' "${baseline_cordoned_worker_seconds}"
  printf 'baseline_dashboard_unschedulable_pod_seconds_10m=%s\n' "${baseline_unschedulable_pod_seconds}"
  printf 'baseline_dashboard_stateful_scheduling_blocker_seconds_10m=%s\n' "${baseline_stateful_scheduling_blocker_seconds}"
  printf 'baseline_dashboard_stateful_ready_gap_seconds_10m=%s\n' "${baseline_stateful_ready_gap_seconds}"
}

wait_for_dashboard_node_drain_observed() {
  if [[ -z "${pod_selector}" || "${expect_blocked}" == "true" ]]; then
    return 0
  fi

  wait_for_dashboard_at_least "dashboard_node_unschedulable" dashboard_node_unschedulable 1
  wait_for_dashboard_at_least "dashboard_min_schedulable_workers_per_zone" dashboard_min_schedulable_workers_per_zone 1
  wait_for_dashboard_at_most "dashboard_zone_capacity_depleted" dashboard_zone_capacity_depleted 0
  wait_for_dashboard_at_least "dashboard_cordoned_workers" dashboard_cordoned_workers 1
  wait_for_dashboard_at_least "dashboard_cordoned_worker_seconds_10m" dashboard_cordoned_worker_seconds_10m "${min_range_signal_seconds}"
  wait_for_dashboard_at_most "dashboard_unavailable_app_replicas" dashboard_unavailable_app_replicas 0
  wait_for_dashboard_at_most "dashboard_pending_pods" dashboard_pending_pods 0
  wait_for_dashboard_at_most "dashboard_unschedulable_pods" dashboard_unschedulable_pods 0
  wait_for_dashboard_at_most "dashboard_stateful_scheduling_blockers" dashboard_stateful_scheduling_blockers 0
  wait_for_dashboard_at_most "dashboard_stateful_ready_gap" dashboard_stateful_ready_gap 0
}

restore_node() {
  if [[ "${uncordon}" != "true" || "${node_cordoned}" != "true" ]]; then
    return 0
  fi

  kubectl uncordon "${node}" >/dev/null
  node_cordoned=false
}

rebalance_app_deployments() {
  if [[ -z "${pod_selector}" || "${expect_blocked}" == "true" || "${uncordon}" != "true" ]]; then
    return 0
  fi

  NAMESPACE="${namespace}" \
    DEPLOYMENT_SELECTOR="app.kubernetes.io/instance=${release},app.kubernetes.io/name=tbite-platform" \
    scripts/local-ha/rebalance-apps.sh
}

rebalance_app_deployments_after_blocker() {
  if [[ "${expect_blocked}" != "true" || "${restore_after_blocker}" != "true" ]]; then
    return 0
  fi

  NAMESPACE="${namespace}" \
    DEPLOYMENT_SELECTOR="app.kubernetes.io/instance=${release},app.kubernetes.io/name=tbite-platform" \
    scripts/local-ha/rebalance-apps.sh
}

wait_for_dashboard_recovery() {
  if [[ -z "${pod_selector}" || "${expect_blocked}" == "true" ]]; then
    return 0
  fi

  wait_for_dashboard_at_most "dashboard_node_unschedulable" dashboard_node_unschedulable 0
  wait_for_dashboard_at_least "dashboard_min_schedulable_workers_per_zone" dashboard_min_schedulable_workers_per_zone 1
  wait_for_dashboard_at_most "dashboard_zone_capacity_depleted" dashboard_zone_capacity_depleted 0
  wait_for_dashboard_at_most "dashboard_cordoned_workers" dashboard_cordoned_workers 0
  wait_for_dashboard_at_most "dashboard_zone_coverage_gaps" dashboard_zone_coverage_gaps 0
  wait_for_dashboard_at_most "dashboard_unavailable_app_replicas" dashboard_unavailable_app_replicas 0
  wait_for_dashboard_at_most "dashboard_pending_pods" dashboard_pending_pods 0
  wait_for_dashboard_at_most "dashboard_unschedulable_pods" dashboard_unschedulable_pods 0
  wait_for_dashboard_at_most "dashboard_stateful_ready_gap" dashboard_stateful_ready_gap 0
}

wait_for_dashboard_expected_blocker() {
  if [[ "${expect_blocked}" != "true" ]]; then
    return 0
  fi

  start_vm_port_forward
  wait_for_dashboard_at_least "dashboard_node_unschedulable" dashboard_node_unschedulable 1
  wait_for_dashboard_at_least "dashboard_cordoned_workers" dashboard_cordoned_workers 1
  wait_for_dashboard_at_least "dashboard_unschedulable_pods" dashboard_unschedulable_pods 1
  wait_for_dashboard_at_least "dashboard_stateful_scheduling_blockers" dashboard_stateful_scheduling_blockers 1
  wait_for_dashboard_at_least "dashboard_stateful_ready_gap" dashboard_stateful_ready_gap 1
  wait_for_dashboard_delta_at_least "dashboard_cordoned_worker_seconds_10m" dashboard_cordoned_worker_seconds_10m "${baseline_cordoned_worker_seconds}" "${min_range_signal_seconds}"
  wait_for_dashboard_delta_at_least "dashboard_unschedulable_pod_seconds_10m" dashboard_unschedulable_pod_seconds_10m "${baseline_unschedulable_pod_seconds}" "${min_range_signal_seconds}"
  wait_for_dashboard_delta_at_least "dashboard_stateful_scheduling_blocker_seconds_10m" dashboard_stateful_scheduling_blocker_seconds_10m "${baseline_stateful_scheduling_blocker_seconds}" "${min_range_signal_seconds}"
  wait_for_dashboard_delta_at_least "dashboard_stateful_ready_gap_seconds_10m" dashboard_stateful_ready_gap_seconds_10m "${baseline_stateful_ready_gap_seconds}" "${min_range_signal_seconds}"
}

wait_for_dashboard_expected_blocker_recovery() {
  if [[ "${expect_blocked}" != "true" || "${restore_after_blocker}" != "true" ]]; then
    return 0
  fi

  wait_for_dashboard_at_most "dashboard_node_unschedulable" dashboard_node_unschedulable 0
  wait_for_dashboard_at_least "dashboard_min_schedulable_workers_per_zone" dashboard_min_schedulable_workers_per_zone 1
  wait_for_dashboard_at_most "dashboard_zone_capacity_depleted" dashboard_zone_capacity_depleted 0
  wait_for_dashboard_at_most "dashboard_cordoned_workers" dashboard_cordoned_workers 0
  wait_for_dashboard_at_most "dashboard_zone_coverage_gaps" dashboard_zone_coverage_gaps 0
  wait_for_dashboard_at_most "dashboard_unavailable_app_replicas" dashboard_unavailable_app_replicas 0
  wait_for_dashboard_at_most "dashboard_pending_pods" dashboard_pending_pods 0
  wait_for_dashboard_at_most "dashboard_unschedulable_pods" dashboard_unschedulable_pods 0
  wait_for_dashboard_at_most "dashboard_stateful_scheduling_blockers" dashboard_stateful_scheduling_blockers 0
  wait_for_dashboard_at_most "dashboard_stateful_ready_gap" dashboard_stateful_ready_gap 0
}

pinned_claims_for_node() {
  kubectl get pv -o json | jq -r --arg namespace "${namespace}" --arg node "${node}" '
    .items[]
    | select(.spec.claimRef.namespace == $namespace)
    | select(
        [.spec.nodeAffinity.required.nodeSelectorTerms[]?.matchExpressions[]?
         | select(.key == "kubernetes.io/hostname")
         | .values[]?] | index($node)
      )
    | .spec.claimRef.name
  '
}

pinned_pvc_pods() {
  local pinned_claims pod_claims
  pinned_claims="$(pinned_claims_for_node)"
  if [[ -z "${pinned_claims}" ]]; then
    return 0
  fi

  pod_claims="$(
    kubectl -n "${namespace}" get pods --field-selector "spec.nodeName=${node}" -o json \
      | jq -r '
          .items[]
          | .metadata.name as $pod
          | .spec.volumes[]?
          | select(.persistentVolumeClaim.claimName != null)
          | [$pod, .persistentVolumeClaim.claimName]
          | @tsv
        '
  )"
  while IFS=$'\t' read -r pod claim; do
    if grep -Fxq "${claim}" <<<"${pinned_claims}"; then
      printf '%s\t%s\n' "${pod}" "${claim}"
    fi
  done <<<"${pod_claims}"
}

blocked_pinned_pvc_pods() {
  local pinned_claims
  pinned_claims="$(pinned_claims_for_node)"
  if [[ -z "${pinned_claims}" ]]; then
    return 0
  fi

  kubectl -n "${namespace}" get pods -o json \
    | jq -r --arg claims "${pinned_claims}" '
        ($claims | split("\n") | map(select(. != ""))) as $claims
        | .items[]
        | .metadata.name as $pod
        | .status.phase as $phase
        | (.spec.nodeName // "") as $node
        | ((.status.conditions[]? | select(.type == "PodScheduled") | .reason) // "") as $scheduleReason
        | .spec.volumes[]?
        | select(.persistentVolumeClaim.claimName != null)
        | .persistentVolumeClaim.claimName as $claim
        | select($claims | index($claim))
        | select($phase != "Running" and $phase != "Succeeded")
        | [$pod, $claim, $phase, $node, $scheduleReason]
        | @tsv
      '
}

wait_for_expected_blocker() {
  local deadline blocked
  deadline=$((SECONDS + blocked_timeout))
  echo "==> waiting for pinned PVC blocker on ${node}"
  while (( SECONDS < deadline )); do
    blocked="$(blocked_pinned_pvc_pods)"
    if [[ -n "${blocked}" ]]; then
      echo "==> expected pinned PVC blocker observed"
      printf 'pod\tclaim\tphase\tnode\tschedule_reason\n'
      printf '%s\n' "${blocked}"
      kubectl -n "${namespace}" get pods --field-selector=status.phase!=Running,status.phase!=Succeeded -o wide || true
      return 0
    fi
    sleep "${poll_seconds}"
  done

  echo "timed out waiting for a pinned PVC pod to become blocked on ${node}" >&2
  echo "pinned claims:" >&2
  pinned_claims_for_node >&2 || true
  kubectl -n "${namespace}" get pods -o wide >&2 || true
  return 1
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
      echo "platform app pods entered CrashLoopBackOff during the expected blocker:" >&2
      printf 'pod\tcontainers\n' >&2
      printf '%s\n' "${crashing}" >&2
      return 1
    fi
    sleep "${poll_seconds}"
  done
}

if [[ -z "${pod_selector}" && "${allow_pinned_pvc_drain}" != "true" ]]; then
  pinned="$(pinned_pvc_pods)"
  if [[ -n "${pinned}" ]]; then
    cat >&2 <<EOF
Refusing full-node drain of ${node}: one or more pods use local-path PVs pinned to this node.

Pinned pods:
$(printf '%s\n' "${pinned}" | sed 's/^/  /')

kind's default storage class is rancher.io/local-path, so these StatefulSet pods
cannot reschedule while the node is cordoned. Use one of:
  DRAIN_POD_SELECTOR='app.kubernetes.io/component in (api,realtime,web-employee,web-merchant,web-admin,worker-outbox-relay,worker-payroll-settler,worker-on-time-evaluator,scheduler-cutoff,scheduler-no-show,scheduler-doc-expiry,scheduler-feedback)' make local-ha-drain-node
  make local-ha-drain-apps
  ALLOW_PINNED_PVC_DRAIN=true UNCORDON=false make local-ha-drain-node
EOF
    exit 2
  fi
fi

if [[ "${expect_blocked}" == "true" && -z "$(pinned_claims_for_node)" ]]; then
  echo "EXPECT_BLOCKED=true requested, but ${node} has no local-path PV claims pinned to it." >&2
  exit 2
fi

echo "==> before drain"
kubectl get nodes -L topology.kubernetes.io/zone
kubectl -n "${namespace}" get pods -o wide
wait_for_dashboard_baseline
record_dashboard_blocker_baseline

echo "==> draining ${node}"
drain_args=(
  "${node}"
  --ignore-daemonsets
  --delete-emptydir-data
  --timeout="${DRAIN_TIMEOUT:-10m}"
)
if [[ -n "${pod_selector}" ]]; then
  echo "pod selector: ${pod_selector}"
  drain_args+=("--pod-selector=${pod_selector}")
fi
node_cordoned=true
kubectl drain "${drain_args[@]}"

if [[ "${expect_blocked}" == "true" ]]; then
  wait_for_expected_blocker
  wait_for_dashboard_expected_blocker
  assert_no_app_crashloops
  echo "==> expected blocker retained on ${node}"
  printf 'dashboard_cordoned_worker_seconds_10m=%s\n' "$(dashboard_cordoned_worker_seconds_10m)"
  printf 'dashboard_zone_coverage_gaps=%s\n' "$(dashboard_zone_coverage_gaps)"
  printf 'dashboard_unavailable_app_replicas=%s\n' "$(dashboard_unavailable_app_replicas)"
  printf 'dashboard_unavailable_app_seconds_10m=%s\n' "$(dashboard_unavailable_app_seconds_10m)"
  printf 'dashboard_stateful_scheduling_blockers=%s\n' "$(dashboard_stateful_scheduling_blockers)"
  printf 'dashboard_stateful_scheduling_blocker_seconds_10m=%s\n' "$(dashboard_stateful_scheduling_blocker_seconds_10m)"
  printf 'dashboard_unschedulable_pod_seconds_10m=%s\n' "$(dashboard_unschedulable_pod_seconds_10m)"
  printf 'dashboard_stateful_ready_gap_seconds_10m=%s\n' "$(dashboard_stateful_ready_gap_seconds_10m)"
  kubectl get nodes -L topology.kubernetes.io/zone
  kubectl -n "${namespace}" get pods -o wide
  if [[ "${restore_after_blocker}" == "true" ]]; then
    echo "==> restoring ${node} after expected blocker"
    restore_node
    scripts/local-ha/wait-ready.sh
    rebalance_app_deployments_after_blocker
    wait_for_dashboard_expected_blocker_recovery
    echo "==> expected blocker recovered on ${node}"
    kubectl get nodes -L topology.kubernetes.io/zone
    kubectl -n "${namespace}" get pods -o wide
  fi
  exit 0
fi

scripts/local-ha/wait-ready.sh
wait_for_dashboard_node_drain_observed
restore_node
rebalance_app_deployments
wait_for_dashboard_recovery
printf 'dashboard_cordoned_worker_seconds_10m=%s\n' "$(dashboard_cordoned_worker_seconds_10m)"
printf 'dashboard_unavailable_app_seconds_10m=%s\n' "$(dashboard_unavailable_app_seconds_10m)"

echo "==> drained ${node}"
kubectl -n "${namespace}" get pods -o wide
