#!/usr/bin/env bash
set -euo pipefail

cd "$(dirname "$0")/../.."

namespace="${NAMESPACE:-tbite}"
release="${RELEASE:-tbite}"
requested_zone="${ZONE:-}"
cnpg_cluster="${CNPG_CLUSTER:-${release}-pg}"
uncordon="${UNCORDON:-true}"
pod_selector="${DRAIN_POD_SELECTOR:-}"
allow_pinned_pvc_drain="${ALLOW_PINNED_PVC_DRAIN:-false}"
expect_blocked="${EXPECT_BLOCKED:-}"
blocked_timeout="${BLOCKED_TIMEOUT:-120}"
poll_seconds="${POLL_SECONDS:-5}"
timeout_seconds="${TIMEOUT_SECONDS:-240}"
min_range_signal_seconds="${MIN_RANGE_SIGNAL_SECONDS:-15}"
crashloop_observe_seconds="${CRASHLOOP_OBSERVE_SECONDS:-75}"
vm_service="${VM_SERVICE:-vmsingle-${release}-victoria-metrics-k8s-stack}"
vm_url="${VM_URL:-}"
vm_local_port="${VM_LOCAL_PORT:-18428}"
port_forward_pid=""
app_component_regex="api|realtime|web-employee|web-merchant|web-admin|worker-outbox-relay|worker-payroll-settler|worker-on-time-evaluator|scheduler-cutoff|scheduler-no-show|scheduler-doc-expiry|scheduler-feedback"
cnpg_primary_before=""
cnpg_primary_node_before=""
expect_cnpg_role_change="false"
nodes_cordoned="false"

if [[ -z "${expect_blocked}" ]]; then
  expect_blocked="false"
  if [[ -z "${pod_selector}" && "${allow_pinned_pvc_drain}" == "true" && "${uncordon}" == "false" ]]; then
    expect_blocked="true"
  fi
fi

if [[ "${expect_blocked}" == "true" && "${uncordon}" == "true" ]]; then
  echo "EXPECT_BLOCKED=true requires UNCORDON=false so the blocker remains observable for evidence collection." >&2
  exit 2
fi

default_blocker_zone() {
  local nodes_json pvs_json zone
  nodes_json="$(kubectl get nodes --selector='!node-role.kubernetes.io/control-plane' -o json)"
  pvs_json="$(kubectl get pv -o json)"
  zone="$(
    jq -r -n --arg namespace "${namespace}" --argjson nodes "${nodes_json}" --argjson pvs "${pvs_json}" '
      def pv_node($pv):
        [$pv.spec.nodeAffinity.required.nodeSelectorTerms[]?.matchExpressions[]?
         | select(.key == "kubernetes.io/hostname")
         | .values[]?][0] // "";

      ($nodes.items
       | map({
           node: .metadata.name,
           zone: (.metadata.labels["topology.kubernetes.io/zone"] // "")
         })
       | map(select(.zone != ""))
       | sort_by(.zone)) as $workers
      | ($pvs.items
         | map(select(.spec.claimRef.namespace == $namespace)
               | {claim: .spec.claimRef.name, node: pv_node(.)})
         | map(select(.node != ""))) as $claims
      | $workers
      | group_by(.zone)
      | map(
          map(.node) as $zoneNodes
          | {
              zone: .[0].zone,
              pinned: ([$claims[] | select(.node as $node | $zoneNodes | index($node))] | length),
              observability: ([
                $claims[]
                | select(.node as $node | $zoneNodes | index($node))
                | select(.claim | test("^(vmsingle-|server-volume-tbite-victoria-logs|server-volume-tbite-vt|vmalertmanager-)"))
              ] | length)
            }
        )
      | map(select(.pinned > 0))
      | sort_by(.observability, .pinned, .zone)
      | .[0].zone // empty
    '
  )"
  if [[ -n "${zone}" ]]; then
    printf '%s\n' "${zone}"
    return 0
  fi

  printf 'local-a\n'
}

if [[ -n "${requested_zone}" ]]; then
  zone="${requested_zone}"
elif [[ "${expect_blocked}" == "true" ]]; then
  zone="$(default_blocker_zone)"
else
  zone="local-a"
fi

nodes=()
while IFS= read -r node; do
  [[ -n "${node}" ]] && nodes+=("${node}")
done < <(kubectl get nodes -l "topology.kubernetes.io/zone=${zone}" -o jsonpath='{range .items[*]}{.metadata.name}{"\n"}{end}')
if [[ "${#nodes[@]}" -eq 0 ]]; then
  echo "no nodes found for zone ${zone}" >&2
  exit 1
fi

cleanup() {
  if [[ "${uncordon}" == "true" ]]; then
    for node in "${nodes[@]}"; do
      kubectl uncordon "${node}" >/dev/null 2>&1 || true
    done
  fi
  if [[ -n "${port_forward_pid}" ]]; then
    kill "${port_forward_pid}" >/dev/null 2>&1 || true
  fi
}
trap cleanup EXIT

float_ge() {
  awk -v left="$1" -v right="$2" 'BEGIN { exit !(left >= right) }'
}

float_gt() {
  awk -v left="$1" -v right="$2" 'BEGIN { exit !(left > right) }'
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

dashboard_min_schedulable_workers_per_zone() {
  promql_value 'min(sum by (label_topology_kubernetes_io_zone) ((max by (node) (kube_node_status_condition{condition="Ready",status="true"})) * on(node) group_left() (1 - (max by (node) (kube_node_spec_unschedulable))) * on(node) group_left(label_topology_kubernetes_io_zone) (max by (node, label_topology_kubernetes_io_zone) (kube_node_labels{label_topology_kubernetes_io_zone!=""}))))'
}

dashboard_zone_capacity_depleted() {
  promql_value '((min(sum by (label_topology_kubernetes_io_zone) ((max by (node) (kube_node_status_condition{condition="Ready",status="true"})) * on(node) group_left() (1 - (max by (node) (kube_node_spec_unschedulable))) * on(node) group_left(label_topology_kubernetes_io_zone) (max by (node, label_topology_kubernetes_io_zone) (kube_node_labels{label_topology_kubernetes_io_zone!=""})))) or vector(0)) < bool 1)'
}

dashboard_cordoned_workers() {
  promql_value 'sum((max by (node) (kube_node_spec_unschedulable)) * on(node) group_left(label_topology_kubernetes_io_zone) (max by (node, label_topology_kubernetes_io_zone) (kube_node_labels{label_topology_kubernetes_io_zone!=""})))'
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

dashboard_zone_coverage_gap_seconds_10m() {
  promql_value "$(cat <<QUERY
(sum_over_time(((count((count by (label_app_kubernetes_io_component) (count by (label_app_kubernetes_io_component, label_topology_kubernetes_io_zone) (kube_pod_status_phase{namespace="${namespace}",phase="Running"} * on(namespace,pod) group_left(node) (max by (namespace, pod, node) (kube_pod_info{namespace="${namespace}"})) * on(node) group_left(label_topology_kubernetes_io_zone) (max by (node, label_topology_kubernetes_io_zone) (kube_node_labels{label_topology_kubernetes_io_zone!=""})) * on(namespace,pod) group_left(label_app_kubernetes_io_component) (max by (namespace, pod, label_app_kubernetes_io_component) (kube_pod_labels{namespace="${namespace}",label_app_kubernetes_io_instance="${release}",label_app_kubernetes_io_name="tbite-platform",label_app_kubernetes_io_component=~"${app_component_regex}"}))))) < on(label_app_kubernetes_io_component) (clamp_max(max by (label_app_kubernetes_io_component) (label_replace((max by (namespace, deployment) (kube_deployment_spec_replicas{namespace="${namespace}",deployment=~"${release}-tbite-platform-.*"})), "label_app_kubernetes_io_component", "\$1", "deployment", "${release}-tbite-platform-(.*)")), 3))) or vector(0)) > bool 0)[10m:15s]) or vector(0)) * 15
QUERY
)"
}

dashboard_cnpg_unhealthy() {
  promql_value "(((sum(cnpg_collector_up{namespace=\"${namespace}\",cluster=\"${cnpg_cluster}\"}) or vector(0)) < bool 3) + ((sum(1 - cnpg_pg_replication_in_recovery{namespace=\"${namespace}\",pod=~\"${cnpg_cluster}-[0-9]+\"}) or vector(0)) != bool 1) + ((max(cnpg_pg_replication_lag{namespace=\"${namespace}\",pod=~\"${cnpg_cluster}-[0-9]+\"}) or vector(0)) > bool 5) + ((sum(kube_pod_status_ready{namespace=\"${namespace}\",pod=~\"${cnpg_cluster}-[0-9]+\",condition=\"true\"}) or vector(0)) < bool 3))"
}

dashboard_cnpg_role_changes() {
  promql_value "sum(changes(cnpg_pg_replication_in_recovery{namespace=\"${namespace}\",pod=~\"${cnpg_cluster}-[0-9]+\"}[10m])) or vector(0)"
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
    sleep 5
  done

  echo "timed out waiting for dashboard signal ${name} >= ${threshold}" >&2
  return 1
}

wait_for_dashboard_greater_than() {
  local name="$1"
  local metric_func="$2"
  local threshold="$3"
  local deadline=$((SECONDS + timeout_seconds))
  local current

  while (( SECONDS < deadline )); do
    current="$("${metric_func}")"
    if [[ -n "${current}" ]] && float_gt "${current}" "${threshold}"; then
      printf '%s %s=%s threshold_gt=%s\n' "$(date -u +%Y-%m-%dT%H:%M:%SZ)" "${name}" "${current}" "${threshold}"
      return 0
    fi
    printf '%s %s=%s waiting_for_greater_than=%s\n' "$(date -u +%Y-%m-%dT%H:%M:%SZ)" "${name}" "${current:-empty}" "${threshold}"
    sleep 5
  done

  echo "timed out waiting for dashboard signal ${name} > ${threshold}" >&2
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

wait_for_dashboard_baseline() {
  if [[ "${expect_blocked}" == "true" ]]; then
    return 0
  fi

  start_vm_port_forward
  wait_for_dashboard_at_least "dashboard_min_schedulable_workers_per_zone" dashboard_min_schedulable_workers_per_zone 1
  wait_for_dashboard_at_most "dashboard_zone_capacity_depleted" dashboard_zone_capacity_depleted 0
  wait_for_dashboard_at_most "dashboard_cordoned_workers" dashboard_cordoned_workers 0
  wait_for_dashboard_at_most "dashboard_zone_coverage_gaps" dashboard_zone_coverage_gaps 0
  wait_for_dashboard_at_most "dashboard_unavailable_app_replicas" dashboard_unavailable_app_replicas 0
  wait_for_dashboard_at_most "dashboard_pending_pods" dashboard_pending_pods 0
  wait_for_dashboard_at_most "dashboard_unschedulable_pods" dashboard_unschedulable_pods 0
  wait_for_dashboard_at_most "dashboard_stateful_scheduling_blockers" dashboard_stateful_scheduling_blockers 0
  wait_for_dashboard_at_most "dashboard_stateful_ready_gap" dashboard_stateful_ready_gap 0
  wait_for_dashboard_at_most "dashboard_cnpg_unhealthy" dashboard_cnpg_unhealthy 0
}

wait_for_dashboard_zone_drain_observed() {
  if [[ -z "${pod_selector}" || "${expect_blocked}" == "true" ]]; then
    return 0
  fi

  wait_for_dashboard_at_least "dashboard_cordoned_workers" dashboard_cordoned_workers "${#nodes[@]}"
  wait_for_dashboard_at_most "dashboard_min_schedulable_workers_per_zone" dashboard_min_schedulable_workers_per_zone 0
  wait_for_dashboard_at_least "dashboard_zone_capacity_depleted" dashboard_zone_capacity_depleted 1
  wait_for_dashboard_at_least "dashboard_zone_coverage_gaps" dashboard_zone_coverage_gaps 1
  wait_for_dashboard_at_least "dashboard_cordoned_worker_seconds_10m" dashboard_cordoned_worker_seconds_10m "${min_range_signal_seconds}"
  wait_for_dashboard_at_least "dashboard_zone_coverage_gap_seconds_10m" dashboard_zone_coverage_gap_seconds_10m "${min_range_signal_seconds}"
  wait_for_dashboard_at_most "dashboard_unavailable_app_replicas" dashboard_unavailable_app_replicas 0
  wait_for_dashboard_at_most "dashboard_pending_pods" dashboard_pending_pods 0
  wait_for_dashboard_at_most "dashboard_unschedulable_pods" dashboard_unschedulable_pods 0
  wait_for_dashboard_at_most "dashboard_stateful_scheduling_blockers" dashboard_stateful_scheduling_blockers 0
  wait_for_dashboard_at_most "dashboard_stateful_ready_gap" dashboard_stateful_ready_gap 0
  wait_for_dashboard_at_most "dashboard_cnpg_unhealthy" dashboard_cnpg_unhealthy 0
}

restore_zone_nodes() {
  if [[ "${uncordon}" != "true" || "${nodes_cordoned}" != "true" ]]; then
    return 0
  fi

  for node in "${nodes[@]}"; do
    kubectl uncordon "${node}" >/dev/null
  done
  nodes_cordoned="false"
}

rebalance_app_deployments() {
  if [[ -z "${pod_selector}" || "${expect_blocked}" == "true" ]]; then
    return 0
  fi

  NAMESPACE="${namespace}" \
    DEPLOYMENT_SELECTOR="app.kubernetes.io/instance=${release},app.kubernetes.io/name=tbite-platform" \
    scripts/local-ha/rebalance-apps.sh
}

wait_for_dashboard_zone_recovery() {
  if [[ -z "${pod_selector}" || "${expect_blocked}" == "true" ]]; then
    return 0
  fi

  wait_for_dashboard_at_most "dashboard_cordoned_workers" dashboard_cordoned_workers 0
  wait_for_dashboard_at_least "dashboard_min_schedulable_workers_per_zone" dashboard_min_schedulable_workers_per_zone 1
  wait_for_dashboard_at_most "dashboard_zone_capacity_depleted" dashboard_zone_capacity_depleted 0
  wait_for_dashboard_at_most "dashboard_zone_coverage_gaps" dashboard_zone_coverage_gaps 0
  wait_for_dashboard_at_most "dashboard_unavailable_app_replicas" dashboard_unavailable_app_replicas 0
  wait_for_dashboard_at_most "dashboard_pending_pods" dashboard_pending_pods 0
  wait_for_dashboard_at_most "dashboard_unschedulable_pods" dashboard_unschedulable_pods 0
  wait_for_dashboard_at_most "dashboard_stateful_ready_gap" dashboard_stateful_ready_gap 0
  wait_for_dashboard_at_most "dashboard_cnpg_unhealthy" dashboard_cnpg_unhealthy 0
}

wait_for_dashboard_expected_blocker() {
  if [[ "${expect_blocked}" != "true" ]]; then
    return 0
  fi

  start_vm_port_forward
  wait_for_dashboard_at_least "dashboard_cordoned_workers" dashboard_cordoned_workers "${#nodes[@]}"
  wait_for_dashboard_at_most "dashboard_min_schedulable_workers_per_zone" dashboard_min_schedulable_workers_per_zone 0
  wait_for_dashboard_at_least "dashboard_zone_capacity_depleted" dashboard_zone_capacity_depleted 1
  wait_for_dashboard_at_least "dashboard_cordoned_worker_seconds_10m" dashboard_cordoned_worker_seconds_10m "${min_range_signal_seconds}"
  wait_for_dashboard_at_least "dashboard_unschedulable_pods" dashboard_unschedulable_pods 1
  wait_for_dashboard_at_least "dashboard_stateful_scheduling_blockers" dashboard_stateful_scheduling_blockers 1
  wait_for_dashboard_at_least "dashboard_unschedulable_pod_seconds_10m" dashboard_unschedulable_pod_seconds_10m "${min_range_signal_seconds}"
  wait_for_dashboard_at_least "dashboard_stateful_scheduling_blocker_seconds_10m" dashboard_stateful_scheduling_blocker_seconds_10m "${min_range_signal_seconds}"
  wait_for_dashboard_at_least "dashboard_stateful_ready_gap" dashboard_stateful_ready_gap 1
  wait_for_dashboard_at_least "dashboard_stateful_ready_gap_seconds_10m" dashboard_stateful_ready_gap_seconds_10m "${min_range_signal_seconds}"
}

node_is_in_drained_zone() {
  local candidate="$1"
  local node
  for node in "${nodes[@]}"; do
    if [[ "${node}" == "${candidate}" ]]; then
      return 0
    fi
  done
  return 1
}

detect_cnpg_primary_location() {
  local primary_node_cordoned="false"

  cnpg_primary_before="$(kubectl -n "${namespace}" get cluster "${cnpg_cluster}" -o jsonpath='{.status.currentPrimary}')"
  cnpg_primary_node_before="$(kubectl -n "${namespace}" get pod "${cnpg_primary_before}" -o jsonpath='{.spec.nodeName}')"
  if node_is_in_drained_zone "${cnpg_primary_node_before}"; then
    primary_node_cordoned="true"
    expect_cnpg_role_change="true"
  fi

  printf 'cnpg_primary_before=%s\n' "${cnpg_primary_before}"
  printf 'cnpg_primary_node_before=%s\n' "${cnpg_primary_node_before}"
  printf 'cnpg_primary_node_cordoned=%s\n' "${primary_node_cordoned}"
  printf 'expect_cnpg_role_change=%s\n' "${expect_cnpg_role_change}"
}

pinned_claims_on_node() {
  local node="$1"
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

pinned_pvc_pods_on_node() {
  local node="$1"
  local pinned_claims pod_claims
  pinned_claims="$(pinned_claims_on_node "${node}")"
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
      printf '%s\t%s\t%s\n' "${node}" "${pod}" "${claim}"
    fi
  done <<<"${pod_claims}"
}

blocked_pinned_pvc_pods() {
  local node pinned_claims
  for node in "${nodes[@]}"; do
    pinned_claims="$(pinned_claims_on_node "${node}")"
    if [[ -z "${pinned_claims}" ]]; then
      continue
    fi

    kubectl -n "${namespace}" get pods -o json \
      | jq -r --arg pinnedNode "${node}" --arg claims "${pinned_claims}" '
          ($claims | split("\n") | map(select(. != ""))) as $claims
          | .items[]
          | .metadata.name as $pod
          | .status.phase as $phase
          | (.spec.nodeName // "") as $currentNode
          | ((.status.conditions[]? | select(.type == "PodScheduled") | .reason) // "") as $scheduleReason
          | .spec.volumes[]?
          | select(.persistentVolumeClaim.claimName != null)
          | .persistentVolumeClaim.claimName as $claim
          | select($claims | index($claim))
          | select($phase != "Running" and $phase != "Succeeded")
          | [$pinnedNode, $pod, $claim, $phase, $currentNode, $scheduleReason]
          | @tsv
        '
  done
}

wait_for_expected_blocker() {
  local deadline blocked
  deadline=$((SECONDS + blocked_timeout))
  echo "==> waiting for pinned PVC blocker in zone ${zone}"
  while (( SECONDS < deadline )); do
    blocked="$(blocked_pinned_pvc_pods)"
    if [[ -n "${blocked}" ]]; then
      echo "==> expected pinned PVC blocker observed"
      printf 'pinned_node\tpod\tclaim\tphase\tcurrent_node\tschedule_reason\n'
      printf '%s\n' "${blocked}"
      kubectl -n "${namespace}" get pods --field-selector=status.phase!=Running,status.phase!=Succeeded -o wide || true
      return 0
    fi
    sleep "${poll_seconds}"
  done

  echo "timed out waiting for pinned PVC pods to become blocked in zone ${zone}" >&2
  for node in "${nodes[@]}"; do
    echo "pinned claims on ${node}:" >&2
    pinned_claims_on_node "${node}" >&2 || true
  done
  kubectl -n "${namespace}" get pods -o wide >&2 || true
  return 1
}

assert_selected_pods_evacuated() {
  local node_json selected
  if [[ -z "${pod_selector}" ]]; then
    return 0
  fi

  node_json="$(printf '%s\n' "${nodes[@]}" | jq -R . | jq -s .)"
  selected="$(
    kubectl -n "${namespace}" get pods --selector "${pod_selector}" -o json \
      | jq -r --argjson nodes "${node_json}" '
          .items[]
          | select(.metadata.deletionTimestamp == null)
          | select(.status.phase != "Succeeded" and .status.phase != "Failed")
          | select((.spec.nodeName // "") as $node | $nodes | index($node))
          | [.metadata.name, .status.phase, (.spec.nodeName // "")]
          | @tsv
        '
  )"
  if [[ -n "${selected}" ]]; then
    echo "selected pods are still assigned to drained zone ${zone}:" >&2
    printf 'pod\tphase\tnode\n' >&2
    printf '%s\n' "${selected}" >&2
    exit 1
  fi

  echo "==> selected pods evacuated from zone ${zone}"
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
      echo "platform app pods entered CrashLoopBackOff during the zone drain:" >&2
      printf 'pod\tcontainers\n' >&2
      printf '%s\n' "${crashing}" >&2
      return 1
    fi
    sleep "${poll_seconds}"
  done
}

if [[ -z "${pod_selector}" && "${allow_pinned_pvc_drain}" != "true" ]]; then
  pinned=""
  for node in "${nodes[@]}"; do
    node_pinned="$(pinned_pvc_pods_on_node "${node}")"
    if [[ -n "${node_pinned}" ]]; then
      pinned+="${node_pinned}"$'\n'
    fi
  done
  if [[ -n "${pinned}" ]]; then
    cat >&2 <<EOF
Refusing full-zone drain of ${zone}: one or more pods use local-path PVs pinned to zone nodes.

Pinned pods:
$(printf '%s' "${pinned}" | sed 's/^/  /')

kind's default storage class is rancher.io/local-path, so these StatefulSet pods
cannot reschedule while their nodes are cordoned. Use one of:
  DRAIN_POD_SELECTOR='app.kubernetes.io/component in (api,realtime,web-employee,web-merchant,web-admin,worker-outbox-relay,worker-payroll-settler,worker-on-time-evaluator,scheduler-cutoff,scheduler-no-show,scheduler-doc-expiry,scheduler-feedback)' make local-ha-drain-zone
  make local-ha-drain-zone-apps
  ALLOW_PINNED_PVC_DRAIN=true make local-ha-drain-zone
EOF
    exit 2
  fi
fi

if [[ "${expect_blocked}" == "true" ]]; then
  has_pinned_claims="false"
  for node in "${nodes[@]}"; do
    if [[ -n "$(pinned_claims_on_node "${node}")" ]]; then
      has_pinned_claims="true"
      break
    fi
  done
  if [[ "${has_pinned_claims}" != "true" ]]; then
    echo "EXPECT_BLOCKED=true requested, but zone ${zone} has no local-path PV claims pinned to its nodes." >&2
    exit 2
  fi
fi

echo "==> before zone drain: ${zone}"
wait_for_dashboard_baseline
if [[ "${expect_blocked}" != "true" ]]; then
  detect_cnpg_primary_location
  baseline_cnpg_role_changes="$(dashboard_cnpg_role_changes)"
  baseline_cnpg_role_changes="${baseline_cnpg_role_changes:-0}"
  printf 'baseline_dashboard_cnpg_role_changes=%s\n' "${baseline_cnpg_role_changes}"
fi
kubectl get nodes -L topology.kubernetes.io/zone
kubectl -n "${namespace}" get pods -o wide

echo "==> cordoning all nodes in zone ${zone}"
for node in "${nodes[@]}"; do
  kubectl cordon "${node}"
done
nodes_cordoned="true"

for node in "${nodes[@]}"; do
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
  kubectl drain "${drain_args[@]}"
done

if [[ "${expect_blocked}" == "true" ]]; then
  wait_for_expected_blocker
  wait_for_dashboard_expected_blocker
  assert_no_app_crashloops
  echo "==> expected blockers retained in zone ${zone}"
  printf 'dashboard_cordoned_worker_seconds_10m=%s\n' "$(dashboard_cordoned_worker_seconds_10m)"
  printf 'dashboard_unavailable_app_seconds_10m=%s\n' "$(dashboard_unavailable_app_seconds_10m)"
  printf 'dashboard_stateful_scheduling_blockers=%s\n' "$(dashboard_stateful_scheduling_blockers)"
  printf 'dashboard_stateful_scheduling_blocker_seconds_10m=%s\n' "$(dashboard_stateful_scheduling_blocker_seconds_10m)"
  printf 'dashboard_unschedulable_pod_seconds_10m=%s\n' "$(dashboard_unschedulable_pod_seconds_10m)"
  printf 'dashboard_stateful_ready_gap_seconds_10m=%s\n' "$(dashboard_stateful_ready_gap_seconds_10m)"
  kubectl get nodes -L topology.kubernetes.io/zone
  kubectl -n "${namespace}" get pods -o wide
  exit 0
fi

scripts/local-ha/wait-ready.sh
assert_selected_pods_evacuated
wait_for_dashboard_zone_drain_observed
assert_no_app_crashloops
if [[ "${expect_cnpg_role_change}" == "true" ]]; then
  wait_for_dashboard_greater_than "dashboard_cnpg_role_changes" dashboard_cnpg_role_changes "${baseline_cnpg_role_changes}"
fi

echo "==> restoring zone ${zone}"
restore_zone_nodes
rebalance_app_deployments
wait_for_dashboard_zone_recovery
printf 'dashboard_cordoned_worker_seconds_10m=%s\n' "$(dashboard_cordoned_worker_seconds_10m)"
printf 'dashboard_unavailable_app_seconds_10m=%s\n' "$(dashboard_unavailable_app_seconds_10m)"
printf 'dashboard_zone_coverage_gap_seconds_10m=%s\n' "$(dashboard_zone_coverage_gap_seconds_10m)"

echo "==> drained zone ${zone}"
kubectl -n "${namespace}" get pods -o wide
