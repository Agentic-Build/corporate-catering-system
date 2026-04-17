#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
OUTPUT_DIR="${ROOT_DIR}/ops/release/artifacts"

usage() {
  cat <<'USAGE'
Usage: scripts/render-production-overlay.sh [--output-dir <path>]

Renders production kustomize overlay and writes promotion artifact + digest.
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
    --output-dir)
      shift
      [[ $# -gt 0 ]] || {
        echo "--output-dir requires a path" >&2
        exit 1
      }
      OUTPUT_DIR="$1"
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

require_command kustomize
require_command sha256sum
require_command rg

manifest_path="${OUTPUT_DIR}/production-overlay.yaml"
digest_path="${OUTPUT_DIR}/production-overlay.sha256"
images_path="${OUTPUT_DIR}/production-overlay-images.txt"
summary_path="${OUTPUT_DIR}/production-overlay-summary.txt"

mkdir -p "${OUTPUT_DIR}"
kustomize build "${ROOT_DIR}/ops/kubernetes/overlays/production" >"${manifest_path}"

for workload in \
  corporate-catering-api \
  corporate-catering-mcp \
  corporate-catering-compliance-worker \
  corporate-catering-web

do
  rg -q "name:[[:space:]]+${workload}" "${manifest_path}"
done

rg -q "runtime.corporate-catering.io/environment:[[:space:]]+production" "${manifest_path}"
rg -q "key:[[:space:]]+prod/corporate-catering/runtime" "${manifest_path}"

rg --no-filename --only-matching "image:[[:space:]]+[^[:space:]]+" "${manifest_path}" \
  | sed -E 's/^image:[[:space:]]+//' \
  | sort -u >"${images_path}"

if [[ ! -s "${images_path}" ]]; then
  echo "rendered production overlay does not contain any container image refs" >&2
  exit 1
fi

manifest_digest="$(sha256sum "${manifest_path}" | awk '{print $1}')"
printf '%s  %s\n' "${manifest_digest}" "$(basename "${manifest_path}")" >"${digest_path}"

cat >"${summary_path}" <<SUMMARY
renderedAt=$(date -u +"%Y-%m-%dT%H:%M:%SZ")
commitSha=${GITHUB_SHA:-$(git rev-parse HEAD)}
overlay=production
manifest=${manifest_path}
digest=${manifest_digest}
imageRefCount=$(wc -l <"${images_path}" | tr -d '[:space:]')
SUMMARY

echo "production overlay artifact rendered"
echo "manifest: ${manifest_path}"
echo "digest: ${manifest_digest}"
