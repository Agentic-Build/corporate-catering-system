#!/usr/bin/env bash
# One-stop dev runner: brings up deps (Postgres/Redis/NATS/MinIO) via docker
# compose, applies migrations, seeds e2e + p2 fixtures, then runs the Go API
# and the three SvelteKit dev servers on the host. Ctrl-C stops the host
# processes; compose deps stay running (use `make dev-down` to stop them).
set -euo pipefail

cd "$(dirname "$0")/../.."

COMPOSE="docker compose -f ops/local/docker-compose.dev.yml"
DB_DSN="postgres://tbite:tbite@localhost:5432/tbite?sslmode=disable"

echo "==> deps"
$COMPOSE up -d

echo "==> waiting for postgres"
for i in {1..40}; do
  if $COMPOSE exec -T postgres pg_isready -U tbite -d tbite >/dev/null 2>&1; then
    break
  fi
  sleep 0.5
done

echo "==> migrate"
DATABASE_URL="$DB_DSN" scripts/db/migrate.sh up

echo "==> seed"
$COMPOSE exec -T postgres psql -U tbite -d tbite < scripts/dev/seed-e2e.sql >/dev/null
$COMPOSE exec -T postgres psql -U tbite -d tbite < scripts/dev/seed-p2.sql >/dev/null

cat <<EOF

==> ready
   employee   http://localhost:5173
   merchant   http://localhost:5174
   admin      http://localhost:5175
   api        http://localhost:8080/healthz
   minio      http://localhost:9001 (tbite / tbite-dev-secret)

Ctrl-C stops host processes. Deps stay up — 'make dev-down' to stop them.

EOF

trap 'kill 0' EXIT

export FAKE_OIDC=1
export DATABASE_RW_URL="$DB_DSN"
export REDIS_URL="redis://localhost:6379"
export NATS_URL="nats://localhost:4222"
export S3_ENDPOINT="http://localhost:9000"
export S3_REGION="us-east-1"
export S3_ACCESS_KEY_ID="tbite"
export S3_SECRET_ACCESS_KEY="tbite-dev-secret"
export S3_BUCKET="tbite"
export S3_USE_PATH_STYLE=1

go run ./services/api/cmd/tbite --role=api &
pnpm --filter @tbite/employee dev &
pnpm --filter @tbite/merchant dev &
pnpm --filter @tbite/admin dev &
wait
