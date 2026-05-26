#!/usr/bin/env bash
set -euo pipefail

# Run the k6 lunch-peak load test against an already deployed platform.
# The canonical local target is the Helm dev chart on kind/k3d/OrbStack.

: "${API_BASE_URL:?API_BASE_URL is required, e.g. http://api.tbite.local}"
: "${K6_TOKEN_EMPLOYEE:?K6_TOKEN_EMPLOYEE is required}"
: "${K6_PLANT:?K6_PLANT is required}"
: "${K6_MENU_ITEM_ID:?K6_MENU_ITEM_ID is required}"
: "${K6_READY_ORDER_IDS:?K6_READY_ORDER_IDS is required}"

if ! command -v k6 >/dev/null 2>&1; then
  echo "k6 is required to run the load test." >&2
  exit 1
fi

export K6_DAY="${K6_DAY:-$(date -u +%Y-%m-%d)}"
SUMMARY_PATH="${SUMMARY_PATH:-ops/load/last-summary.json}"

k6 run --summary-export="${SUMMARY_PATH}" ops/load/k6-lunch-peak.js

echo "loadtest complete. summary at ${SUMMARY_PATH}"
