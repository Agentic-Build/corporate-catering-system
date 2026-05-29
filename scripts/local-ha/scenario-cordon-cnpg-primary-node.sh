#!/usr/bin/env bash
set -euo pipefail

cd "$(dirname "$0")/../.."

namespace="${NAMESPACE:-tbite}"
release="${RELEASE:-tbite}"
cluster="${CNPG_CLUSTER:-${release}-pg}"
database_rw_url_secret="${DATABASE_RW_URL_SECRET:-${release}-db}"
database_rw_url_key="${DATABASE_RW_URL_KEY:-rwUrl}"
timeout_seconds="${TIMEOUT_SECONDS:-300}"
poll_seconds="${POLL_SECONDS:-5}"
max_replication_lag="${MAX_REPLICATION_LAG:-5}"
vm_service="${VM_SERVICE:-vmsingle-${release}-victoria-metrics-k8s-stack}"
vm_url="${VM_URL:-}"
vm_local_port="${VM_LOCAL_PORT:-18428}"
port_forward_pid=""
primary_before=""
primary_node_before=""
node_cordoned=false
baseline_role_changes=0
baseline_cordon_switchover_events=0

cleanup() {
  if [[ "${node_cordoned}" == "true" && -n "${primary_node_before}" ]]; then
    kubectl uncordon "${primary_node_before}" >/dev/null 2>&1 || true
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

cnpg_primary_on_cordoned_node_query() {
  printf '%s\n' "sum(((1 - cnpg_pg_replication_in_recovery{namespace=\"${namespace}\",pod=~\"${cluster}-[0-9]+\"}) == 1) * on(namespace,pod) group_left(node) (max by (namespace, pod, node) (kube_pod_info{namespace=\"${namespace}\",pod=~\"${cluster}-[0-9]+\"})) * on(node) group_left() (max by (node) (kube_node_spec_unschedulable))) or vector(0)"
}

dashboard_node_unschedulable() {
  promql_value "max(kube_node_spec_unschedulable{node=\"${primary_node_before}\"}) or vector(0)"
}

dashboard_cordoned_workers() {
  promql_value 'sum((max by (node) (kube_node_spec_unschedulable)) * on(node) group_left(label_topology_kubernetes_io_zone) (max by (node, label_topology_kubernetes_io_zone) (kube_node_labels{label_topology_kubernetes_io_zone!=""}))) or vector(0)'
}

dashboard_cordoned_worker_seconds_10m() {
  promql_value '(sum_over_time(((sum((max by (node) (kube_node_spec_unschedulable)) * on(node) group_left(label_topology_kubernetes_io_zone) (max by (node, label_topology_kubernetes_io_zone) (kube_node_labels{label_topology_kubernetes_io_zone!=""}))) or vector(0)) > bool 0)[10m:15s]) or vector(0)) * 15'
}

dashboard_cnpg_collectors_up() {
  promql_value "sum(cnpg_collector_up{namespace=\"${namespace}\",cluster=\"${cluster}\"}) or vector(0)"
}

dashboard_cnpg_primaries() {
  promql_value "sum(1 - cnpg_pg_replication_in_recovery{namespace=\"${namespace}\",pod=~\"${cluster}-[0-9]+\"}) or vector(0)"
}

dashboard_cnpg_ready_pods() {
  promql_value "sum(kube_pod_status_ready{namespace=\"${namespace}\",pod=~\"${cluster}-[0-9]+\",condition=\"true\"}) or vector(0)"
}

dashboard_cnpg_role_changes() {
  promql_value "sum(changes(cnpg_pg_replication_in_recovery{namespace=\"${namespace}\",pod=~\"${cluster}-[0-9]+\"}[10m])) or vector(0)"
}

dashboard_cnpg_cordon_switchover_events() {
  promql_value "(ceil(((sum(changes(cnpg_pg_replication_in_recovery{namespace=\"${namespace}\",pod=~\"${cluster}-[0-9]+\"}[10m])) or vector(0)) / 2)) * on() ((sum((max by (node) (max_over_time(kube_node_spec_unschedulable[10m])))) or vector(0)) > bool 0))"
}

dashboard_cnpg_primary_on_cordoned_node() {
  promql_value "$(cnpg_primary_on_cordoned_node_query)"
}

dashboard_cnpg_primary_on_cordoned_node_seconds_10m() {
  promql_value "(sum_over_time((($(cnpg_primary_on_cordoned_node_query)) > bool 0)[10m:15s]) or vector(0)) * 15"
}

dashboard_cnpg_max_replication_lag() {
  promql_value "max(cnpg_pg_replication_lag{namespace=\"${namespace}\",pod=~\"${cluster}-[0-9]+\"}) or vector(0)"
}

dashboard_cnpg_rw_endpoint_count() {
  promql_value "sum(kube_endpointslice_endpoints{namespace=\"${namespace}\",endpointslice=~\"${cluster}-rw-.*\",ready=\"true\"}) or vector(0)"
}

dashboard_cnpg_rw_endpoint_is_primary() {
  promql_value "sum(label_replace(kube_endpointslice_endpoints{namespace=\"${namespace}\",endpointslice=~\"${cluster}-rw-.*\",ready=\"true\"}, \"pod\", \"\$1\", \"targetref_name\", \"(.*)\") and on(namespace,pod) ((1 - cnpg_pg_replication_in_recovery{namespace=\"${namespace}\",pod=~\"${cluster}-[0-9]+\"}) == 1)) or vector(0)"
}

dashboard_cnpg_unhealthy() {
  promql_value "(((sum(cnpg_collector_up{namespace=\"${namespace}\",cluster=\"${cluster}\"}) or vector(0)) < bool ${instances}) + ((sum(1 - cnpg_pg_replication_in_recovery{namespace=\"${namespace}\",pod=~\"${cluster}-[0-9]+\"}) or vector(0)) != bool 1) + ((max(cnpg_pg_replication_lag{namespace=\"${namespace}\",pod=~\"${cluster}-[0-9]+\"}) or vector(0)) > bool ${max_replication_lag}) + ((sum(kube_pod_status_ready{namespace=\"${namespace}\",pod=~\"${cluster}-[0-9]+\",condition=\"true\"}) or vector(0)) < bool ${instances}) + ((sum(kube_endpointslice_endpoints{namespace=\"${namespace}\",endpointslice=~\"${cluster}-rw-.*\",ready=\"true\"}) or vector(0)) != bool 1) + ((sum(label_replace(kube_endpointslice_endpoints{namespace=\"${namespace}\",endpointslice=~\"${cluster}-rw-.*\",ready=\"true\"}, \"pod\", \"\$1\", \"targetref_name\", \"(.*)\") and on(namespace,pod) ((1 - cnpg_pg_replication_in_recovery{namespace=\"${namespace}\",pod=~\"${cluster}-[0-9]+\"}) == 1)) or vector(0)) != bool 1))"
}

dashboard_pdb_policy_blockers() {
  promql_value "((sum((max by (namespace, pod, node) (kube_pod_info{namespace=\"${namespace}\"})) * on(namespace,pod) group_left(owner_kind,owner_name) (max by (namespace, pod, owner_kind, owner_name) (kube_pod_owner{namespace=\"${namespace}\",owner_kind=\"StatefulSet\",owner_is_controller=\"true\"})) * on(node) group_left() (max by (node) (kube_node_spec_unschedulable)) * on(namespace,owner_name) group_left() label_replace((max by (namespace, poddisruptionbudget) (kube_poddisruptionbudget_status_pod_disruptions_allowed{namespace=\"${namespace}\"}) == bool 0), \"owner_name\", \"\$1\", \"poddisruptionbudget\", \"(.*)\")) or vector(0)) + (sum((max by (namespace, pod, node) (kube_pod_info{namespace=\"${namespace}\",pod=~\"${release}-tbite-platform-.*\"})) * on(namespace,pod) group_left(label_app_kubernetes_io_component) (max by (namespace, pod, label_app_kubernetes_io_component) (kube_pod_labels{namespace=\"${namespace}\",label_app_kubernetes_io_instance=\"${release}\",label_app_kubernetes_io_name=\"tbite-platform\",label_app_kubernetes_io_component=~\"api|realtime|web-employee|web-merchant|web-admin|worker-outbox-relay|worker-payroll-settler|worker-on-time-evaluator|scheduler-cutoff|scheduler-no-show|scheduler-doc-expiry|scheduler-feedback\"})) * on(node) group_left() (max by (node) (kube_node_spec_unschedulable)) * on(namespace,label_app_kubernetes_io_component) group_left() label_replace((max by (namespace, poddisruptionbudget) (kube_poddisruptionbudget_status_pod_disruptions_allowed{namespace=\"${namespace}\",poddisruptionbudget=~\"${release}-tbite-platform-.*\"}) == bool 0), \"label_app_kubernetes_io_component\", \"\$1\", \"poddisruptionbudget\", \"${release}-tbite-platform-(.*)\")) or vector(0)))"
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
  wait_for_dashboard_at_most "dashboard_cordoned_workers" dashboard_cordoned_workers 0
  wait_for_dashboard_at_most "dashboard_cnpg_primary_on_cordoned_node" dashboard_cnpg_primary_on_cordoned_node 0
  wait_for_dashboard_at_most "dashboard_pdb_policy_blockers" dashboard_pdb_policy_blockers 0
  wait_for_dashboard_at_least "dashboard_cnpg_collectors_up" dashboard_cnpg_collectors_up "${instances}"
  wait_for_dashboard_at_least "dashboard_cnpg_ready_pods" dashboard_cnpg_ready_pods "${instances}"
  wait_for_dashboard_at_least "dashboard_cnpg_primaries" dashboard_cnpg_primaries 1
  wait_for_dashboard_at_most "dashboard_cnpg_primaries" dashboard_cnpg_primaries 1
  wait_for_dashboard_at_most "dashboard_cnpg_max_replication_lag" dashboard_cnpg_max_replication_lag "${max_replication_lag}"
  wait_for_dashboard_at_least "dashboard_cnpg_rw_endpoint_count" dashboard_cnpg_rw_endpoint_count 1
  wait_for_dashboard_at_most "dashboard_cnpg_rw_endpoint_count" dashboard_cnpg_rw_endpoint_count 1
  wait_for_dashboard_at_least "dashboard_cnpg_rw_endpoint_is_primary" dashboard_cnpg_rw_endpoint_is_primary 1
  wait_for_dashboard_at_most "dashboard_cnpg_rw_endpoint_is_primary" dashboard_cnpg_rw_endpoint_is_primary 1
  wait_for_dashboard_at_most "dashboard_cnpg_unhealthy" dashboard_cnpg_unhealthy 0

  baseline_role_changes="$(dashboard_cnpg_role_changes)"
  baseline_role_changes="${baseline_role_changes:-0}"
  baseline_cordon_switchover_events="$(dashboard_cnpg_cordon_switchover_events)"
  baseline_cordon_switchover_events="${baseline_cordon_switchover_events:-0}"

  printf 'baseline_dashboard_cnpg_role_changes=%s\n' "${baseline_role_changes}"
  printf 'baseline_dashboard_cnpg_cordon_switchover_events=%s\n' "${baseline_cordon_switchover_events}"
}

wait_for_primary_to_move() {
  local deadline current_primary current_node
  deadline=$((SECONDS + timeout_seconds))
  while (( SECONDS < deadline )); do
    current_primary="$(kubectl -n "${namespace}" get cluster "${cluster}" -o jsonpath='{.status.currentPrimary}' 2>/dev/null || true)"
    if [[ -n "${current_primary}" && "${current_primary}" != "${primary_before}" ]]; then
      current_node="$(kubectl -n "${namespace}" get pod "${current_primary}" -o jsonpath='{.spec.nodeName}' 2>/dev/null || true)"
      if [[ -n "${current_node}" && "${current_node}" != "${primary_node_before}" ]]; then
        printf 'current_primary=%s\n' "${current_primary}"
        printf 'current_primary_node=%s\n' "${current_node}"
        return 0
      fi
      printf '%s current_primary=%s current_node=%s waiting_for_node_not_%s\n' "$(date -u +%Y-%m-%dT%H:%M:%SZ)" "${current_primary}" "${current_node:-empty}" "${primary_node_before}"
    else
      printf '%s current_primary=%s waiting_for_not_%s\n' "$(date -u +%Y-%m-%dT%H:%M:%SZ)" "${current_primary:-empty}" "${primary_before}"
    fi
    sleep "${poll_seconds}"
  done

  echo "timed out waiting for CNPG primary to move away from ${primary_before} on ${primary_node_before}" >&2
  return 1
}

wait_for_dashboard_switchover() {
  wait_for_dashboard_at_least "dashboard_node_unschedulable" dashboard_node_unschedulable 1
  wait_for_dashboard_at_least "dashboard_cordoned_workers" dashboard_cordoned_workers 1
  wait_for_dashboard_delta_at_least "dashboard_cnpg_role_changes" dashboard_cnpg_role_changes "${baseline_role_changes}" 1
  wait_for_dashboard_delta_at_least "dashboard_cnpg_cordon_switchover_events" dashboard_cnpg_cordon_switchover_events "${baseline_cordon_switchover_events}" 1
  wait_for_dashboard_at_most "dashboard_cnpg_primary_on_cordoned_node" dashboard_cnpg_primary_on_cordoned_node 0
  wait_for_dashboard_at_most "dashboard_pdb_policy_blockers" dashboard_pdb_policy_blockers 0
  wait_for_dashboard_recovery
}

wait_for_dashboard_recovery() {
  wait_for_dashboard_at_least "dashboard_cnpg_collectors_up" dashboard_cnpg_collectors_up "${instances}"
  wait_for_dashboard_at_least "dashboard_cnpg_ready_pods" dashboard_cnpg_ready_pods "${instances}"
  wait_for_dashboard_at_least "dashboard_cnpg_primaries" dashboard_cnpg_primaries 1
  wait_for_dashboard_at_most "dashboard_cnpg_primaries" dashboard_cnpg_primaries 1
  wait_for_dashboard_at_most "dashboard_cnpg_max_replication_lag" dashboard_cnpg_max_replication_lag "${max_replication_lag}"
  wait_for_dashboard_at_least "dashboard_cnpg_rw_endpoint_count" dashboard_cnpg_rw_endpoint_count 1
  wait_for_dashboard_at_most "dashboard_cnpg_rw_endpoint_count" dashboard_cnpg_rw_endpoint_count 1
  wait_for_dashboard_at_least "dashboard_cnpg_rw_endpoint_is_primary" dashboard_cnpg_rw_endpoint_is_primary 1
  wait_for_dashboard_at_most "dashboard_cnpg_rw_endpoint_is_primary" dashboard_cnpg_rw_endpoint_is_primary 1
  wait_for_dashboard_at_most "dashboard_cnpg_unhealthy" dashboard_cnpg_unhealthy 0
}

wait_for_node_recovery() {
  wait_for_dashboard_at_most "dashboard_node_unschedulable" dashboard_node_unschedulable 0
  wait_for_dashboard_at_most "dashboard_cordoned_workers" dashboard_cordoned_workers 0
  wait_for_dashboard_at_most "dashboard_cnpg_primary_on_cordoned_node" dashboard_cnpg_primary_on_cordoned_node 0
  wait_for_dashboard_at_most "dashboard_pdb_policy_blockers" dashboard_pdb_policy_blockers 0
}

choose_probe_pod() {
  kubectl -n "${namespace}" get pods -l "cnpg.io/cluster=${cluster},cnpg.io/podRole=instance" -o json \
    | jq -r '
        .items
        | map(select(.status.phase == "Running"))
        | map(select([.status.conditions[]? | select(.type == "Ready" and .status == "True")] | length > 0))
        | sort_by(.metadata.name)
        | .[0].metadata.name // empty
      '
}

database_rw_url() {
  local encoded
  encoded="$(kubectl -n "${namespace}" get secret "${database_rw_url_secret}" -o json \
    | jq -r --arg key "${database_rw_url_key}" '.data[$key] // empty')"
  if [[ -z "${encoded}" ]]; then
    echo "secret ${namespace}/${database_rw_url_secret} does not contain key ${database_rw_url_key}" >&2
    exit 1
  fi
  printf '%s' "${encoded}" | base64 -d
}

verify_postgres_rw_service() {
  local phase="$1"
  local probe_pod rw_url
  probe_pod="$(choose_probe_pod)"
  if [[ -z "${probe_pod}" ]]; then
    echo "could not find a Ready CNPG instance pod to run the Postgres probe" >&2
    exit 1
  fi

  rw_url="$(database_rw_url)"
  kubectl -n "${namespace}" exec "${probe_pod}" -c postgres -- env DATABASE_RW_URL="${rw_url}" PHASE="${phase}" sh -lc '
    set -e
    value="ok-${PHASE}-$(date +%s)"
    case "${value}" in
      *[!A-Za-z0-9._-]*)
        echo "unsafe Postgres probe value: ${value}" >&2
        exit 2
        ;;
    esac
    got="$(psql -X -qAt -v ON_ERROR_STOP=1 "${DATABASE_RW_URL}" -c "CREATE TEMP TABLE local_ha_probe(value text) ON COMMIT DROP; INSERT INTO local_ha_probe(value) VALUES (\$\$${value}\$\$); SELECT value FROM local_ha_probe;" | tail -n 1 | tr -d "\r")"
    if [ "${got}" != "${value}" ]; then
      echo "Postgres RW service probe failed during ${PHASE}: got ${got}, want ${value}" >&2
      exit 1
    fi
    echo "postgres_rw_probe_${PHASE}=ok probe_pod=${HOSTNAME} value=${value}"
  '
}

instances="${CNPG_INSTANCES:-$(kubectl -n "${namespace}" get cluster "${cluster}" -o jsonpath='{.spec.instances}')}"
primary_before="$(kubectl -n "${namespace}" get cluster "${cluster}" -o jsonpath='{.status.currentPrimary}')"
primary_node_before="$(kubectl -n "${namespace}" get pod "${primary_before}" -o jsonpath='{.spec.nodeName}')"
primary_phase="$(kubectl -n "${namespace}" get pod "${primary_before}" -o jsonpath='{.status.phase}')"
primary_role="$(kubectl -n "${namespace}" get pod "${primary_before}" -o json | jq -r '.metadata.labels["cnpg.io/instanceRole"] // ""')"

if [[ "${primary_phase}" != "Running" || "${primary_role}" != "primary" ]]; then
  echo "${primary_before} is not a running CNPG primary pod: phase=${primary_phase} role=${primary_role}" >&2
  exit 2
fi

printf 'target_cluster=%s\n' "${cluster}"
printf 'primary_before=%s\n' "${primary_before}"
printf 'primary_node_before=%s\n' "${primary_node_before}"

echo "==> before CNPG primary-node cordon"
kubectl get nodes -L topology.kubernetes.io/zone
kubectl -n "${namespace}" get cluster "${cluster}" -o wide
kubectl -n "${namespace}" get pods -l "cnpg.io/cluster=${cluster}" -o wide
wait_for_dashboard_baseline
verify_postgres_rw_service "before"

echo "==> cordoning CNPG primary node ${primary_node_before}"
kubectl cordon "${primary_node_before}"
node_cordoned=true

wait_for_dashboard_at_least "dashboard_node_unschedulable" dashboard_node_unschedulable 1
wait_for_primary_to_move
wait_for_dashboard_switchover
verify_postgres_rw_service "after"

printf 'dashboard_cnpg_role_changes=%s\n' "$(dashboard_cnpg_role_changes)"
printf 'dashboard_cnpg_cordon_switchover_events=%s\n' "$(dashboard_cnpg_cordon_switchover_events)"
printf 'dashboard_cnpg_primary_on_cordoned_node_seconds_10m=%s\n' "$(dashboard_cnpg_primary_on_cordoned_node_seconds_10m)"
printf 'dashboard_cordoned_worker_seconds_10m=%s\n' "$(dashboard_cordoned_worker_seconds_10m)"

echo "==> restoring ${primary_node_before}"
kubectl uncordon "${primary_node_before}"
node_cordoned=false
wait_for_node_recovery

echo "==> CNPG primary-node cordon switchover recovered"
kubectl get nodes -L topology.kubernetes.io/zone
kubectl -n "${namespace}" get cluster "${cluster}" -o wide
kubectl -n "${namespace}" get pods -l "cnpg.io/cluster=${cluster}" -o wide
