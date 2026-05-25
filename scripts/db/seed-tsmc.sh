#!/usr/bin/env bash
set -euo pipefail

# Apply the complete local TSMC demo seed to the configured database.
# Uses local psql if available, else psql inside the dev postgres container.

DSN="${DATABASE_URL:-postgres://tbite:tbite@localhost:5432/tbite?sslmode=disable}"
DEV_COMPOSE="docker compose -f ops/local/docker-compose.dev.yml"
SEED_DIR="scripts/dev"
SEEDS=(
  seed-p2.sql
  seed-demo.sql
  seed-tsmc.sql
  seed-tsmc-scale.sql
)

if command -v psql >/dev/null 2>&1; then
  run() { psql "$DSN" -v ON_ERROR_STOP=1 -f "$1"; }
else
  run() { $DEV_COMPOSE exec -T postgres psql -U tbite -d tbite -v ON_ERROR_STOP=1 < "$1"; }
fi

for seed in "${SEEDS[@]}"; do
  echo "==> seeding $seed"
  run "$SEED_DIR/$seed"
done

echo "==> TSMC enterprise seed complete"
