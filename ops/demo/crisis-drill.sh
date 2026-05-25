#!/usr/bin/env bash
set -euo pipefail

# Delete one pod from a selected component during a demo. Kubernetes should
# recreate it; the operator should watch Grafana and kubectl rollout status.
#
# Usage:
#   ops/demo/crisis-drill.sh api
#   ops/demo/crisis-drill.sh realtime
#   ops/demo/crisis-drill.sh worker-outbox-relay
#   ops/demo/crisis-drill.sh cloudflared
#
# Optional env:
#   NS=tbite

NS="${NS:-tbite}"
COMPONENT="${1:-api}"

case "$COMPONENT" in
  api|realtime|worker-outbox-relay|worker-payroll-settler|worker-on-time-evaluator|cloudflared|minio)
    ;;
  *)
    echo "unsupported component: $COMPONENT" >&2
    echo "supported: api realtime worker-outbox-relay worker-payroll-settler worker-on-time-evaluator cloudflared minio" >&2
    exit 2
    ;;
esac

POD="$(
  kubectl -n "$NS" get pods \
    -l "app.kubernetes.io/component=${COMPONENT}" \
    -o jsonpath='{.items[0].metadata.name}'
)"

if [ -z "$POD" ]; then
  echo "no pod found for component ${COMPONENT} in namespace ${NS}" >&2
  exit 1
fi

echo "==> deleting ${NS}/${POD}"
kubectl -n "$NS" delete pod "$POD"

echo "==> watch recovery"
echo "kubectl -n ${NS} rollout status deploy -l app.kubernetes.io/component=${COMPONENT} --timeout=3m"
echo "kubectl -n ${NS} get pods -l app.kubernetes.io/component=${COMPONENT} -w"
