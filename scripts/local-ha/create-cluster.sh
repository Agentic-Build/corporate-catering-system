#!/usr/bin/env bash
set -euo pipefail

cd "$(dirname "$0")/../.."

cluster_name="${CLUSTER_NAME:-tbite-local-ha}"
config="${KIND_CONFIG:-ops/local-ha/kind-multiaz.yaml}"

command -v kind >/dev/null 2>&1 || {
  echo "kind is required" >&2
  exit 1
}
command -v kubectl >/dev/null 2>&1 || {
  echo "kubectl is required" >&2
  exit 1
}

if kind get clusters | grep -qx "${cluster_name}"; then
  if [[ "${RESET:-false}" == "true" ]]; then
    kind delete cluster --name "${cluster_name}"
  else
    echo "kind cluster ${cluster_name} already exists"
    kubectl config use-context "kind-${cluster_name}" >/dev/null
    kubectl get nodes -L topology.kubernetes.io/zone
    exit 0
  fi
fi

kind create cluster --name "${cluster_name}" --config "${config}" --wait 120s
kubectl config use-context "kind-${cluster_name}" >/dev/null
kubectl get nodes -L topology.kubernetes.io/zone
