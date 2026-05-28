#!/usr/bin/env bash
# Mark remaining SonarCloud hotspots as REVIEWED + Safe, with the rationale
# inline. Hotspots are intended for human review — the API call records the
# review decision (not the same as `sonar.issue.ignore`).
#
# Usage:
#   SONAR_TOKEN=<your-token> ./scripts/security/sonar-mark-hotspots-safe.sh
#
# Get a token at https://sonarcloud.io > My Account > Security > Generate Token.

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

echo "Fetching open hotspots..."
hotspots=$(curl -fsS -u "${SONAR_TOKEN}:" \
  "https://sonarcloud.io/api/hotspots/search?projectKey=${PROJECT}&status=TO_REVIEW&ps=100")

count=$(echo "$hotspots" | jq -r '.paging.total')
echo "Found $count hotspots."

# kubernetes:S5332 in chart/templates → in-cluster traffic; backend TLS is an
# infra-level decision tracked separately from SonarCloud.
echo
echo "Marking kubernetes:S5332 in chart/ as Safe (in-cluster plaintext is by design)..."
echo "$hotspots" | jq -r '.hotspots[]
  | select(.ruleKey == "kubernetes:S5332")
  | select(.component | contains("chart/tbite-platform/templates"))
  | .key' | while read -r key; do
  mark "$key" "In-cluster traffic only; backend TLS (Postgres, NATS, Valkey, MinIO, OTel, Authentik) is a platform-level decision, not addressed per-template. Tracked separately."
done

# typescript:S5332 in tests/e2e → local dev fallback for Playwright; CI sets BASE_URL.
echo
echo "Marking typescript:S5332 in tests/e2e/ as Safe..."
echo "$hotspots" | jq -r '.hotspots[]
  | select(.ruleKey == "typescript:S5332")
  | select(.component | contains("tests/e2e"))
  | .key' | while read -r key; do
  mark "$key" "Local-dev fallback; production runs override via E2E_BASE_URL."
done

# Anything left → list so the operator can review manually.
echo
remaining=$(curl -fsS -u "${SONAR_TOKEN}:" \
  "https://sonarcloud.io/api/hotspots/search?projectKey=${PROJECT}&status=TO_REVIEW&ps=100" \
  | jq -r '.hotspots[] | "\(.ruleKey) \(.component | split(":")[-1]):\(.line // "?")"')
if [ -n "$remaining" ]; then
  echo "Remaining (review manually at https://sonarcloud.io/project/security_hotspots?id=${PROJECT}):"
  echo "$remaining"
else
  echo "All hotspots marked. ✓"
fi
