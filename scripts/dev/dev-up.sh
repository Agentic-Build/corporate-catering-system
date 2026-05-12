#!/usr/bin/env bash
set -euo pipefail

CLUSTER="${CLUSTER:-tbite}"
NAMESPACE="${NAMESPACE:-tbite}"

cd "$(dirname "$0")/../.."

# Create k3d cluster if not exists. Disable Traefik so we can install NGINX Ingress.
if ! k3d cluster list 2>/dev/null | grep -q "^${CLUSTER}\b"; then
  echo "creating k3d cluster ${CLUSTER}..."
  k3d cluster create "${CLUSTER}" \
    --port "80:80@loadbalancer" \
    --port "443:443@loadbalancer" \
    --k3s-arg "--disable=traefik@server:0"
else
  echo "k3d cluster ${CLUSTER} already exists"
fi

# Create namespace idempotently.
kubectl create namespace "${NAMESPACE}" --dry-run=client -o yaml | kubectl apply -f -

# Install ingress-nginx (lightweight pin).
if ! kubectl get ns ingress-nginx >/dev/null 2>&1; then
  echo "installing ingress-nginx..."
  kubectl apply -f https://raw.githubusercontent.com/kubernetes/ingress-nginx/controller-v1.11.2/deploy/static/provider/cloud/deploy.yaml
  kubectl -n ingress-nginx wait deploy/ingress-nginx-controller --for=condition=Available --timeout=180s
fi

# Apply single-node overlay.
kubectl kustomize ops/kubernetes/overlays/single-node | kubectl apply -f -

# Wait for core deployments (best effort).
for d in api postgres redis nats minio; do
  kubectl -n "${NAMESPACE}" rollout status deploy/"${d}" --timeout=180s || true
done

cat <<EOF

Cluster up. Add these to /etc/hosts:
  127.0.0.1 app.tbite.test merchant.tbite.test admin.tbite.test api.tbite.test

Then open: http://app.tbite.test/
EOF
