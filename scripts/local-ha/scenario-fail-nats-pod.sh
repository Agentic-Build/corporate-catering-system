#!/usr/bin/env bash
set -euo pipefail

cd "$(dirname "$0")/../.."

namespace="${NAMESPACE:-tbite}"
release="${RELEASE:-tbite}"
statefulset="${NATS_STATEFULSET:-${release}-nats}"
pod="${POD:-}"
nats_service="${NATS_SERVICE:-${release}-nats}"
nats_box_deployment="${NATS_BOX_DEPLOYMENT:-${release}-nats-box}"
timeout="${TIMEOUT:-5m}"
poll_seconds="${POLL_SECONDS:-5}"
nats_metrics_job="${NATS_METRICS_JOB:-${namespace}/${release}-tbite-platform-nats}"
vm_service="${VM_SERVICE:-vmsingle-${release}-victoria-metrics-k8s-stack}"
vm_url="${VM_URL:-}"
vm_local_port="${VM_LOCAL_PORT:-18428}"
max_meta_pending="${MAX_NATS_META_PENDING:-0}"
max_api_errors="${MAX_NATS_API_ERRORS_10M:-0}"
max_consumer_pending="${MAX_NATS_CONSUMER_PENDING:-0}"
messaging_client_regex="${MESSAGING_CLIENT_COMPONENT_REGEX:-realtime|worker-outbox-relay|worker-payroll-settler|worker-on-time-evaluator}"
app_component_regex="${APP_COMPONENT_REGEX:-api|realtime|web-employee|web-merchant|web-admin|worker-outbox-relay|worker-payroll-settler|worker-on-time-evaluator|scheduler-cutoff|scheduler-no-show|scheduler-doc-expiry|scheduler-feedback}"
crashloop_observe_seconds="${CRASHLOOP_OBSERVE_SECONDS:-45}"
min_range_signal_seconds="${MIN_RANGE_SIGNAL_SECONDS:-15}"
dashboard_file="chart/tbite-platform/dashboards/local-ha-drills.json"
port_forward_pid=""
baseline_data_messaging_degraded_seconds=0

float_ge() {
  awk -v left="$1" -v right="$2" 'BEGIN { exit !(left >= right) }'
}

float_gt() {
  awk -v left="$1" -v right="$2" 'BEGIN { exit !(left > right) }'
}

float_le() {
  awk -v left="$1" -v right="$2" 'BEGIN { exit !(left <= right) }'
}

timeout_seconds() {
  local value="$1"
  local number="$1"
  local unit="s"
  if [[ "${value}" == *[smh] ]]; then
    number="${value%?}"
    unit="${value: -1}"
  fi
  if [[ -z "${number}" || "${number}" == *[!0-9]* ]]; then
    echo "invalid TIMEOUT '${value}', expected seconds or a value ending in s, m, or h" >&2
    exit 2
  fi
  case "${unit}" in
    s) echo "${number}" ;;
    m) echo "$(( number * 60 ))" ;;
    h) echo "$(( number * 3600 ))" ;;
  esac
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

dashboard_data_database_degraded() {
  dashboard_target_value "Data service availability" "database service degraded"
}

dashboard_data_messaging_degraded() {
  dashboard_target_value "Data service availability" "messaging service degraded"
}

dashboard_data_cache_degraded() {
  dashboard_target_value "Data service availability" "cache service degraded"
}

dashboard_data_object_storage_degraded() {
  dashboard_target_value "Data service availability" "object storage service degraded"
}

dashboard_data_app_dependency_clients_degraded() {
  dashboard_target_value "Data service availability" "app dependency clients degraded"
}

dashboard_data_messaging_degraded_seconds_10m() {
  dashboard_target_value "Data service activity" "messaging service degraded seconds / 10m"
}

dashboard_nats_ready_pods() {
  promql_value "sum(kube_pod_status_ready{namespace=\"${namespace}\",pod=~\"${release}-nats-[0-9]+\",condition=\"true\"}) or vector(0)"
}

dashboard_nats_scrape_up() {
  promql_value "sum(up{namespace=\"${namespace}\",pod=~\"${release}-nats-[0-9]+\",job=\"${nats_metrics_job}\"}) or vector(0)"
}

dashboard_nats_routes() {
  promql_value "sum(nats_varz_routes{namespace=\"${namespace}\",pod=~\"${release}-nats-[0-9]+\"}) or vector(0)"
}

dashboard_nats_meta_leaders() {
  promql_value "count(count by (value) (nats_varz_jetstream_meta_leader{namespace=\"${namespace}\",pod=~\"${release}-nats-[0-9]+\"})) or vector(0)"
}

dashboard_nats_meta_cluster_min() {
  promql_value "min(nats_varz_jetstream_meta_cluster_size{namespace=\"${namespace}\",pod=~\"${release}-nats-[0-9]+\"}) or vector(0)"
}

dashboard_nats_meta_pending() {
  promql_value "max(nats_varz_jetstream_meta_pending{namespace=\"${namespace}\",pod=~\"${release}-nats-[0-9]+\"}) or vector(0)"
}

dashboard_nats_pod_recreations() {
  promql_value "sum(changes(kube_pod_created{namespace=\"${namespace}\",pod=~\"${release}-nats-[0-9]+\"}[10m])) or vector(0)"
}

dashboard_nats_server_degraded_seconds_10m() {
  promql_value "(sum_over_time(((((sum(kube_pod_status_ready{namespace=\"${namespace}\",pod=~\"${release}-nats-[0-9]+\",condition=\"true\"}) or vector(0)) < bool ${desired}) + ((sum(up{namespace=\"${namespace}\",pod=~\"${release}-nats-[0-9]+\",job=\"${nats_metrics_job}\"}) or vector(0)) < bool ${desired}) + ((count(count by (value) (nats_varz_jetstream_meta_leader{namespace=\"${namespace}\",pod=~\"${release}-nats-[0-9]+\"})) or vector(0)) != bool 1) + ((min(nats_varz_jetstream_meta_cluster_size{namespace=\"${namespace}\",pod=~\"${release}-nats-[0-9]+\"}) or vector(0)) < bool ${desired}) + ((max(nats_varz_jetstream_meta_pending{namespace=\"${namespace}\",pod=~\"${release}-nats-[0-9]+\"}) or vector(0)) > bool ${max_meta_pending})) > bool 0)[10m:15s]) or vector(0)) * 15"
}

dashboard_nats_pod_created_timestamp() {
  local target_pod="$1"
  promql_value "max(kube_pod_created{namespace=\"${namespace}\",pod=\"${target_pod}\"}) or vector(0)"
}

dashboard_nats_api_errors() {
  promql_value "sum(increase(nats_varz_jetstream_stats_api_errors{namespace=\"${namespace}\",pod=~\"${release}-nats-[0-9]+\"}[10m])) or vector(0)"
}

dashboard_nats_consumer_pending() {
  promql_value "sum(nats_consumer_num_pending{namespace=\"${namespace}\"}) or vector(0)"
}

dashboard_messaging_client_not_ready_pods() {
  promql_value "sum(kube_pod_status_ready{namespace=\"${namespace}\",pod=~\"${release}-tbite-platform-(${messaging_client_regex}).*\",condition=\"false\"} == 1) or vector(0)"
}

dashboard_messaging_client_readiness_changes() {
  promql_value "sum(changes(kube_pod_status_ready{namespace=\"${namespace}\",pod=~\"${release}-tbite-platform-(${messaging_client_regex}).*\",condition=\"true\"}[10m])) or vector(0)"
}

dashboard_nats_unhealthy() {
  promql_value "(((sum(kube_pod_status_ready{namespace=\"${namespace}\",pod=~\"${release}-nats-[0-9]+\",condition=\"true\"}) or vector(0)) < bool ${desired}) + ((sum(up{namespace=\"${namespace}\",pod=~\"${release}-nats-[0-9]+\",job=\"${nats_metrics_job}\"}) or vector(0)) < bool ${desired}) + ((count(count by (value) (nats_varz_jetstream_meta_leader{namespace=\"${namespace}\",pod=~\"${release}-nats-[0-9]+\"})) or vector(0)) != bool 1) + ((min(nats_varz_jetstream_meta_cluster_size{namespace=\"${namespace}\",pod=~\"${release}-nats-[0-9]+\"}) or vector(0)) < bool ${desired}) + ((max(nats_varz_jetstream_meta_pending{namespace=\"${namespace}\",pod=~\"${release}-nats-[0-9]+\"}) or vector(0)) > bool ${max_meta_pending}))"
}

wait_for_dashboard_at_least() {
  local name="$1"
  local metric_func="$2"
  local threshold="$3"
  local deadline=$((SECONDS + $(timeout_seconds "${timeout}")))
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
  local deadline=$((SECONDS + $(timeout_seconds "${timeout}")))
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

wait_for_dashboard_delta_at_most() {
  local name="$1"
  local metric_func="$2"
  local baseline="$3"
  local max_delta="$4"
  local deadline=$((SECONDS + $(timeout_seconds "${timeout}")))
  local current

  while (( SECONDS < deadline )); do
    current="$("${metric_func}")"
    if [[ -n "${current}" ]] && awk -v current="${current}" -v baseline="${baseline}" -v max_delta="${max_delta}" 'BEGIN { exit !((current - baseline) <= max_delta) }'; then
      printf '%s %s=%s baseline=%s max_delta=%s\n' "$(date -u +%Y-%m-%dT%H:%M:%SZ)" "${name}" "${current}" "${baseline}" "${max_delta}"
      return 0
    fi
    printf '%s %s=%s waiting_for_delta_at_most=%s baseline=%s\n' "$(date -u +%Y-%m-%dT%H:%M:%SZ)" "${name}" "${current:-empty}" "${max_delta}" "${baseline}"
    sleep "${poll_seconds}"
  done

  echo "timed out waiting for dashboard signal ${name} to stay within ${max_delta} of baseline ${baseline}" >&2
  return 1
}

wait_for_dashboard_delta_at_least() {
  local name="$1"
  local metric_func="$2"
  local baseline="$3"
  local min_delta="$4"
  local deadline=$((SECONDS + $(timeout_seconds "${timeout}")))
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
  wait_for_dashboard_at_most "dashboard_data_database_degraded" dashboard_data_database_degraded 0
  wait_for_dashboard_at_most "dashboard_data_messaging_degraded" dashboard_data_messaging_degraded 0
  wait_for_dashboard_at_most "dashboard_data_cache_degraded" dashboard_data_cache_degraded 0
  wait_for_dashboard_at_most "dashboard_data_object_storage_degraded" dashboard_data_object_storage_degraded 0
  wait_for_dashboard_at_most "dashboard_data_app_dependency_clients_degraded" dashboard_data_app_dependency_clients_degraded 0
  wait_for_dashboard_at_least "dashboard_nats_ready_pods" dashboard_nats_ready_pods "${desired}"
  wait_for_dashboard_at_least "dashboard_nats_scrape_up" dashboard_nats_scrape_up "${desired}"
  wait_for_dashboard_at_least "dashboard_nats_meta_leaders" dashboard_nats_meta_leaders 1
  wait_for_dashboard_at_most "dashboard_nats_meta_leaders" dashboard_nats_meta_leaders 1
  wait_for_dashboard_at_least "dashboard_nats_meta_cluster_min" dashboard_nats_meta_cluster_min "${desired}"
  wait_for_dashboard_at_most "dashboard_nats_meta_pending" dashboard_nats_meta_pending "${max_meta_pending}"
  wait_for_dashboard_at_most "dashboard_nats_consumer_pending" dashboard_nats_consumer_pending "${max_consumer_pending}"
  wait_for_dashboard_at_most "dashboard_nats_unhealthy" dashboard_nats_unhealthy 0
  wait_for_dashboard_at_most "dashboard_messaging_client_not_ready_pods" dashboard_messaging_client_not_ready_pods 0
  baseline_data_messaging_degraded_seconds="$(dashboard_data_messaging_degraded_seconds_10m)"
  baseline_data_messaging_degraded_seconds="${baseline_data_messaging_degraded_seconds:-0}"
  printf 'baseline_dashboard_data_messaging_degraded_seconds_10m=%s\n' "${baseline_data_messaging_degraded_seconds}"
}

wait_for_dashboard_recovery() {
  local baseline_routes="$1"
  local baseline_pod_created="$2"
  local target_pod="$3"
  local baseline_api_errors="$4"

  wait_for_dashboard_at_least "dashboard_nats_ready_pods" dashboard_nats_ready_pods "${desired}"
  wait_for_dashboard_at_least "dashboard_nats_scrape_up" dashboard_nats_scrape_up "${desired}"
  wait_for_dashboard_at_least "dashboard_nats_routes" dashboard_nats_routes "${baseline_routes}"
  wait_for_dashboard_at_least "dashboard_nats_meta_leaders" dashboard_nats_meta_leaders 1
  wait_for_dashboard_at_most "dashboard_nats_meta_leaders" dashboard_nats_meta_leaders 1
  wait_for_dashboard_at_least "dashboard_nats_meta_cluster_min" dashboard_nats_meta_cluster_min "${desired}"
  wait_for_dashboard_at_most "dashboard_nats_meta_pending" dashboard_nats_meta_pending "${max_meta_pending}"
  wait_for_dashboard_delta_at_most "dashboard_nats_api_errors" dashboard_nats_api_errors "${baseline_api_errors}" "${max_api_errors}"
  wait_for_dashboard_at_most "dashboard_nats_consumer_pending" dashboard_nats_consumer_pending "${max_consumer_pending}"
  wait_for_dashboard_at_most "dashboard_nats_unhealthy" dashboard_nats_unhealthy 0
  wait_for_dashboard_at_most "dashboard_data_database_degraded" dashboard_data_database_degraded 0
  wait_for_dashboard_at_most "dashboard_data_messaging_degraded" dashboard_data_messaging_degraded 0
  wait_for_dashboard_at_most "dashboard_data_cache_degraded" dashboard_data_cache_degraded 0
  wait_for_dashboard_at_most "dashboard_data_object_storage_degraded" dashboard_data_object_storage_degraded 0
  wait_for_dashboard_at_most "dashboard_data_app_dependency_clients_degraded" dashboard_data_app_dependency_clients_degraded 0
  wait_for_dashboard_at_most "dashboard_messaging_client_not_ready_pods" dashboard_messaging_client_not_ready_pods 0
  wait_for_dashboard_at_least "dashboard_nats_pod_recreations" dashboard_nats_pod_recreations 1
  wait_for_dashboard_at_least "dashboard_nats_server_degraded_seconds_10m" dashboard_nats_server_degraded_seconds_10m "${min_range_signal_seconds}"
  wait_for_dashboard_delta_at_least "dashboard_data_messaging_degraded_seconds_10m" dashboard_data_messaging_degraded_seconds_10m "${baseline_data_messaging_degraded_seconds}" "${min_range_signal_seconds}"
  wait_for_dashboard_target_pod_recreated "${target_pod}" "${baseline_pod_created}"
}

wait_for_dashboard_target_pod_recreated() {
  local target_pod="$1"
  local baseline_pod_created="$2"
  local deadline=$((SECONDS + $(timeout_seconds "${timeout}")))
  local current

  while (( SECONDS < deadline )); do
    current="$(dashboard_nats_pod_created_timestamp "${target_pod}")"
    if [[ -n "${current}" ]] && float_gt "${current}" "${baseline_pod_created}"; then
      printf '%s dashboard_nats_pod_created{%s}=%s threshold_gt=%s\n' \
        "$(date -u +%Y-%m-%dT%H:%M:%SZ)" "${target_pod}" "${current}" "${baseline_pod_created}"
      return 0
    fi
    printf '%s dashboard_nats_pod_created{%s}=%s waiting_for_greater_than=%s\n' \
      "$(date -u +%Y-%m-%dT%H:%M:%SZ)" "${target_pod}" "${current:-empty}" "${baseline_pod_created}"
    sleep "${poll_seconds}"
  done

  echo "timed out waiting for dashboard signal kube_pod_created for ${target_pod} to change" >&2
  return 1
}

app_crashloops() {
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
}

fail_if_app_crashloops() {
  local crashloops
  crashloops="$(app_crashloops)"
  if [[ -n "${crashloops}" ]]; then
    echo "platform app pods entered CrashLoopBackOff during NATS pod loss:" >&2
    printf 'pod\tcontainers\n' >&2
    printf '%s\n' "${crashloops}" >&2
    exit 1
  fi
}

assert_no_app_crashloops() {
  local deadline
  deadline=$((SECONDS + crashloop_observe_seconds))
  echo "==> watching platform app pods for CrashLoopBackOff for ${crashloop_observe_seconds}s"
  while (( SECONDS < deadline )); do
    fail_if_app_crashloops
    sleep "${poll_seconds}"
  done
}

verify_nats_core_api() {
  local phase="$1"

  kubectl -n "${namespace}" exec "deploy/${nats_box_deployment}" -- sh -lc '
    set -e
    phase="$1"
    server="$2"
    subject="local-ha.nats-pod-loss.${phase}.$(date +%s).$$"
    payload="ok-${subject}"
    out="/tmp/local-ha-nats-probe.$$.out"
    err="/tmp/local-ha-nats-probe.$$.err"
    pub="/tmp/local-ha-nats-probe.$$.pub"
    cleanup() {
      rm -f "${out}" "${err}" "${pub}"
    }
    trap cleanup EXIT

    nats --server "${server}" subscribe "${subject}" --count 1 --raw --wait 5s >"${out}" 2>"${err}" &
    sub_pid="$!"
    sleep 1
    nats --server "${server}" publish "${subject}" "${payload}" >"${pub}" 2>&1
    wait "${sub_pid}"

    got="$(cat "${out}" | tr -d "\r")"
    if [ "${got}" != "${payload}" ]; then
      echo "NATS core pub/sub probe failed during ${phase}: got ${got}, want ${payload}" >&2
      echo "subscriber stderr:" >&2
      cat "${err}" >&2 || true
      echo "publisher output:" >&2
      cat "${pub}" >&2 || true
      exit 1
    fi
    echo "nats_core_pubsub_${phase}=ok subject=${subject}"
  ' sh "${phase}" "nats://${nats_service}:4222"
}

desired="$(kubectl -n "${namespace}" get statefulset "${statefulset}" -o jsonpath='{.spec.replicas}')"
if [[ -z "${desired}" || "${desired}" == *[!0-9]* || "${desired}" == "0" ]]; then
  echo "statefulset ${namespace}/${statefulset} has invalid desired replicas: ${desired:-empty}" >&2
  exit 1
fi

wait_for_dashboard_baseline
verify_nats_core_api "baseline"

if [[ -z "${pod}" ]]; then
  pod="$(kubectl -n "${namespace}" get pods -l app.kubernetes.io/name=nats,app.kubernetes.io/component=nats -o jsonpath='{.items[0].metadata.name}')"
fi
if [[ -z "${pod}" ]]; then
  echo "could not find a NATS pod in namespace ${namespace}" >&2
  exit 1
fi

baseline_recreations="$(dashboard_nats_pod_recreations)"
baseline_recreations="${baseline_recreations:-0}"
baseline_degraded_seconds="$(dashboard_nats_server_degraded_seconds_10m)"
baseline_degraded_seconds="${baseline_degraded_seconds:-0}"
baseline_api_errors="$(dashboard_nats_api_errors)"
baseline_api_errors="${baseline_api_errors:-0}"
baseline_routes="$(dashboard_nats_routes)"
baseline_routes="${baseline_routes:-0}"
baseline_pod_created="$(dashboard_nats_pod_created_timestamp "${pod}")"
baseline_pod_created="${baseline_pod_created:-0}"
baseline_client_readiness_changes="$(dashboard_messaging_client_readiness_changes)"
baseline_client_readiness_changes="${baseline_client_readiness_changes:-0}"
printf 'baseline_dashboard_nats_pod_recreations=%s\n' "${baseline_recreations}"
printf 'baseline_dashboard_nats_server_degraded_seconds_10m=%s\n' "${baseline_degraded_seconds}"
printf 'baseline_dashboard_nats_api_errors_10m=%s\n' "${baseline_api_errors}"
printf 'baseline_dashboard_nats_routes=%s\n' "${baseline_routes}"
printf 'baseline_dashboard_nats_pod_created{%s}=%s\n' "${pod}" "${baseline_pod_created}"
printf 'baseline_dashboard_messaging_client_readiness_changes=%s\n' "${baseline_client_readiness_changes}"

old_uid="$(kubectl -n "${namespace}" get pod "${pod}" -o jsonpath='{.metadata.uid}')"

echo "==> deleting NATS pod ${pod}"
kubectl -n "${namespace}" delete pod "${pod}" --wait=false

deadline=$((SECONDS + $(timeout_seconds "${timeout}")))
while true; do
  new_uid="$(kubectl -n "${namespace}" get pod "${pod}" -o jsonpath='{.metadata.uid}' 2>/dev/null || true)"
  ready="$(kubectl -n "${namespace}" get pod "${pod}" -o jsonpath='{.status.conditions[?(@.type=="Ready")].status}' 2>/dev/null || true)"
  phase="$(kubectl -n "${namespace}" get pod "${pod}" -o jsonpath='{.status.phase}' 2>/dev/null || true)"
  printf '%s pod=%s phase=%s ready=%s uid_changed=%s\n' \
    "$(date -u +%Y-%m-%dT%H:%M:%SZ)" "${pod}" "${phase:-missing}" "${ready:-unknown}" \
    "$([[ -n "${new_uid}" && "${new_uid}" != "${old_uid}" ]] && echo true || echo false)"
  fail_if_app_crashloops
  if [[ -n "${new_uid}" && "${new_uid}" != "${old_uid}" && "${ready}" == "True" ]]; then
    break
  fi
  if (( SECONDS > deadline )); then
    kubectl -n "${namespace}" describe pod "${pod}" >&2 || true
    echo "timed out waiting for replacement NATS pod ${pod}" >&2
    exit 1
  fi
  sleep "${poll_seconds}"
done

kubectl -n "${namespace}" rollout status "statefulset/${statefulset}" --timeout="${timeout}"
wait_for_dashboard_recovery "${baseline_routes}" "${baseline_pod_created}" "${pod}" "${baseline_api_errors}"
printf 'recovery_dashboard_nats_server_degraded_seconds_10m=%s\n' "$(dashboard_nats_server_degraded_seconds_10m)"
printf 'recovery_dashboard_data_messaging_degraded_seconds_10m=%s\n' "$(dashboard_data_messaging_degraded_seconds_10m)"
printf 'recovery_dashboard_nats_api_errors_10m=%s\n' "$(dashboard_nats_api_errors)"
printf 'recovery_dashboard_messaging_client_readiness_changes=%s\n' "$(dashboard_messaging_client_readiness_changes)"
assert_no_app_crashloops
verify_nats_core_api "recovery"
kubectl -n "${namespace}" get pods -l app.kubernetes.io/name=nats,app.kubernetes.io/component=nats -o wide
