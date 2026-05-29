#!/usr/bin/env bash
set -euo pipefail

namespace="${NAMESPACE:-tbite}"
timeout="${TIMEOUT:-15m}"

wait_rollouts() {
  local kind="$1"
  local resource
  local strategy
  while IFS= read -r resource; do
    [[ -n "${resource}" ]] || continue
    if [[ "${kind}" == "statefulset" ]]; then
      strategy="$(kubectl -n "${namespace}" get "${resource}" -o jsonpath='{.spec.updateStrategy.type}')"
      if [[ "${strategy}" != "RollingUpdate" ]]; then
        echo "skipping ${resource} rollout status; updateStrategy=${strategy}"
        continue
      fi
    fi
    kubectl -n "${namespace}" rollout status "${resource}" --timeout="${timeout}"
  done < <(kubectl -n "${namespace}" get "${kind}" -o name)
}

wait_pods_ready() {
  local deadline not_ready
  deadline=$((SECONDS + 900))
  while true; do
    not_ready="$(
      kubectl -n "${namespace}" get pods -o json \
        | jq -r '
            .items[]
            | select(.metadata.deletionTimestamp == null)
            | select(.status.phase != "Succeeded")
            | select((([.status.conditions[]? | select(.type == "Ready") | .status] | first) // "False") != "True")
            | [.metadata.name, .status.phase]
            | @tsv
          '
    )"
    if [[ -z "${not_ready}" ]]; then
      return 0
    fi
    if (( SECONDS > deadline )); then
      echo "timed out waiting for pods to become ready:" >&2
      printf 'pod\tphase\n' >&2
      printf '%s\n' "${not_ready}" >&2
      exit 1
    fi
    sleep 5
  done
}

wait_rollouts deployment
wait_rollouts statefulset
wait_rollouts daemonset
wait_pods_ready

deadline=$((SECONDS + 900))
while true; do
  phase="$(kubectl -n "${namespace}" get cluster tbite-pg -o jsonpath='{.status.phase}' 2>/dev/null || true)"
  [[ "${phase}" == *"healthy"* || "${phase}" == *"Healthy"* ]] && break
  if (( SECONDS > deadline )); then
    echo "timed out waiting for tbite-pg; phase=${phase}" >&2
    exit 1
  fi
  sleep 5
done

kubectl -n "${namespace}" get hpa
kubectl -n "${namespace}" get scaledobject 2>/dev/null || true
kubectl -n "${namespace}" get pods -o wide
