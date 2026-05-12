#!/usr/bin/env bash
set -euo pipefail
DB_URL="${DATABASE_URL:-postgres://tbite:tbite@localhost:5432/tbite?sslmode=disable}"
docker run --rm --network host -v "$(pwd)/migrations:/migrations" \
  migrate/migrate:v4.18.1 -path=/migrations -database "$DB_URL" "$@"
