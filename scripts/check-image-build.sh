#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
EVIDENCE_OUTPUT=""

usage() {
  cat <<'USAGE'
Usage: scripts/check-image-build.sh [--evidence-output <path>]

Build-correctness gate for runtime/web image inputs.
USAGE
}

require_command() {
  local cmd="$1"
  if ! command -v "${cmd}" >/dev/null 2>&1; then
    echo "missing required command: ${cmd}" >&2
    exit 1
  fi
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --evidence-output)
      shift
      [[ $# -gt 0 ]] || {
        echo "--evidence-output requires a path" >&2
        exit 1
      }
      EVIDENCE_OUTPUT="$1"
      shift
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      echo "unknown argument: $1" >&2
      usage >&2
      exit 1
      ;;
  esac
done

require_command cargo
require_command pnpm
require_command node
require_command rg

pushd "${ROOT_DIR}" >/dev/null

pnpm install --frozen-lockfile
pnpm --dir apps/web install --frozen-lockfile

pnpm run contract:verify

cargo build --locked --release --bin apply_sql_migrations --bin export_openapi_contract --bin observability_runtime_service

pnpm --dir apps/web run check
MOCK_AUTH_SIGNING_SECRET="${MOCK_AUTH_SIGNING_SECRET:-clar-002-ci-mock-auth-signing-secret}" \
  pnpm --dir apps/web run build

image_refs_file="$(mktemp -t image-build-refs.XXXXXX.txt)"
rg --no-filename --only-matching "image:[[:space:]]+[^[:space:]]+" \
  ops/kubernetes/base/deployment.yaml \
  ops/kubernetes/base/deployment-mcp.yaml \
  ops/kubernetes/base/deployment-compliance-worker.yaml \
  ops/kubernetes/base/deployment-web.yaml \
  | sed -E 's/^image:[[:space:]]+//' \
  | sort -u >"${image_refs_file}"

if [[ ! -s "${image_refs_file}" ]]; then
  echo "failed to collect runtime image refs from kubernetes manifests" >&2
  rm -f "${image_refs_file}"
  exit 1
fi

if [[ -n "${EVIDENCE_OUTPUT}" ]]; then
  mkdir -p "$(dirname "${EVIDENCE_OUTPUT}")"
  image_refs_json="$(sed 's/.*/"&"/' "${image_refs_file}" | paste -sd, -)"
  if [[ -z "${image_refs_json}" ]]; then
    image_refs_json=""
  fi
  cat >"${EVIDENCE_OUTPUT}" <<JSON
{
  "gate": "image-build",
  "status": "passed",
  "generatedAt": "$(date -u +"%Y-%m-%dT%H:%M:%SZ")",
  "commitSha": "${GITHUB_SHA:-$(git rev-parse HEAD)}",
  "imageRefs": [${image_refs_json}],
  "checks": [
    "contract:verify",
    "cargo build --release (apply_sql_migrations/export_openapi_contract/observability_runtime_service)",
    "apps/web check + build"
  ]
}
JSON
fi

rm -f "${image_refs_file}"

popd >/dev/null

echo "image-build gate passed"
