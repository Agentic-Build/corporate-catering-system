#!/usr/bin/env bash
set -euo pipefail

cd "$(dirname "$0")/../.."

namespace="${NAMESPACE:-tbite}"
cluster="${CNPG_CLUSTER:-tbite-pg}"
release="${RELEASE:-tbite}"
failure_mode="${FAILURE_MODE:-abrupt}"
database_rw_url_secret="${DATABASE_RW_URL_SECRET:-${release}-db}"
database_rw_url_key="${DATABASE_RW_URL_KEY:-rwUrl}"
timeout_seconds="${FAILOVER_TIMEOUT_SECONDS:-300}"
poll_seconds="${POLL_SECONDS:-5}"
vm_service="${VM_SERVICE:-vmsingle-${release}-victoria-metrics-k8s-stack}"
vm_url="${VM_URL:-}"
vm_local_port="${VM_LOCAL_PORT:-18428}"
max_replication_lag="${MAX_REPLICATION_LAG:-5}"
port_forward_pid=""
fault_start_epoch=""

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

cleanup() {
  if [[ -n "${port_forward_pid}" ]]; then
    kill "${port_forward_pid}" >/dev/null 2>&1 || true
  fi
}
trap cleanup EXIT

promql_value() {
  local query="$1"
  curl -fsS --get "${vm_url}/api/v1/query" --data-urlencode "query=${query}" \
    | jq -r '.data.result[0].value[1] // empty'
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

dashboard_cnpg_pod_recreations() {
  promql_value "sum(changes(kube_pod_created{namespace=\"${namespace}\",pod=~\"${cluster}-[0-9]+\"}[10m])) or vector(0)"
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

dashboard_deleted_primary_recreated_after_fault() {
  if [[ -z "${fault_start_epoch}" ]]; then
    echo 0
    return 0
  fi

  promql_value "max((kube_pod_created{namespace=\"${namespace}\",pod=\"${primary}\"} > bool ${fault_start_epoch}) or vector(0))"
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

wait_for_dashboard_baseline() {
  start_vm_port_forward
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
  wait_for_dashboard_at_least "dashboard_cnpg_role_changes" dashboard_cnpg_role_changes 1
  wait_for_dashboard_at_least "dashboard_cnpg_pod_recreations" dashboard_cnpg_pod_recreations 1
  wait_for_dashboard_at_least "dashboard_deleted_primary_recreated_after_fault" dashboard_deleted_primary_recreated_after_fault 1
}

choose_probe_pod() {
  kubectl -n "${namespace}" get pods -l "cnpg.io/cluster=${cluster},cnpg.io/podRole=instance" -o json \
    | jq -r '
        .items
        | map(select(.status.phase == "Running"))
        | map(select([.status.conditions[]? | select(.type == "Ready" and .status == "True")] | length > 0))
        | sort_by(.metadata.creationTimestamp)
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

primary="$(kubectl -n "${namespace}" get cluster "${cluster}" -o jsonpath='{.status.currentPrimary}' 2>/dev/null || true)"
if [[ -z "${primary}" ]]; then
  echo "could not read current primary from CNPG cluster ${namespace}/${cluster}" >&2
  exit 1
fi

instances="$(kubectl -n "${namespace}" get cluster "${cluster}" -o jsonpath='{.spec.instances}')"
wait_for_dashboard_baseline
verify_postgres_rw_service "baseline"
baseline_role_changes="$(dashboard_cnpg_role_changes)"
baseline_role_changes="${baseline_role_changes:-0}"
baseline_pod_recreations="$(dashboard_cnpg_pod_recreations)"
baseline_pod_recreations="${baseline_pod_recreations:-0}"
printf 'baseline_dashboard_cnpg_role_changes=%s\n' "${baseline_role_changes}"
printf 'baseline_dashboard_cnpg_pod_recreations=%s\n' "${baseline_pod_recreations}"

case "${failure_mode}" in
  abrupt)
    echo "==> abruptly deleting CNPG primary pod ${primary}"
    fault_start_epoch="$(date +%s)"
    kubectl -n "${namespace}" delete pod "${primary}" --grace-period=0 --force --wait=false
    ;;
  graceful)
    echo "==> gracefully deleting CNPG primary pod ${primary}"
    fault_start_epoch="$(date +%s)"
    kubectl -n "${namespace}" delete pod "${primary}" --wait=false
    ;;
  *)
    echo "FAILURE_MODE must be abrupt or graceful, got ${failure_mode}" >&2
    exit 2
    ;;
esac

wait_for_dashboard_at_least "dashboard_cnpg_unhealthy" dashboard_cnpg_unhealthy 1
printf 'fault_dashboard_cnpg_unhealthy=%s\n' "$(dashboard_cnpg_unhealthy)"
printf 'fault_dashboard_cnpg_collectors_up=%s\n' "$(dashboard_cnpg_collectors_up)"
printf 'fault_dashboard_cnpg_ready_pods=%s\n' "$(dashboard_cnpg_ready_pods)"
printf 'fault_dashboard_cnpg_primaries=%s\n' "$(dashboard_cnpg_primaries)"
printf 'fault_dashboard_cnpg_rw_endpoint_count=%s\n' "$(dashboard_cnpg_rw_endpoint_count)"
printf 'fault_dashboard_cnpg_rw_endpoint_is_primary=%s\n' "$(dashboard_cnpg_rw_endpoint_is_primary)"
printf 'fault_dashboard_cnpg_max_replication_lag=%s\n' "$(dashboard_cnpg_max_replication_lag)"

start_seconds="${SECONDS}"
deadline=$((SECONDS + timeout_seconds))
while true; do
  next_primary="$(kubectl -n "${namespace}" get cluster "${cluster}" -o jsonpath='{.status.currentPrimary}' 2>/dev/null || true)"
  target_primary="$(kubectl -n "${namespace}" get cluster "${cluster}" -o jsonpath='{.status.targetPrimary}' 2>/dev/null || true)"
  phase="$(kubectl -n "${namespace}" get cluster "${cluster}" -o jsonpath='{.status.phase}' 2>/dev/null || true)"
  ready_instances="$(kubectl -n "${namespace}" get cluster "${cluster}" -o jsonpath='{.status.readyInstances}' 2>/dev/null || true)"
  printf '%s current=%s target=%s phase=%q ready=%s/%s\n' \
    "$(date -u +%Y-%m-%dT%H:%M:%SZ)" "${next_primary:-unknown}" "${target_primary:-unknown}" \
    "${phase:-unknown}" "${ready_instances:-0}" "${instances}"

  if [[ -n "${next_primary}" \
      && "${next_primary}" != "${primary}" \
      && ( "${phase}" == *"healthy"* || "${phase}" == *"Healthy"* ) \
      && "${ready_instances}" == "${instances}" ]]; then
    break
  fi
  if (( SECONDS > deadline )); then
    kubectl -n "${namespace}" describe cluster "${cluster}" >&2 || true
    echo "timed out waiting for primary failover; old=${primary} new=${next_primary} phase=${phase}" >&2
    exit 1
  fi
  sleep "${poll_seconds}"
done

duration=$((SECONDS - start_seconds))
echo "primary moved from ${primary} to ${next_primary} in ${duration}s"
wait_for_dashboard_recovery
verify_postgres_rw_service "recovery"
kubectl -n "${namespace}" get pods -l "cnpg.io/cluster=${cluster}" -o wide
kubectl -n "${namespace}" get hpa "keda-hpa-tbite-tbite-platform-worker-outbox-relay" -o wide || true
