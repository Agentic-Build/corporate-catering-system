#!/usr/bin/env bash
set -euo pipefail

cd "$(dirname "$0")/../.."

cluster_name="${CLUSTER_NAME:-tbite-local-ha}"
tag="${TAG:-local}"
build="${BUILD:-true}"

images=(
  "ghcr.io/agentic-build/tbite-api:${tag}"
  "ghcr.io/agentic-build/tbite-web-employee:${tag}"
  "ghcr.io/agentic-build/tbite-web-merchant:${tag}"
  "ghcr.io/agentic-build/tbite-web-admin:${tag}"
  "ghcr.io/agentic-build/tbite-migrations:${tag}"
)

if [[ "${build}" == "true" ]]; then
  docker build -f services/api/Dockerfile -t "ghcr.io/agentic-build/tbite-api:${tag}" .
  docker build -f apps/employee/Dockerfile -t "ghcr.io/agentic-build/tbite-web-employee:${tag}" .
  docker build -f apps/merchant/Dockerfile -t "ghcr.io/agentic-build/tbite-web-merchant:${tag}" .
  docker build -f apps/admin/Dockerfile -t "ghcr.io/agentic-build/tbite-web-admin:${tag}" .
  docker build -f migrations/Dockerfile -t "ghcr.io/agentic-build/tbite-migrations:${tag}" .
fi

kind load docker-image --name "${cluster_name}" "${images[@]}"

printf 'loaded images into kind cluster %s:\n' "${cluster_name}"
printf '  %s\n' "${images[@]}"
echo "deploy with: IMAGE_TAG=${tag} scripts/local-ha/deploy.sh"
