#!/usr/bin/env bash
set -euo pipefail

# Drive demo traffic against the Kubernetes deployment through local
# port-forwards, so Grafana dashboards and alerts have live signals.
#
# Optional env:
#   NS=tbite
#   API_SERVICE=svc/tbite-api
#   DB_SERVICE=svc/tbite-pg-rw
#   VALKEY_SERVICE=svc/tbite-valkey-primary
#   DB_SECRET=tbite-db
#   DB_SECRET_KEY=rwUrl
#   VALKEY_SECRET=tbite-valkey
#   VALKEY_SECRET_KEY=password
#   SCENARIO=lunch-crunch
#   DURATION=5m
#   RPS=12
#   CONCURRENCY=16
#   EMPLOYEES=800

NS="${NS:-tbite}"
API_SERVICE="${API_SERVICE:-svc/tbite-api}"
DB_SERVICE="${DB_SERVICE:-svc/tbite-pg-rw}"
VALKEY_SERVICE="${VALKEY_SERVICE:-svc/tbite-valkey-primary}"
DB_SECRET="${DB_SECRET:-tbite-db}"
DB_SECRET_KEY="${DB_SECRET_KEY:-rwUrl}"
VALKEY_SECRET="${VALKEY_SECRET:-tbite-valkey}"
VALKEY_SECRET_KEY="${VALKEY_SECRET_KEY:-password}"

LOCAL_API_PORT="${LOCAL_API_PORT:-18080}"
LOCAL_DB_PORT="${LOCAL_DB_PORT:-15432}"
LOCAL_VALKEY_PORT="${LOCAL_VALKEY_PORT:-16379}"

SCENARIO="${SCENARIO:-lunch-crunch}"
DURATION="${DURATION:-5m}"
RPS="${RPS:-12}"
CONCURRENCY="${CONCURRENCY:-16}"
EMPLOYEES="${EMPLOYEES:-800}"
TARGET_PLANT="${TARGET_PLANT:-hc-12a-1f}"
TARGET_VENDOR="${TARGET_VENDOR:-a1111111-1111-1111-1111-111111111111}"
HOT_ITEM="${HOT_ITEM:-4f26e612-b35f-5500-8f2a-63eded235675}"
PLANTS="${PLANTS:-hc-12a-1f,hc-12a-3f,hc-12b-1f,tc-15a-1f,tn-18p1-1f,tn-18p3-1f,tn-18p7-2f}"

cleanup() {
  if [ "${#PIDS[@]}" -gt 0 ]; then
    kill "${PIDS[@]}" >/dev/null 2>&1 || true
  fi
}
PIDS=()
trap cleanup EXIT

port_forward() {
  local target="$1"
  local mapping="$2"
  kubectl -n "$NS" port-forward "$target" "$mapping" >/tmp/tbite-demo-port-forward.log 2>&1 &
  PIDS+=("$!")
}

DATABASE_RW_URL="$(
  kubectl -n "$NS" get secret "$DB_SECRET" \
    -o "jsonpath={.data.${DB_SECRET_KEY}}" | base64 -d
)"
VALKEY_PASSWORD="$(
  kubectl -n "$NS" get secret "$VALKEY_SECRET" \
    -o "jsonpath={.data.${VALKEY_SECRET_KEY}}" | base64 -d
)"

if [ -z "$DATABASE_RW_URL" ]; then
  echo "empty database URL from secret ${NS}/${DB_SECRET}:${DB_SECRET_KEY}" >&2
  exit 1
fi
if [ -z "$VALKEY_PASSWORD" ]; then
  echo "empty Valkey password from secret ${NS}/${VALKEY_SECRET}:${VALKEY_SECRET_KEY}" >&2
  exit 1
fi

port_forward "$API_SERVICE" "${LOCAL_API_PORT}:80"
port_forward "$DB_SERVICE" "${LOCAL_DB_PORT}:5432"
port_forward "$VALKEY_SERVICE" "${LOCAL_VALKEY_PORT}:6379"
sleep 3

LOCAL_DATABASE_RW_URL="$(printf '%s' "$DATABASE_RW_URL" | sed -E "s#@[^/@?]+(:[0-9]+)?/#@127.0.0.1:${LOCAL_DB_PORT}/#")"
LOCAL_REDIS_URL="redis://:${VALKEY_PASSWORD}@127.0.0.1:${LOCAL_VALKEY_PORT}/0"

echo "==> running ${SCENARIO} for ${DURATION}"
go run ./services/api/cmd/stress \
  --base-url="http://127.0.0.1:${LOCAL_API_PORT}" \
  --db="$LOCAL_DATABASE_RW_URL" \
  --redis="$LOCAL_REDIS_URL" \
  --scenario="$SCENARIO" \
  --duration="$DURATION" \
  --rps="$RPS" \
  --concurrency="$CONCURRENCY" \
  --employees="$EMPLOYEES" \
  --plants="$PLANTS" \
  --target-plant="$TARGET_PLANT" \
  --target-vendor="$TARGET_VENDOR" \
  --hot-item="$HOT_ITEM"
