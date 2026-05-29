#!/usr/bin/env bash
set -euo pipefail

cd "$(dirname "$0")/../.."

namespace="${NAMESPACE:-tbite}"
release="${RELEASE:-tbite}"
zone="${ZONE:-}"
api_deployment="${API_DEPLOYMENT:-${release}-tbite-platform-api}"
api_hpa="${API_HPA:-${release}-tbite-platform-api}"
pod_selector="${DRAIN_POD_SELECTOR:-app.kubernetes.io/instance=${release},app.kubernetes.io/name=tbite-platform,app.kubernetes.io/component=api}"
timeout_seconds="${TIMEOUT_SECONDS:-300}"
poll_seconds="${POLL_SECONDS:-5}"
min_range_signal_seconds="${MIN_RANGE_SIGNAL_SECONDS:-15}"
drain_timeout="${DRAIN_TIMEOUT:-10m}"
crashloop_observe_seconds="${CRASHLOOP_OBSERVE_SECONDS:-60}"
restore="${RESTORE:-true}"
vm_service="${VM_SERVICE:-vmsingle-${release}-victoria-metrics-k8s-stack}"
vm_url="${VM_URL:-}"
vm_local_port="${VM_LOCAL_PORT:-18428}"
app_component_regex="${APP_COMPONENT_REGEX:-api|realtime|web-employee|web-merchant|web-admin|worker-outbox-relay|worker-payroll-settler|worker-on-time-evaluator|scheduler-cutoff|scheduler-no-show|scheduler-doc-expiry|scheduler-feedback}"
port_forward_pid=""
nodes_cordoned="false"

nodes=()

float_ge() {
  awk -v left="$1" -v right="$2" 'BEGIN { exit !(left >= right) }'
}

float_gt() {
  awk -v left="$1" -v right="$2" 'BEGIN { exit !(left > right) }'
}

float_le() {
  awk -v left="$1" -v right="$2" 'BEGIN { exit !(left <= right) }'
}

default_zone() {
  local nodes_json pods_json selected
  nodes_json="$(kubectl get nodes --selector='!node-role.kubernetes.io/control-plane' -o json)"
  pods_json="$(kubectl -n "${namespace}" get pods -l "app.kubernetes.io/instance=${release},app.kubernetes.io/name=tbite-platform,app.kubernetes.io/component=api" -o json)"
  selected="$(
    jq -r -n --argjson nodes "${nodes_json}" --argjson pods "${pods_json}" '
      ($nodes.items
       | map({node: .metadata.name, zone: (.metadata.labels["topology.kubernetes.io/zone"] // "")})
       | map(select(.zone != ""))) as $workers
      | ($pods.items
         | map(select(.status.phase == "Running"))
         | map(.spec.nodeName // "")
         | map(select(. != ""))) as $podNodes
      | $workers
      | map(select(.node as $node | $podNodes | index($node)))
      | group_by(.zone)
      | map({zone: .[0].zone, count: length})
      | sort_by(.count, .zone)
      | reverse
      | .[0].zone // empty
    '
  )"
  if [[ -n "${selected}" ]]; then
    printf '%s\n' "${selected}"
    return 0
  fi

  kubectl get nodes --selector='!node-role.kubernetes.io/control-plane' -o jsonpath='{.items[0].metadata.labels.topology\.kubernetes\.io/zone}'
}

if [[ -z "${zone}" ]]; then
  zone="$(default_zone)"
fi
if [[ -z "${zone}" ]]; then
  echo "could not resolve a worker zone for the combined drain/autoscale drill" >&2
  exit 1
fi

while IFS= read -r node; do
  [[ -n "${node}" ]] && nodes+=("${node}")
done < <(kubectl get nodes --selector="topology.kubernetes.io/zone=${zone},!node-role.kubernetes.io/control-plane" -o jsonpath='{range .items[*]}{.metadata.name}{"\n"}{end}')
if [[ "${#nodes[@]}" -eq 0 ]]; then
  echo "no worker nodes found for zone ${zone}" >&2
  exit 1
fi

cleanup() {
  local status="$?"
  trap - EXIT
  set +e
  if [[ "${restore}" == "true" && "${nodes_cordoned}" == "true" ]]; then
    for node in "${nodes[@]}"; do
      kubectl uncordon "${node}" >/dev/null 2>&1 || true
    done
  fi
  if [[ -n "${port_forward_pid}" ]]; then
    kill "${port_forward_pid}" >/dev/null 2>&1 || true
  fi
  exit "${status}"
}
trap cleanup EXIT

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
  promql_value 'min(sum by (label_topology_kubernetes_io_zone) (kube_node_status_condition{condition="Ready",status="true"} * on(node) group_left() (1 - kube_node_spec_unschedulable) * on(node) group_left(label_topology_kubernetes_io_zone) kube_node_labels{label_topology_kubernetes_io_zone!=""})) or vector(0)'
}

dashboard_zone_capacity_depleted() {
  promql_value '((min(sum by (label_topology_kubernetes_io_zone) (kube_node_status_condition{condition="Ready",status="true"} * on(node) group_left() (1 - kube_node_spec_unschedulable) * on(node) group_left(label_topology_kubernetes_io_zone) kube_node_labels{label_topology_kubernetes_io_zone!=""})) or vector(0)) < bool 1)'
}

dashboard_cordoned_workers() {
  promql_value 'sum(kube_node_spec_unschedulable * on(node) group_left(label_topology_kubernetes_io_zone) kube_node_labels{label_topology_kubernetes_io_zone!=""}) or vector(0)'
}

dashboard_cordoned_worker_seconds_10m() {
  promql_value '(sum_over_time(((sum(kube_node_spec_unschedulable * on(node) group_left(label_topology_kubernetes_io_zone) kube_node_labels{label_topology_kubernetes_io_zone!=""}) or vector(0)) > bool 0)[10m:15s]) or vector(0)) * 15'
}

dashboard_zone_coverage_gaps() {
  promql_value "$(cat <<QUERY
count((count by (label_app_kubernetes_io_component) (count by (label_app_kubernetes_io_component, label_topology_kubernetes_io_zone) (kube_pod_status_phase{namespace="${namespace}",phase="Running"} * on(namespace,pod) group_left(node) kube_pod_info{namespace="${namespace}"} * on(node) group_left(label_topology_kubernetes_io_zone) kube_node_labels{label_topology_kubernetes_io_zone!=""} * on(namespace,pod) group_left(label_app_kubernetes_io_component) kube_pod_labels{namespace="${namespace}",label_app_kubernetes_io_instance="${release}",label_app_kubernetes_io_name="tbite-platform",label_app_kubernetes_io_component=~"${app_component_regex}"}))) < on(label_app_kubernetes_io_component) (clamp_max(max by (label_app_kubernetes_io_component) (label_replace(kube_deployment_spec_replicas{namespace="${namespace}",deployment=~"${release}-tbite-platform-.*"}, "label_app_kubernetes_io_component", "\$1", "deployment", "${release}-tbite-platform-(.*)")), 3))) or vector(0)
QUERY
)"
}

dashboard_zone_coverage_gap_seconds_10m() {
  promql_value "$(cat <<QUERY
(sum_over_time(((count((count by (label_app_kubernetes_io_component) (count by (label_app_kubernetes_io_component, label_topology_kubernetes_io_zone) (kube_pod_status_phase{namespace="${namespace}",phase="Running"} * on(namespace,pod) group_left(node) kube_pod_info{namespace="${namespace}"} * on(node) group_left(label_topology_kubernetes_io_zone) kube_node_labels{label_topology_kubernetes_io_zone!=""} * on(namespace,pod) group_left(label_app_kubernetes_io_component) kube_pod_labels{namespace="${namespace}",label_app_kubernetes_io_instance="${release}",label_app_kubernetes_io_name="tbite-platform",label_app_kubernetes_io_component=~"${app_component_regex}"}))) < on(label_app_kubernetes_io_component) (clamp_max(max by (label_app_kubernetes_io_component) (label_replace(kube_deployment_spec_replicas{namespace="${namespace}",deployment=~"${release}-tbite-platform-.*"}, "label_app_kubernetes_io_component", "\$1", "deployment", "${release}-tbite-platform-(.*)")), 3))) or vector(0)) > bool 0)[10m:15s]) or vector(0)) * 15
QUERY
)"
}

dashboard_unavailable_app_replicas() {
  promql_value "sum(kube_deployment_status_replicas_unavailable{namespace=\"${namespace}\",deployment=~\"${release}-tbite-platform-.*\"}) or vector(0)"
}

dashboard_pending_pods() {
  promql_value "sum(kube_pod_status_phase{namespace=\"${namespace}\",phase=\"Pending\"} == 1) or vector(0)"
}

dashboard_unschedulable_pods() {
  promql_value "sum(kube_pod_status_unschedulable{namespace=\"${namespace}\"}) or vector(0)"
}

dashboard_api_ready_pods() {
  promql_value "sum(kube_pod_status_ready{namespace=\"${namespace}\",pod=~\"${api_deployment}-.*\",condition=\"true\"}) or vector(0)"
}

dashboard_api_current() {
  promql_value "sum(kube_horizontalpodautoscaler_status_current_replicas{namespace=\"${namespace}\",horizontalpodautoscaler=\"${api_hpa}\"}) or vector(0)"
}

dashboard_api_desired() {
  promql_value "sum(kube_horizontalpodautoscaler_status_desired_replicas{namespace=\"${namespace}\",horizontalpodautoscaler=\"${api_hpa}\"}) or vector(0)"
}

dashboard_api_scale_event_seconds_10m() {
  promql_value "(sum_over_time((((clamp_min((sum(kube_horizontalpodautoscaler_status_desired_replicas{namespace=\"${namespace}\",horizontalpodautoscaler=\"${api_hpa}\"}) or vector(0)) - (sum(kube_horizontalpodautoscaler_spec_min_replicas{namespace=\"${namespace}\",horizontalpodautoscaler=\"${api_hpa}\"}) or vector(0)), 0) > bool 0) + (clamp_min((sum(kube_horizontalpodautoscaler_status_current_replicas{namespace=\"${namespace}\",horizontalpodautoscaler=\"${api_hpa}\"}) or vector(0)) - (sum(kube_horizontalpodautoscaler_spec_min_replicas{namespace=\"${namespace}\",horizontalpodautoscaler=\"${api_hpa}\"}) or vector(0)), 0) > bool 0) + (clamp_min((sum(kube_horizontalpodautoscaler_status_target_metric{namespace=\"${namespace}\",horizontalpodautoscaler=\"${api_hpa}\",metric_name=\"cpu\",metric_target_type=\"utilization\"}) or vector(0)) - (sum(kube_horizontalpodautoscaler_spec_target_metric{namespace=\"${namespace}\",horizontalpodautoscaler=\"${api_hpa}\",metric_name=\"cpu\",metric_target_type=\"utilization\"}) or vector(0)), 0) > bool 0)) > bool 0)[10m:15s]) or vector(0)) * 15"
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

wait_for_dashboard_activity_present() {
  local name="$1"
  local metric_func="$2"

  wait_for_dashboard_at_least "${name}" "${metric_func}" "${min_range_signal_seconds}"
}

wait_for_dashboard_baseline() {
  start_vm_port_forward
  wait_for_dashboard_at_least "dashboard_min_schedulable_workers_per_zone" dashboard_min_schedulable_workers_per_zone 1
  wait_for_dashboard_at_most "dashboard_zone_capacity_depleted" dashboard_zone_capacity_depleted 0
  wait_for_dashboard_at_most "dashboard_cordoned_workers" dashboard_cordoned_workers 0
  wait_for_dashboard_at_most "dashboard_zone_coverage_gaps" dashboard_zone_coverage_gaps 0
  wait_for_dashboard_at_most "dashboard_unavailable_app_replicas" dashboard_unavailable_app_replicas 0
  wait_for_dashboard_at_most "dashboard_pending_pods" dashboard_pending_pods 0
  wait_for_dashboard_at_most "dashboard_unschedulable_pods" dashboard_unschedulable_pods 0
}

assert_selected_pods_evacuated() {
  local node_json selected
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
            | select(($containers | length) > 0)
            | [$pod, ($containers | join(","))]
            | @tsv'
    )"
    if [[ -n "${crashing}" ]]; then
      echo "platform app pods entered CrashLoopBackOff during zone drain + API autoscale:" >&2
      printf 'pod\tcontainers\n' >&2
      printf '%s\n' "${crashing}" >&2
      exit 1
    fi
    sleep "${poll_seconds}"
  done
}

restore_zone_nodes() {
  if [[ "${restore}" != "true" || "${nodes_cordoned}" != "true" ]]; then
    return 0
  fi
  for node in "${nodes[@]}"; do
    kubectl uncordon "${node}" >/dev/null
  done
  nodes_cordoned="false"
}

rebalance_api_deployment() {
  NAMESPACE="${namespace}" \
    DEPLOYMENT_SELECTOR="app.kubernetes.io/instance=${release},app.kubernetes.io/name=tbite-platform,app.kubernetes.io/component=api" \
    scripts/local-ha/rebalance-apps.sh
}

wait_for_dashboard_zone_recovery() {
  wait_for_dashboard_at_most "dashboard_cordoned_workers" dashboard_cordoned_workers 0
  wait_for_dashboard_at_least "dashboard_min_schedulable_workers_per_zone" dashboard_min_schedulable_workers_per_zone 1
  wait_for_dashboard_at_most "dashboard_zone_capacity_depleted" dashboard_zone_capacity_depleted 0
  wait_for_dashboard_at_most "dashboard_zone_coverage_gaps" dashboard_zone_coverage_gaps 0
  wait_for_dashboard_at_most "dashboard_unavailable_app_replicas" dashboard_unavailable_app_replicas 0
  wait_for_dashboard_at_most "dashboard_pending_pods" dashboard_pending_pods 0
  wait_for_dashboard_at_most "dashboard_unschedulable_pods" dashboard_unschedulable_pods 0
}

wait_for_dashboard_baseline
baseline_cordoned_worker_seconds="$(dashboard_cordoned_worker_seconds_10m)"
baseline_cordoned_worker_seconds="${baseline_cordoned_worker_seconds:-0}"
baseline_zone_gap_seconds="$(dashboard_zone_coverage_gap_seconds_10m)"
baseline_zone_gap_seconds="${baseline_zone_gap_seconds:-0}"
baseline_api_scale_event_seconds="$(dashboard_api_scale_event_seconds_10m)"
baseline_api_scale_event_seconds="${baseline_api_scale_event_seconds:-0}"

printf 'target_zone=%s\n' "${zone}"
printf 'target_nodes=%s\n' "${nodes[*]}"
printf 'baseline_dashboard_cordoned_worker_seconds_10m=%s\n' "${baseline_cordoned_worker_seconds}"
printf 'baseline_dashboard_zone_coverage_gap_seconds_10m=%s\n' "${baseline_zone_gap_seconds}"
printf 'baseline_dashboard_api_scale_event_seconds_10m=%s\n' "${baseline_api_scale_event_seconds}"

kubectl get nodes -L topology.kubernetes.io/zone
kubectl -n "${namespace}" get pods -l "${pod_selector}" -o wide

echo "==> cordoning all worker nodes in zone ${zone}"
for node in "${nodes[@]}"; do
  kubectl cordon "${node}"
done
nodes_cordoned="true"

for node in "${nodes[@]}"; do
  echo "==> draining selected API pods from ${node}"
  kubectl drain "${node}" \
    --ignore-daemonsets \
    --delete-emptydir-data \
    --timeout="${drain_timeout}" \
    --pod-selector="${pod_selector}"
done

kubectl -n "${namespace}" rollout status "deployment/${api_deployment}" --timeout="${drain_timeout}"
assert_selected_pods_evacuated

wait_for_dashboard_at_least "dashboard_cordoned_workers" dashboard_cordoned_workers "${#nodes[@]}"
wait_for_dashboard_at_most "dashboard_min_schedulable_workers_per_zone" dashboard_min_schedulable_workers_per_zone 0
wait_for_dashboard_at_least "dashboard_zone_capacity_depleted" dashboard_zone_capacity_depleted 1
wait_for_dashboard_at_least "dashboard_zone_coverage_gaps" dashboard_zone_coverage_gaps 1
wait_for_dashboard_activity_present "dashboard_cordoned_worker_seconds_10m" dashboard_cordoned_worker_seconds_10m
wait_for_dashboard_activity_present "dashboard_zone_coverage_gap_seconds_10m" dashboard_zone_coverage_gap_seconds_10m
wait_for_dashboard_at_most "dashboard_unavailable_app_replicas" dashboard_unavailable_app_replicas 0
wait_for_dashboard_at_most "dashboard_pending_pods" dashboard_pending_pods 0
wait_for_dashboard_at_most "dashboard_unschedulable_pods" dashboard_unschedulable_pods 0
assert_no_app_crashloops

echo "==> running API CPU autoscale while zone ${zone} is cordoned"
NAMESPACE="${namespace}" \
RELEASE="${release}" \
VM_URL="${vm_url}" \
SCALE_TIMEOUT_SECONDS="${API_SCALE_TIMEOUT_SECONDS:-300}" \
BASELINE_TIMEOUT_SECONDS="${API_BASELINE_TIMEOUT_SECONDS:-900}" \
scripts/local-ha/scenario-autoscale-api.sh

wait_for_dashboard_activity_present "dashboard_api_scale_event_seconds_10m" dashboard_api_scale_event_seconds_10m
wait_for_dashboard_at_least "dashboard_cordoned_workers" dashboard_cordoned_workers "${#nodes[@]}"
wait_for_dashboard_at_least "dashboard_zone_coverage_gaps" dashboard_zone_coverage_gaps 1
wait_for_dashboard_at_most "dashboard_api_desired" dashboard_api_desired "$(kubectl -n "${namespace}" get hpa "${api_hpa}" -o jsonpath='{.spec.minReplicas}')"
wait_for_dashboard_at_most "dashboard_api_current" dashboard_api_current "$(kubectl -n "${namespace}" get hpa "${api_hpa}" -o jsonpath='{.spec.minReplicas}')"
wait_for_dashboard_at_most "dashboard_unavailable_app_replicas" dashboard_unavailable_app_replicas 0
wait_for_dashboard_at_most "dashboard_pending_pods" dashboard_pending_pods 0
wait_for_dashboard_at_most "dashboard_unschedulable_pods" dashboard_unschedulable_pods 0
assert_no_app_crashloops

echo "==> restoring zone ${zone}"
restore_zone_nodes
rebalance_api_deployment
wait_for_dashboard_zone_recovery

printf 'recovery_dashboard_cordoned_worker_seconds_10m=%s\n' "$(dashboard_cordoned_worker_seconds_10m)"
printf 'recovery_dashboard_zone_coverage_gap_seconds_10m=%s\n' "$(dashboard_zone_coverage_gap_seconds_10m)"
printf 'recovery_dashboard_api_scale_event_seconds_10m=%s\n' "$(dashboard_api_scale_event_seconds_10m)"
kubectl -n "${namespace}" get hpa "${api_hpa}" -o wide
kubectl -n "${namespace}" get pods -l "${pod_selector}" -o wide
