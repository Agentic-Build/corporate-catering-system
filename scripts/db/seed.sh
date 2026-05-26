#!/usr/bin/env bash
set -euo pipefail
# Apply the dev seeds (seed-p2.sql then seed-demo.sql) to the database.
# Before applying, uploads static brand assets to MinIO and substitutes
# __ASSET_BASE__ placeholders with the public MinIO URL.
# Requires an explicit DATABASE_URL; local Kubernetes users can port-forward
# Postgres or run this from a host that can reach the chart-managed database.
: "${DATABASE_URL:?DATABASE_URL is required}"
: "${S3_PUBLIC_BASE_URL:?S3_PUBLIC_BASE_URL is required}"
: "${S3_BUCKET:?S3_BUCKET is required}"

SEED_DIR="scripts/dev"

if ! command -v psql >/dev/null 2>&1; then
  echo "psql is required to apply seed SQL." >&2
  exit 1
fi

# Step 1: upload brand assets to MinIO (idempotent).
echo "==> uploading brand assets"
bash scripts/db/seed-assets.sh

# Step 2: apply SQL with __ASSET_BASE__ substituted.
BASE="${S3_PUBLIC_BASE_URL}/${S3_BUCKET}"

run() {
  local f="$1"
  if grep -q '__ASSET_BASE__' "$f" 2>/dev/null; then
    sed "s|__ASSET_BASE__|${BASE}|g" "$f" | psql "$DATABASE_URL" -v ON_ERROR_STOP=1
  else
    psql "$DATABASE_URL" -v ON_ERROR_STOP=1 -f "$f"
  fi
}

for f in seed-p2.sql seed-demo.sql; do
  echo "==> seeding $f"
  run "$SEED_DIR/$f"
done
echo "==> seed complete"
