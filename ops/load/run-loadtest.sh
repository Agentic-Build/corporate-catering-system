#!/usr/bin/env bash
set -euo pipefail

# run-loadtest.sh: Run the k6 lunch-peak load test against a local stack.
# Requirements: k6 (install: brew install k6 / curl ... | bash), docker, go.
# Optional env: PRE_RUNNING=1 (skip stack boot — assume pg/redis/nats already up)

CONTAINER_PREFIX="${CONTAINER_PREFIX:-p8-load}"
KEEP_RUNNING="${KEEP_RUNNING:-0}"

cleanup() {
  if [ "$KEEP_RUNNING" = "1" ]; then
    echo "KEEP_RUNNING=1 — leaving containers and api running"
    return
  fi
  echo "tearing down..."
  jobs -p | xargs -I{} kill {} 2>/dev/null || true
  docker rm -f "${CONTAINER_PREFIX}-pg" "${CONTAINER_PREFIX}-rd" "${CONTAINER_PREFIX}-nats" 2>/dev/null || true
}
trap cleanup EXIT

if [ "${PRE_RUNNING:-0}" != "1" ]; then
  echo "starting containers..."
  docker run -d --name "${CONTAINER_PREFIX}-pg" -e POSTGRES_USER=tbite -e POSTGRES_PASSWORD=tbite -e POSTGRES_DB=tbite -p 55432:5432 postgres:18-alpine
  docker run -d --name "${CONTAINER_PREFIX}-rd" -p 56379:6379 redis:7-alpine
  docker run -d --name "${CONTAINER_PREFIX}-nats" -p 54222:4222 nats:2.10-alpine -js
  sleep 5

  echo "running migrations..."
  DATABASE_URL="postgres://tbite:tbite@localhost:55432/tbite?sslmode=disable" scripts/db/migrate.sh up

  echo "seeding..."
  docker exec -i "${CONTAINER_PREFIX}-pg" psql -U tbite -d tbite < scripts/dev/seed-p2.sql >/dev/null
fi

# Start API (background)
echo "starting api..."
echo "requires Authentik on http://localhost:9002; run make dev or ops/local/docker-compose.dev.yml first"
DATABASE_RW_URL="postgres://tbite:tbite@localhost:55432/tbite?sslmode=disable" \
REDIS_URL="redis://localhost:56379/0" \
NATS_URL="nats://localhost:54222" \
OIDC_CALLBACK_BASE_URL="http://localhost:8080" \
AUTH_PROVIDER_SLUGS="authentik" \
AUTH_PROVIDER_AUTHENTIK_DISPLAY_NAME="Authentik" \
AUTH_PROVIDER_AUTHENTIK_ISSUER_URL="http://localhost:9002/application/o/tbite/" \
AUTH_PROVIDER_AUTHENTIK_CLIENT_ID="tbite-local" \
AUTH_PROVIDER_AUTHENTIK_CLIENT_SECRET="tbite-local-client-secret" \
AUTH_PROVIDER_AUTHENTIK_SCOPES="openid email profile tbite" \
AUTHENTIK_BASE_URL="http://localhost:9002" \
AUTHENTIK_API_TOKEN="tbite-dev-authentik-api-token" \
AUTHENTIK_VENDOR_OPERATOR_GROUP="tbite:role:vendor_operator" \
APP_BASE_URL_EMPLOYEE="http://localhost:5173" \
APP_BASE_URL_MERCHANT="http://localhost:5174" \
APP_BASE_URL_ADMIN="http://localhost:5175" \
S3_ENDPOINT="http://localhost:9000" S3_ACCESS_KEY_ID=x S3_SECRET_ACCESS_KEY=x S3_BUCKET=tbite \
go run ./services/api/cmd/tbite --role=api &
sleep 4

# Mint a fake employee + session via SQL + Redis
docker exec "${CONTAINER_PREFIX}-pg" psql -U tbite -d tbite -c "
INSERT INTO \"user\" (id, primary_email, display_name, role, status, plant)
VALUES ('22222222-2222-2222-2222-222222222222', 'load@tbite.test', 'Load', 'employee', 'active', 'F12B-3F')
ON CONFLICT (id) DO NOTHING;
" >/dev/null

# Insert session into Redis directly: key sess:tb_load TTL 7d
docker exec "${CONTAINER_PREFIX}-rd" redis-cli SET "sess:tb_load" \
  '{"user_id":"22222222-2222-2222-2222-222222222222","role":"employee","created_at":"2026-05-13T00:00:00Z","last_seen_at":"2026-05-13T00:00:00Z"}' \
  EX 86400 >/dev/null

# Pick a seeded menu item
MENU_ITEM_ID=$(docker exec "${CONTAINER_PREFIX}-pg" psql -U tbite -d tbite -tAc "
SELECT id FROM menu_item WHERE status='active' LIMIT 1
")

# (Optional) READY orders for pickup_code scenario: seed a few via SQL
docker exec "${CONTAINER_PREFIX}-pg" psql -U tbite -d tbite -c "
INSERT INTO \"order\" (id, user_id, vendor_id, plant, supply_date, status, total_price_minor, placed_at, cutoff_at, ready_at, totp_secret)
SELECT
  gen_random_uuid(),
  '22222222-2222-2222-2222-222222222222',
  (SELECT vendor_id FROM menu_item WHERE id='${MENU_ITEM_ID}'),
  'F12B-3F',
  CURRENT_DATE,
  'ready',
  110,
  now() - interval '1 hour',
  now() + interval '6 hours',
  now() - interval '30 minutes',
  decode('00','hex')
FROM generate_series(1, 5);
" >/dev/null

READY_ORDER_IDS=$(docker exec "${CONTAINER_PREFIX}-pg" psql -U tbite -d tbite -tAc "
SELECT string_agg(id::text, ',') FROM \"order\" WHERE status='ready' AND user_id='22222222-2222-2222-2222-222222222222'
")

echo "running k6..."
K6_TOKEN_EMPLOYEE=tb_load \
K6_PLANT=F12B-3F \
K6_DAY=$(date -u +%Y-%m-%d) \
K6_MENU_ITEM_ID="${MENU_ITEM_ID}" \
K6_READY_ORDER_IDS="${READY_ORDER_IDS}" \
k6 run --summary-export=ops/load/last-summary.json ops/load/k6-lunch-peak.js

echo "loadtest complete. summary at ops/load/last-summary.json"
