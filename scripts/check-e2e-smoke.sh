#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
EVIDENCE_OUTPUT=""

usage() {
  cat <<'USAGE'
Usage: scripts/check-e2e-smoke.sh [--evidence-output <path>]

Runs role-critical smoke coverage for employee/vendor/admin paths.
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

pushd "${ROOT_DIR}" >/dev/null

# Rust integration smoke coverage for core runtime critical paths by role.
OTEL_EXPORTER_OTLP_ENDPOINT="${OTEL_EXPORTER_OTLP_ENDPOINT:-http://127.0.0.1:4317}" \
  cargo test --test http_menu_supply_execution_gateway
OTEL_EXPORTER_OTLP_ENDPOINT="${OTEL_EXPORTER_OTLP_ENDPOINT:-http://127.0.0.1:4317}" \
  cargo test --test http_vendor_fulfillment_execution_gateway
OTEL_EXPORTER_OTLP_ENDPOINT="${OTEL_EXPORTER_OTLP_ENDPOINT:-http://127.0.0.1:4317}" \
  cargo test --test identity_access_governance

# Web runtime smoke coverage for role-aware portal behavior.
pnpm --dir apps/web exec tsx --test \
  tests/employee-portal.test.ts \
  tests/platform-navigation.test.ts \
  tests/admin-portal.test.ts \
  tests/runtime.test.ts

if [[ -n "${EVIDENCE_OUTPUT}" ]]; then
  mkdir -p "$(dirname "${EVIDENCE_OUTPUT}")"
  cat >"${EVIDENCE_OUTPUT}" <<JSON
{
  "gate": "e2e-smoke",
  "status": "passed",
  "generatedAt": "$(date -u +"%Y-%m-%dT%H:%M:%SZ")",
  "commitSha": "${GITHUB_SHA:-$(git rev-parse HEAD)}",
  "coverage": {
    "employee": [
      "tests/http_menu_supply_execution_gateway.rs",
      "apps/web/tests/employee-portal.test.ts",
      "apps/web/tests/runtime.test.ts"
    ],
    "vendor": [
      "tests/http_vendor_fulfillment_execution_gateway.rs",
      "apps/web/tests/platform-navigation.test.ts",
      "apps/web/tests/runtime.test.ts"
    ],
    "admin": [
      "tests/identity_access_governance.rs",
      "apps/web/tests/admin-portal.test.ts",
      "apps/web/tests/runtime.test.ts"
    ]
  }
}
JSON
fi

popd >/dev/null

echo "e2e-smoke gate passed"
