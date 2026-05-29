#!/usr/bin/env bash
set -euo pipefail

cd "$(dirname "$0")/../.."

namespace="${NAMESPACE:-tbite}"
release="${RELEASE:-tbite}"
backlog_rows="${BACKLOG_ROWS:-3000}"
scale_timeout_seconds="${SCALE_TIMEOUT_SECONDS:-180}"
drain_timeout_seconds="${DRAIN_TIMEOUT_SECONDS:-300}"
rollout_timeout="${ROLLOUT_TIMEOUT:-5m}"
baseline_timeout_seconds="${BASELINE_TIMEOUT_SECONDS:-360}"
poll_seconds="${POLL_SECONDS:-5}"
drain_target="${DRAIN_TARGET:-0}"
cleanup_published="${CLEANUP_PUBLISHED:-true}"
min_range_signal_seconds="${MIN_RANGE_SIGNAL_SECONDS:-15}"
hpa="${HPA:-keda-hpa-${release}-tbite-platform-worker-outbox-relay}"
deployment="${DEPLOYMENT:-${release}-tbite-platform-worker-outbox-relay}"
worker_selector="app.kubernetes.io/component=worker-outbox-relay"
app_component_regex="api|realtime|web-employee|web-merchant|web-admin|worker-outbox-relay|worker-payroll-settler|worker-on-time-evaluator|scheduler-cutoff|scheduler-no-show|scheduler-doc-expiry|scheduler-feedback"
synthetic_stream="LOCAL_HA_KEDA"
synthetic_subject="local-ha-keda.event"
scheduling_blocked=false
vm_service="${VM_SERVICE:-vmsingle-${release}-victoria-metrics-k8s-stack}"
vm_url="${VM_URL:-}"
vm_local_port="${VM_LOCAL_PORT:-18428}"
port_forward_pid=""
dashboard_file="chart/tbite-platform/dashboards/local-ha-drills.json"

float_ge() {
  awk -v left="$1" -v right="$2" 'BEGIN { exit !(left >= right) }'
}

float_gt() {
  awk -v left="$1" -v right="$2" 'BEGIN { exit !(left > right) }'
}

float_le() {
  awk -v left="$1" -v right="$2" 'BEGIN { exit !(left <= right) }'
}

require_integer() {
  local name="$1"
  local value="$2"
  if [[ -z "${value}" || "${value}" == *[!0-9]* ]]; then
    echo "${name} must be a non-negative integer, got '${value}'" >&2
    exit 2
  fi
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

dashboard_async_work_backlog() {
  dashboard_target_value "Async backlog" "async work backlog"
}

dashboard_async_outbox_backlog_above_target() {
  dashboard_target_value "Async backlog" "outbox backlog above target"
}

dashboard_async_relay_autoscale_above_min() {
  dashboard_target_value "Async backlog" "outbox relay autoscale above min"
}

dashboard_async_relay_blockers() {
  dashboard_target_value "Async backlog" "outbox relay blockers"
}

dashboard_async_dlq_pending() {
  dashboard_target_value "Async backlog" "dlq pending"
}

dashboard_hpa_current() {
  promql_value "sum(kube_horizontalpodautoscaler_status_current_replicas{namespace=\"${namespace}\",horizontalpodautoscaler=\"${hpa}\"}) or vector(0)"
}

dashboard_hpa_desired() {
  promql_value "sum(kube_horizontalpodautoscaler_status_desired_replicas{namespace=\"${namespace}\",horizontalpodautoscaler=\"${hpa}\"}) or vector(0)"
}

dashboard_hpa_min() {
  promql_value "sum(kube_horizontalpodautoscaler_spec_min_replicas{namespace=\"${namespace}\",horizontalpodautoscaler=\"${hpa}\"}) or vector(0)"
}

dashboard_hpa_average_backlog() {
  promql_value "sum(kube_horizontalpodautoscaler_status_target_metric{namespace=\"${namespace}\",horizontalpodautoscaler=\"${hpa}\"}) or vector(0)"
}

dashboard_hpa_estimated_backlog() {
  promql_value "((sum(kube_horizontalpodautoscaler_status_target_metric{namespace=\"${namespace}\",horizontalpodautoscaler=\"${hpa}\"}) or vector(0)) * (sum(kube_horizontalpodautoscaler_status_current_replicas{namespace=\"${namespace}\",horizontalpodautoscaler=\"${hpa}\"}) or vector(0))) or vector(0)"
}

dashboard_hpa_target_backlog() {
  promql_value "sum(kube_horizontalpodautoscaler_spec_target_metric{namespace=\"${namespace}\",horizontalpodautoscaler=\"${hpa}\"}) or vector(0)"
}

dashboard_hpa_scale_pressure() {
  promql_value "clamp_min((sum(kube_horizontalpodautoscaler_status_desired_replicas{namespace=\"${namespace}\",horizontalpodautoscaler=\"${hpa}\"}) or vector(0)) - (sum(kube_horizontalpodautoscaler_spec_min_replicas{namespace=\"${namespace}\",horizontalpodautoscaler=\"${hpa}\"}) or vector(0)), 0)"
}

dashboard_hpa_current_over_min() {
  promql_value "clamp_min((sum(kube_horizontalpodautoscaler_status_current_replicas{namespace=\"${namespace}\",horizontalpodautoscaler=\"${hpa}\"}) or vector(0)) - (sum(kube_horizontalpodautoscaler_spec_min_replicas{namespace=\"${namespace}\",horizontalpodautoscaler=\"${hpa}\"}) or vector(0)), 0)"
}

dashboard_bad_scale_conditions() {
  promql_value "sum(kube_horizontalpodautoscaler_status_condition{namespace=\"${namespace}\",horizontalpodautoscaler=~\"(keda-hpa-)?${release}-tbite-platform-.*\",condition=~\"AbleToScale|ScalingActive\",status!=\"true\"} == 1) or vector(0)"
}

dashboard_keda_hpas_inactive() {
  promql_value "sum(kube_horizontalpodautoscaler_status_condition{namespace=\"${namespace}\",horizontalpodautoscaler=~\"keda-hpa-${release}-tbite-platform-.*\",condition=\"ScalingActive\",status=\"false\"} == 1) or vector(0)"
}

dashboard_worker_pending_pods() {
  promql_value "sum(kube_pod_status_phase{namespace=\"${namespace}\",pod=~\"${deployment}-.*\",phase=\"Pending\"}) or vector(0)"
}

dashboard_async_outbox_relay_pending_pods() {
  promql_value "sum(kube_pod_status_phase{namespace=\"${namespace}\",pod=~\"${release}-tbite-platform-worker-outbox-relay-.*\",phase=\"Pending\"} == 1) or vector(0)"
}

dashboard_outbox_scale_event_seconds_10m() {
  promql_value "(sum_over_time((((clamp_min((sum(kube_horizontalpodautoscaler_status_desired_replicas{namespace=\"${namespace}\",horizontalpodautoscaler=\"${hpa}\"}) or vector(0)) - (sum(kube_horizontalpodautoscaler_spec_min_replicas{namespace=\"${namespace}\",horizontalpodautoscaler=\"${hpa}\"}) or vector(0)), 0) > bool 0) + (clamp_min((sum(kube_horizontalpodautoscaler_status_current_replicas{namespace=\"${namespace}\",horizontalpodautoscaler=\"${hpa}\"}) or vector(0)) - (sum(kube_horizontalpodautoscaler_spec_min_replicas{namespace=\"${namespace}\",horizontalpodautoscaler=\"${hpa}\"}) or vector(0)), 0) > bool 0) + ((sum(kube_pod_status_phase{namespace=\"${namespace}\",pod=~\"${deployment}-.*\",phase=\"Pending\"}) or vector(0)) > bool 0)) > bool 0)[10m:15s]) or vector(0)) * 15"
}

dashboard_worker_unschedulable_pods() {
  promql_value "sum(kube_pod_status_unschedulable{namespace=\"${namespace}\",pod=~\"${deployment}-.*\"}) or vector(0)"
}

dashboard_worker_ready_pods() {
  promql_value "sum(kube_pod_status_ready{namespace=\"${namespace}\",pod=~\"${deployment}-.*\",condition=\"true\"}) or vector(0)"
}

dashboard_worker_unavailable_replicas() {
  promql_value "sum(kube_deployment_status_replicas_unavailable{namespace=\"${namespace}\",deployment=\"${deployment}\"}) or vector(0)"
}

dashboard_zone_coverage_gaps() {
  promql_value "$(cat <<QUERY
count((count by (label_app_kubernetes_io_component) (count by (label_app_kubernetes_io_component, label_topology_kubernetes_io_zone) (kube_pod_status_phase{namespace="${namespace}",phase="Running"} * on(namespace,pod) group_left(node) kube_pod_info{namespace="${namespace}"} * on(node) group_left(label_topology_kubernetes_io_zone) kube_node_labels{label_topology_kubernetes_io_zone!=""} * on(namespace,pod) group_left(label_app_kubernetes_io_component) kube_pod_labels{namespace="${namespace}",label_app_kubernetes_io_instance="${release}",label_app_kubernetes_io_name="tbite-platform",label_app_kubernetes_io_component=~"${app_component_regex}"}))) < on(label_app_kubernetes_io_component) (clamp_max(max by (label_app_kubernetes_io_component) (label_replace(kube_deployment_spec_replicas{namespace="${namespace}",deployment=~"${release}-tbite-platform-.*"}, "label_app_kubernetes_io_component", "\$1", "deployment", "${release}-tbite-platform-(.*)")), 3))) or vector(0)
QUERY
)"
}

dashboard_app_outbox_pending() {
  promql_value "max(tbite_outbox_pending{k8s_namespace_name=\"${namespace}\"} * on(k8s_pod_name) group_left() label_replace(kube_pod_status_phase{namespace=\"${namespace}\",phase=\"Running\"} == 1, \"k8s_pod_name\", \"\$1\", \"pod\", \"(.*)\")) or vector(0)"
}

wait_for_dashboard_at_least() {
  local name="$1"
  local metric_func="$2"
  local threshold="$3"
  local timeout="$4"
  local deadline=$((SECONDS + timeout))
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

wait_for_dashboard_greater_than() {
  local name="$1"
  local metric_func="$2"
  local threshold="$3"
  local timeout="$4"
  local deadline=$((SECONDS + timeout))
  local current

  while (( SECONDS < deadline )); do
    current="$("${metric_func}")"
    if [[ -n "${current}" ]] && float_gt "${current}" "${threshold}"; then
      printf '%s %s=%s threshold_gt=%s\n' "$(date -u +%Y-%m-%dT%H:%M:%SZ)" "${name}" "${current}" "${threshold}"
      return 0
    fi
    printf '%s %s=%s waiting_for_greater_than=%s\n' "$(date -u +%Y-%m-%dT%H:%M:%SZ)" "${name}" "${current:-empty}" "${threshold}"
    sleep "${poll_seconds}"
  done

  echo "timed out waiting for dashboard signal ${name} > ${threshold}" >&2
  return 1
}

wait_for_dashboard_at_most() {
  local name="$1"
  local metric_func="$2"
  local threshold="$3"
  local timeout="$4"
  local deadline=$((SECONDS + timeout))
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

wait_for_dashboard_baseline() {
  start_vm_port_forward
  wait_for_dashboard_at_most "dashboard_async_work_backlog" dashboard_async_work_backlog 0 "${baseline_timeout_seconds}"
  wait_for_dashboard_at_most "dashboard_async_outbox_backlog_above_target" dashboard_async_outbox_backlog_above_target 0 "${baseline_timeout_seconds}"
  wait_for_dashboard_at_most "dashboard_async_relay_autoscale_above_min" dashboard_async_relay_autoscale_above_min 0 "${baseline_timeout_seconds}"
  wait_for_dashboard_at_most "dashboard_async_relay_blockers" dashboard_async_relay_blockers 0 "${baseline_timeout_seconds}"
  wait_for_dashboard_at_most "dashboard_async_dlq_pending" dashboard_async_dlq_pending 0 "${baseline_timeout_seconds}"
  wait_for_dashboard_at_most "dashboard_hpa_scale_pressure" dashboard_hpa_scale_pressure 0 "${baseline_timeout_seconds}"
  wait_for_dashboard_at_most "dashboard_hpa_current_over_min" dashboard_hpa_current_over_min 0 "${baseline_timeout_seconds}"
  wait_for_dashboard_at_most "dashboard_worker_pending_pods" dashboard_worker_pending_pods 0 "${baseline_timeout_seconds}"
  wait_for_dashboard_at_most "dashboard_async_outbox_relay_pending_pods" dashboard_async_outbox_relay_pending_pods 0 "${baseline_timeout_seconds}"
  wait_for_dashboard_at_most "dashboard_worker_unschedulable_pods" dashboard_worker_unschedulable_pods 0 "${baseline_timeout_seconds}"
  wait_for_dashboard_at_most "dashboard_worker_unavailable_replicas" dashboard_worker_unavailable_replicas 0 "${baseline_timeout_seconds}"
  wait_for_dashboard_at_most "dashboard_app_outbox_pending" dashboard_app_outbox_pending 0 "${baseline_timeout_seconds}"
  wait_for_dashboard_at_most "dashboard_hpa_estimated_backlog" dashboard_hpa_estimated_backlog 0 "${baseline_timeout_seconds}"
  wait_for_dashboard_at_most "dashboard_zone_coverage_gaps" dashboard_zone_coverage_gaps 0 "${baseline_timeout_seconds}"
  wait_for_dashboard_at_most "dashboard_keda_hpas_inactive" dashboard_keda_hpas_inactive 0 "${baseline_timeout_seconds}"
  wait_for_dashboard_at_most "dashboard_bad_scale_conditions" dashboard_bad_scale_conditions 0 "${baseline_timeout_seconds}"
}

wait_for_dashboard_scale_up() {
  start_vm_port_forward
  wait_for_dashboard_greater_than "dashboard_async_work_backlog" dashboard_async_work_backlog 0 "${scale_timeout_seconds}"
  wait_for_dashboard_greater_than "dashboard_async_outbox_backlog_above_target" dashboard_async_outbox_backlog_above_target 0 "${scale_timeout_seconds}"
  wait_for_dashboard_greater_than "dashboard_async_relay_autoscale_above_min" dashboard_async_relay_autoscale_above_min 0 "${scale_timeout_seconds}"
  wait_for_dashboard_greater_than "dashboard_async_relay_blockers" dashboard_async_relay_blockers 0 "${scale_timeout_seconds}"
  wait_for_dashboard_at_most "dashboard_async_dlq_pending" dashboard_async_dlq_pending 0 "${scale_timeout_seconds}"
  wait_for_dashboard_greater_than "dashboard_hpa_scale_pressure" dashboard_hpa_scale_pressure 0 "${scale_timeout_seconds}"
  wait_for_dashboard_greater_than "dashboard_hpa_average_backlog" dashboard_hpa_average_backlog "$(dashboard_hpa_target_backlog)" "${scale_timeout_seconds}"
  wait_for_dashboard_greater_than "dashboard_hpa_estimated_backlog" dashboard_hpa_estimated_backlog 0 "${scale_timeout_seconds}"
  wait_for_dashboard_greater_than "dashboard_worker_pending_pods" dashboard_worker_pending_pods 0 "${scale_timeout_seconds}"
  wait_for_dashboard_greater_than "dashboard_async_outbox_relay_pending_pods" dashboard_async_outbox_relay_pending_pods 0 "${scale_timeout_seconds}"
  wait_for_dashboard_at_least "dashboard_outbox_scale_event_seconds_10m" dashboard_outbox_scale_event_seconds_10m "${min_range_signal_seconds}" "${scale_timeout_seconds}"
  wait_for_dashboard_at_most "dashboard_keda_hpas_inactive" dashboard_keda_hpas_inactive 0 "${scale_timeout_seconds}"
  wait_for_dashboard_at_most "dashboard_bad_scale_conditions" dashboard_bad_scale_conditions 0 "${scale_timeout_seconds}"
}

wait_for_dashboard_recovery() {
  start_vm_port_forward
  wait_for_dashboard_at_most "dashboard_async_work_backlog" dashboard_async_work_backlog "${drain_target}" "${drain_timeout_seconds}"
  wait_for_dashboard_at_most "dashboard_async_outbox_backlog_above_target" dashboard_async_outbox_backlog_above_target 0 "${drain_timeout_seconds}"
  wait_for_dashboard_at_most "dashboard_async_relay_blockers" dashboard_async_relay_blockers 0 "${drain_timeout_seconds}"
  wait_for_dashboard_at_most "dashboard_async_dlq_pending" dashboard_async_dlq_pending 0 "${drain_timeout_seconds}"
  wait_for_dashboard_at_most "dashboard_app_outbox_pending" dashboard_app_outbox_pending "${drain_target}" "${drain_timeout_seconds}"
  wait_for_dashboard_at_most "dashboard_hpa_estimated_backlog" dashboard_hpa_estimated_backlog "${drain_target}" "${drain_timeout_seconds}"
  wait_for_dashboard_at_most "dashboard_worker_pending_pods" dashboard_worker_pending_pods 0 "${drain_timeout_seconds}"
  wait_for_dashboard_at_most "dashboard_async_outbox_relay_pending_pods" dashboard_async_outbox_relay_pending_pods 0 "${drain_timeout_seconds}"
  wait_for_dashboard_at_most "dashboard_worker_unschedulable_pods" dashboard_worker_unschedulable_pods 0 "${drain_timeout_seconds}"
  wait_for_dashboard_at_most "dashboard_worker_unavailable_replicas" dashboard_worker_unavailable_replicas 0 "${drain_timeout_seconds}"
  wait_for_dashboard_at_most "dashboard_keda_hpas_inactive" dashboard_keda_hpas_inactive 0 "${drain_timeout_seconds}"
  wait_for_dashboard_at_most "dashboard_bad_scale_conditions" dashboard_bad_scale_conditions 0 "${drain_timeout_seconds}"
}

wait_for_dashboard_floor() {
  start_vm_port_forward
  wait_for_dashboard_at_most "dashboard_async_relay_autoscale_above_min" dashboard_async_relay_autoscale_above_min 0 "${baseline_timeout_seconds}"
  wait_for_dashboard_at_most "dashboard_hpa_scale_pressure" dashboard_hpa_scale_pressure 0 "${baseline_timeout_seconds}"
  wait_for_dashboard_at_most "dashboard_hpa_current_over_min" dashboard_hpa_current_over_min 0 "${baseline_timeout_seconds}"
  wait_for_dashboard_at_most "dashboard_hpa_desired" dashboard_hpa_desired "${min_replicas}" "${baseline_timeout_seconds}"
  wait_for_dashboard_at_most "dashboard_hpa_current" dashboard_hpa_current "${min_replicas}" "${baseline_timeout_seconds}"
  wait_for_dashboard_at_most "dashboard_hpa_estimated_backlog" dashboard_hpa_estimated_backlog 0 "${baseline_timeout_seconds}"
  wait_for_dashboard_at_least "dashboard_worker_ready_pods" dashboard_worker_ready_pods "${min_replicas}" "${baseline_timeout_seconds}"
  wait_for_dashboard_at_most "dashboard_keda_hpas_inactive" dashboard_keda_hpas_inactive 0 "${baseline_timeout_seconds}"
  wait_for_dashboard_at_most "dashboard_bad_scale_conditions" dashboard_bad_scale_conditions 0 "${baseline_timeout_seconds}"
}

restore_worker_zone_coverage() {
  local gaps
  start_vm_port_forward
  gaps="$(dashboard_zone_coverage_gaps)"
  if [[ -n "${gaps}" ]] && float_gt "${gaps}" 0; then
    echo "==> restoring worker-outbox-relay zone coverage; dashboard_zone_coverage_gaps=${gaps}"
    NAMESPACE="${namespace}" \
      DEPLOYMENT_SELECTOR="app.kubernetes.io/instance=${release},app.kubernetes.io/name=tbite-platform,app.kubernetes.io/component=worker-outbox-relay" \
      scripts/local-ha/rebalance-apps.sh
  fi
  wait_for_dashboard_at_most "dashboard_zone_coverage_gaps" dashboard_zone_coverage_gaps 0 "${baseline_timeout_seconds}"
}

primary_pod() {
  kubectl -n "${namespace}" get cluster tbite-pg -o jsonpath='{.status.currentPrimary}'
}

psql_scalar() {
  local sql="$1"
  local primary
  primary="$(primary_pod)"
  if [[ -z "${primary}" ]]; then
    echo "CNPG cluster tbite-pg does not report a current primary." >&2
    exit 1
  fi
  kubectl -n "${namespace}" exec -i "${primary}" -c postgres -- \
    psql -q -d tbite -At -v ON_ERROR_STOP=1 <<<"${sql}"
}

pending_backlog() {
  psql_scalar "SELECT count(*) FROM outbox_event WHERE published_at IS NULL;"
}

synthetic_pending_backlog() {
  psql_scalar "SELECT count(*) FROM outbox_event WHERE aggregate_type = 'local-ha-keda' AND published_at IS NULL;"
}

nats_cli() {
  kubectl -n "${namespace}" exec deploy/tbite-nats-box -- \
    nats --server nats://tbite-nats:4222 "$@"
}

ensure_synthetic_stream() {
  local pending
  pending="$(synthetic_pending_backlog)"
  if (( pending > 0 )); then
    echo "found ${pending} unpublished local-ha-keda outbox rows from a previous run; let them drain or inspect before rerunning" >&2
    exit 1
  fi
  nats_cli stream rm "${synthetic_stream}" --force >/dev/null 2>&1 || true
  nats_cli stream add "${synthetic_stream}" \
    --subjects 'local-ha-keda.>' \
    --storage file \
    --retention limits \
    --discard old \
    --max-age 1h \
    --replicas 1 \
    --defaults >/dev/null
}

cleanup_synthetic_rows() {
  if [[ "${cleanup_published}" != "true" ]]; then
    return
  fi
  psql_scalar "DELETE FROM outbox_event WHERE aggregate_type = 'local-ha-keda' AND published_at IS NOT NULL;" >/dev/null
}

delete_synthetic_stream() {
  nats_cli stream rm "${synthetic_stream}" --force >/dev/null 2>&1 || true
}

wait_for_hpa_floor() {
  local deadline current desired
  deadline=$((SECONDS + baseline_timeout_seconds))
  while true; do
    current="$(kubectl -n "${namespace}" get hpa "${hpa}" -o jsonpath='{.status.currentReplicas}' 2>/dev/null || echo 0)"
    desired="$(kubectl -n "${namespace}" get hpa "${hpa}" -o jsonpath='{.status.desiredReplicas}' 2>/dev/null || echo 0)"
    if (( current <= min_replicas && desired <= min_replicas )); then
      break
    fi
    print_sample
    if (( SECONDS > deadline )); then
      echo "timed out waiting for ${hpa} to return to minReplicas=${min_replicas}" >&2
      exit 1
    fi
    sleep "${poll_seconds}"
  done
}

block_worker_scheduling() {
  kubectl -n "${namespace}" patch deployment "${deployment}" --type=merge \
    -p '{"spec":{"template":{"spec":{"nodeSelector":{"local-ha.keda-blocked":"true"}}}}}' >/dev/null
  scheduling_blocked=true
  kubectl -n "${namespace}" scale "deployment/${deployment}" --replicas=0 >/dev/null
}

restore_worker_scheduling() {
  if [[ "${scheduling_blocked}" != "true" ]]; then
    return
  fi
  kubectl -n "${namespace}" patch deployment "${deployment}" --type=json \
    -p '[{"op":"remove","path":"/spec/template/spec/nodeSelector/local-ha.keda-blocked"}]' >/dev/null 2>&1 || true
  scheduling_blocked=false
}

cleanup_on_exit() {
  restore_worker_scheduling
  if [[ -n "${port_forward_pid}" ]]; then
    kill "${port_forward_pid}" >/dev/null 2>&1 || true
  fi
}
trap cleanup_on_exit EXIT

print_sample() {
  local pending current desired target active
  pending="$(pending_backlog)"
  current="$(kubectl -n "${namespace}" get hpa "${hpa}" -o jsonpath='{.status.currentReplicas}' 2>/dev/null || true)"
  desired="$(kubectl -n "${namespace}" get hpa "${hpa}" -o jsonpath='{.status.desiredReplicas}' 2>/dev/null || true)"
  target="$(kubectl -n "${namespace}" get hpa "${hpa}" -o jsonpath='{.status.currentMetrics[0].external.current.averageValue}' 2>/dev/null || true)"
  active="$(kubectl -n "${namespace}" get scaledobject tbite-tbite-platform-worker-outbox-relay -o jsonpath='{.status.conditions[?(@.type=="Active")].status}' 2>/dev/null || true)"
  printf '%s pending=%s hpa_current=%s hpa_desired=%s hpa_average=%s scaledobject_active=%s\n' \
    "$(date -u +%Y-%m-%dT%H:%M:%SZ)" "${pending}" "${current:-unknown}" "${desired:-unknown}" "${target:-unknown}" "${active:-unknown}"
}

require_integer BACKLOG_ROWS "${backlog_rows}"
require_integer SCALE_TIMEOUT_SECONDS "${scale_timeout_seconds}"
require_integer DRAIN_TIMEOUT_SECONDS "${drain_timeout_seconds}"
require_integer BASELINE_TIMEOUT_SECONDS "${baseline_timeout_seconds}"
require_integer POLL_SECONDS "${poll_seconds}"
require_integer DRAIN_TARGET "${drain_target}"
require_integer MIN_RANGE_SIGNAL_SECONDS "${min_range_signal_seconds}"
if (( backlog_rows < 1 )); then
  echo "BACKLOG_ROWS must be at least 1" >&2
  exit 2
fi

min_replicas="$(kubectl -n "${namespace}" get hpa "${hpa}" -o jsonpath='{.spec.minReplicas}')"
require_integer minReplicas "${min_replicas}"
wait_for_hpa_floor
wait_for_dashboard_baseline

echo "==> baseline"
kubectl -n "${namespace}" get hpa "${hpa}"
kubectl -n "${namespace}" get pods -l "${worker_selector}" -o wide
print_sample

echo "==> creating temporary JetStream stream ${synthetic_stream}"
ensure_synthetic_stream

echo "==> blocking worker scheduling so backlog persists across a KEDA polling interval"
block_worker_scheduling

echo "==> injecting ${backlog_rows} publishable outbox rows"
psql_scalar "
INSERT INTO outbox_event (aggregate_type, aggregate_id, subject, payload, headers, created_at)
SELECT
  'local-ha-keda',
  gen_random_uuid(),
  '${synthetic_subject}',
  jsonb_build_object('scenario', 'local-ha-keda', 'ordinal', n),
  '{}'::jsonb,
  now()
FROM generate_series(1, ${backlog_rows}) AS n;
SELECT count(*) FROM outbox_event WHERE published_at IS NULL;
"

echo "==> waiting for outbox-relay HPA to scale above min=${min_replicas}"
scaled=false
scale_deadline=$((SECONDS + scale_timeout_seconds))
while true; do
  print_sample
  current="$(kubectl -n "${namespace}" get hpa "${hpa}" -o jsonpath='{.status.currentReplicas}' 2>/dev/null || echo 0)"
  desired="$(kubectl -n "${namespace}" get hpa "${hpa}" -o jsonpath='{.status.desiredReplicas}' 2>/dev/null || echo 0)"
  if (( desired > min_replicas )); then
    scaled=true
    break
  fi
  if (( SECONDS > scale_deadline )); then
    kubectl -n "${namespace}" describe hpa "${hpa}" >&2 || true
    kubectl -n "${namespace}" describe scaledobject tbite-tbite-platform-worker-outbox-relay >&2 || true
    echo "timed out waiting for ${hpa} to scale above min=${min_replicas}" >&2
    exit 1
  fi
  sleep "${poll_seconds}"
done

kubectl -n "${namespace}" get hpa "${hpa}"
kubectl -n "${namespace}" get pods -l "${worker_selector}" -o wide
wait_for_dashboard_scale_up

echo "==> restoring worker scheduling"
restore_worker_scheduling
kubectl -n "${namespace}" rollout status "deployment/${deployment}" --timeout="${rollout_timeout}"

echo "==> waiting for backlog to drain to <= ${drain_target}"
drain_deadline=$((SECONDS + drain_timeout_seconds))
while true; do
  print_sample
  pending="$(pending_backlog)"
  if (( pending <= drain_target )); then
    break
  fi
  if (( SECONDS > drain_deadline )); then
    kubectl -n "${namespace}" describe hpa "${hpa}" >&2 || true
    echo "timed out waiting for outbox backlog to drain to <= ${drain_target}" >&2
    exit 1
  fi
  sleep "${poll_seconds}"
done

cleanup_synthetic_rows
delete_synthetic_stream
wait_for_dashboard_recovery
wait_for_hpa_floor
wait_for_dashboard_floor
restore_worker_zone_coverage

echo "==> final"
print_sample
printf 'async_work_backlog=%s\n' "$(dashboard_async_work_backlog)"
printf 'async_outbox_backlog_above_target=%s\n' "$(dashboard_async_outbox_backlog_above_target)"
printf 'async_relay_autoscale_above_min=%s\n' "$(dashboard_async_relay_autoscale_above_min)"
printf 'async_relay_blockers=%s\n' "$(dashboard_async_relay_blockers)"
printf 'async_dlq_pending=%s\n' "$(dashboard_async_dlq_pending)"
printf 'outbox_scale_event_seconds_10m=%s\n' "$(dashboard_outbox_scale_event_seconds_10m)"
kubectl -n "${namespace}" get hpa "${hpa}"
kubectl -n "${namespace}" get pods -l "${worker_selector}" -o wide

if [[ "${scaled}" != "true" ]]; then
  echo "outbox-relay did not scale" >&2
  exit 1
fi
