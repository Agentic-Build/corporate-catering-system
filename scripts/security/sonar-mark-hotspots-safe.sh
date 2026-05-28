#!/usr/bin/env bash
# Usage: SONAR_TOKEN=<token> ./scripts/security/sonar-mark-hotspots-safe.sh
set -euo pipefail

: "${SONAR_TOKEN:?Set SONAR_TOKEN before running}"
PROJECT="Agentic-Build_corporate-catering-system"

mark() {
  local key="$1" comment="$2"
  curl -fsS -u "${SONAR_TOKEN}:" -X POST \
    "https://sonarcloud.io/api/hotspots/change_status" \
    -d "hotspot=${key}" \
    -d "status=REVIEWED" \
    -d "resolution=SAFE" \
    -d "comment=${comment}" >/dev/null
  echo "  ✓ ${key}"
}

hotspots=$(curl -fsS -u "${SONAR_TOKEN}:" \
  "https://sonarcloud.io/api/hotspots/search?projectKey=${PROJECT}&status=TO_REVIEW&ps=100")
echo "Found $(echo "$hotspots" | jq -r '.paging.total') hotspots."

echo "$hotspots" | jq -r '.hotspots[]
  | select(.ruleKey == "kubernetes:S5332")
  | select(.component | contains("chart/tbite-platform/templates"))
  | .key' | while read -r key; do
  mark "$key" "In-cluster traffic only; backend TLS is a platform-level decision."
done

echo "$hotspots" | jq -r '.hotspots[]
  | select(.ruleKey == "typescript:S5332")
  | select(.component | contains("tests/e2e"))
  | .key' | while read -r key; do
  mark "$key" "Local-dev fallback; production overrides via E2E_BASE_URL."
done

remaining=$(curl -fsS -u "${SONAR_TOKEN}:" \
  "https://sonarcloud.io/api/hotspots/search?projectKey=${PROJECT}&status=TO_REVIEW&ps=100" \
  | jq -r '.hotspots[] | "\(.ruleKey) \(.component | split(":")[-1]):\(.line // "?")"')
if [[ -n "$remaining" ]]; then
  echo "Remaining:"
  echo "$remaining"
else
  echo "All hotspots marked. ✓"
fi
