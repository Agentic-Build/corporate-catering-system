#!/usr/bin/env bash
set -euo pipefail

cd "$(dirname "$0")/../.."

namespace="${NAMESPACE:-tbite}"
deployment_selector="${DEPLOYMENT_SELECTOR:-app.kubernetes.io/name=tbite-platform}"

zone_count="$(
  kubectl get nodes --selector='!node-role.kubernetes.io/control-plane' -o json \
    | jq '[.items[].metadata.labels["topology.kubernetes.io/zone"] // empty] | unique | length'
)"

if [[ "${zone_count}" -eq 0 ]]; then
  echo "no worker zones found; cannot verify app zone coverage" >&2
  exit 1
fi

mapfile -t deployments < <(kubectl -n "${namespace}" get deployment -l "${deployment_selector}" -o jsonpath='{range .items[*]}{.metadata.name}{"\n"}{end}')
if [[ "${#deployments[@]}" -eq 0 ]]; then
  echo "no deployments match selector ${deployment_selector}" >&2
  exit 1
fi

echo "==> restarting app deployments to restore topology spread"
kubectl -n "${namespace}" rollout restart deployment -l "${deployment_selector}"

scripts/local-ha/wait-ready.sh

zone_for_node() {
  local node="$1"
  kubectl get node "${node}" -o jsonpath='{.metadata.labels.topology\.kubernetes\.io/zone}'
}

component_zone_count() {
  local component="$1"
  local pod_json node zone
  pod_json="$(kubectl -n "${namespace}" get pods -l "${deployment_selector},app.kubernetes.io/component=${component}" -o json)"
  while IFS= read -r node; do
    [[ -n "${node}" ]] || continue
    zone="$(zone_for_node "${node}")"
    [[ -n "${zone}" ]] && printf '%s\n' "${zone}"
  done < <(jq -r '.items[] | select(.status.phase == "Running") | .spec.nodeName // empty' <<<"${pod_json}") \
    | sort -u \
    | wc -l \
    | tr -d ' '
}

echo "==> verifying app zone coverage"
gaps=0
printf 'deployment\tcomponent\treplicas\texpected_zones\tactual_zones\n'
for deployment in "${deployments[@]}"; do
  desired="$(kubectl -n "${namespace}" get deployment "${deployment}" -o jsonpath='{.spec.replicas}')"
  component="$(kubectl -n "${namespace}" get deployment "${deployment}" -o jsonpath='{.metadata.labels.app\.kubernetes\.io/component}')"
  expected="${desired}"
  if (( expected > zone_count )); then
    expected="${zone_count}"
  fi
  actual="$(component_zone_count "${component}")"
  printf '%s\t%s\t%s\t%s\t%s\n' "${deployment}" "${component}" "${desired}" "${expected}" "${actual}"
  if (( actual < expected )); then
    gaps=$((gaps + 1))
  fi
done

if (( gaps > 0 )); then
  echo "zone coverage gaps remain after rebalance: ${gaps}" >&2
  exit 1
fi

echo "==> app zone coverage restored"
