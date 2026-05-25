#!/usr/bin/env bash
set -euo pipefail
# Apply the dev seeds (seed-p2.sql then seed-demo.sql) to the database.
# Before applying, uploads static brand assets to MinIO and substitutes
# __ASSET_BASE__ placeholders with the public MinIO URL.
# Uses local psql if available, else psql inside the dev postgres container.
DSN="${DATABASE_URL:-postgres://tbite:tbite@localhost:5432/tbite?sslmode=disable}"
DEV_COMPOSE="docker compose -f ops/local/docker-compose.dev.yml"
SEED_DIR="scripts/dev"

# S3 config (defaults match the local docker-compose dev stack).
S3_PUBLIC_BASE_URL="${S3_PUBLIC_BASE_URL:-http://127.0.0.1:9000}"
S3_BUCKET="${S3_BUCKET:-tbite}"

# Step 1: upload brand assets to MinIO (idempotent).
echo "==> uploading brand assets"
bash scripts/db/seed-assets.sh

# Step 2: apply SQL with __ASSET_BASE__ substituted.
BASE="${S3_PUBLIC_BASE_URL}/${S3_BUCKET}"

if command -v psql >/dev/null 2>&1; then
  run() {
    local f="$1"
    if grep -q '__ASSET_BASE__' "$f" 2>/dev/null; then
      sed "s|__ASSET_BASE__|${BASE}|g" "$f" | psql "$DSN" -v ON_ERROR_STOP=1
    else
      psql "$DSN" -v ON_ERROR_STOP=1 -f "$f"
    fi
  }
else
  run() {
    local f="$1"
    if grep -q '__ASSET_BASE__' "$f" 2>/dev/null; then
      sed "s|__ASSET_BASE__|${BASE}|g" "$f" | $DEV_COMPOSE exec -T postgres psql -U tbite -d tbite -v ON_ERROR_STOP=1
    else
      $DEV_COMPOSE exec -T postgres psql -U tbite -d tbite -v ON_ERROR_STOP=1 < "$f"
    fi
  }
fi

for f in seed-p2.sql seed-demo.sql; do
  echo "==> seeding $f"
  run "$SEED_DIR/$f"
done
echo "==> seed complete"
