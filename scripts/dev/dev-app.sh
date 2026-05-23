#!/usr/bin/env bash
# One-stop dev runner: brings up deps (Postgres/Redis/NATS/MinIO/Authentik) via docker
# compose, applies migrations, seeds p2 fixtures, then runs the Go API
# and the three SvelteKit dev servers on the host. Ctrl-C stops the host
# processes; compose deps stay running (use `make dev-down` to stop them).
set -euo pipefail

cd "$(dirname "$0")/../.."

COMPOSE="docker compose -f ops/local/docker-compose.dev.yml"
DB_DSN="postgres://tbite:tbite@localhost:5432/tbite?sslmode=disable"

echo "==> deps"
$COMPOSE up -d

echo "==> waiting for postgres"
ready=
for i in {1..40}; do
  if $COMPOSE exec -T postgres pg_isready -U tbite -d tbite >/dev/null 2>&1; then
    ready=1; break
  fi
  sleep 0.5
done
if [ -z "$ready" ]; then
  echo "postgres did not become ready in 20s — check 'make dev-logs svc=postgres'" >&2
  exit 1
fi

echo "==> waiting for authentik"
authentik_ready=
for i in {1..90}; do
  if curl -fsS http://localhost:9002/application/o/tbite/.well-known/openid-configuration >/dev/null 2>&1; then
    authentik_ready=1; break
  fi
  sleep 1
done
if [ -z "$authentik_ready" ]; then
  echo "authentik OIDC app was not ready in 90s — check 'make dev-logs svc=authentik-worker'" >&2
  exit 1
fi

echo "==> waiting for hydra"
hydra_ready=
for i in {1..40}; do
  if curl -fsS http://localhost:4444/health/ready >/dev/null 2>&1; then
    hydra_ready=1; break
  fi
  sleep 0.5
done
if [ -z "$hydra_ready" ]; then
  echo "hydra did not become ready in 20s — check 'make dev-logs svc=hydra'" >&2
  exit 1
fi

echo "==> migrate"
DATABASE_URL="$DB_DSN" scripts/db/migrate.sh up

echo "==> seed"
$COMPOSE exec -T postgres psql -U tbite -d tbite < scripts/dev/seed-p2.sql >/dev/null

cat <<EOF

==> ready
   employee   http://localhost:5173
   merchant   http://localhost:5174
   admin      http://localhost:5175
   api        http://localhost:8080/healthz
   authentik  http://localhost:9002 (akadmin / tbite-dev-admin)
   minio      http://localhost:9001 (tbite / tbite-dev-secret)

Ctrl-C stops host processes. Deps stay up — 'make dev-down' to stop them.

EOF

trap 'kill 0' EXIT

export DATABASE_RW_URL="$DB_DSN"
export REDIS_URL="redis://localhost:6379"
export NATS_URL="nats://localhost:4222"
export S3_ENDPOINT="http://localhost:9000"
export S3_REGION="us-east-1"
export S3_ACCESS_KEY_ID="tbite"
export S3_SECRET_ACCESS_KEY="tbite-dev-secret"
export S3_BUCKET="tbite"
export S3_USE_PATH_STYLE=1

# Local Authentik OIDC provider. The callback URL must match
# ops/local/authentik/blueprints/tbite.yaml exactly.
export OIDC_CALLBACK_BASE_URL="http://localhost:8080"
export AUTH_PROVIDER_SLUGS="authentik"
export AUTH_PROVIDER_AUTHENTIK_DISPLAY_NAME="Authentik"
export AUTH_PROVIDER_AUTHENTIK_ISSUER_URL="http://localhost:9002/application/o/tbite/"
export AUTH_PROVIDER_AUTHENTIK_CLIENT_ID="tbite-local"
export AUTH_PROVIDER_AUTHENTIK_CLIENT_SECRET="tbite-local-client-secret"
export AUTH_PROVIDER_AUTHENTIK_SCOPES="openid email profile tbite"
export AUTHENTIK_BASE_URL="http://localhost:9002"
export AUTHENTIK_API_TOKEN="tbite-dev-authentik-api-token"
export AUTHENTIK_VENDOR_OPERATOR_GROUP="tbite:role:vendor_operator"
export APP_BASE_URL_EMPLOYEE="http://localhost:5173"
export APP_BASE_URL_MERCHANT="http://localhost:5174"
export APP_BASE_URL_ADMIN="http://localhost:5175"

# Ory Hydra sidecar — OAuth + DCR for MCP remote clients.
export HYDRA_PUBLIC_URL="http://localhost:4444"
export HYDRA_ADMIN_URL="http://localhost:4445"

go run ./services/api/cmd/tbite --role=api &
pnpm --filter @tbite/employee dev &
pnpm --filter @tbite/merchant dev &
pnpm --filter @tbite/admin dev &
wait
