#!/usr/bin/env bash
set -euo pipefail
CLUSTER="${CLUSTER:-tbite}"
if k3d cluster list 2>/dev/null | grep -q "^${CLUSTER}\b"; then
  echo "deleting k3d cluster ${CLUSTER}..."
  k3d cluster delete "${CLUSTER}"
else
  echo "no k3d cluster ${CLUSTER} to delete"
fi
