#!/usr/bin/env bash
set -euo pipefail

# Apply the complete local TSMC demo seed to the configured database.
# Requires an explicit DATABASE_URL; local Kubernetes users can port-forward
# Postgres or run this from a host that can reach the chart-managed database.

: "${DATABASE_URL:?DATABASE_URL is required}"
: "${S3_PUBLIC_BASE_URL:?S3_PUBLIC_BASE_URL is required}"
: "${S3_BUCKET:?S3_BUCKET is required}"

SEED_DIR="scripts/dev"
SEEDS=(
  seed-p2.sql
  seed-demo.sql
  seed-tsmc.sql
  seed-tsmc-scale.sql
)

if ! command -v psql >/dev/null 2>&1; then
  echo "psql is required to apply seed SQL." >&2
  exit 1
fi

echo "==> uploading brand assets"
bash scripts/db/seed-assets.sh

BASE="${S3_PUBLIC_BASE_URL}/${S3_BUCKET}"

run() {
  local f="$1"
  if grep -q '__ASSET_BASE__' "$f" 2>/dev/null; then
    sed "s|__ASSET_BASE__|${BASE}|g" "$f" | psql "$DATABASE_URL" -v ON_ERROR_STOP=1
  else
    psql "$DATABASE_URL" -v ON_ERROR_STOP=1 -f "$f"
  fi
}

for seed in "${SEEDS[@]}"; do
  echo "==> seeding $seed"
  run "$SEED_DIR/$seed"
done

echo "==> TSMC enterprise seed complete"
