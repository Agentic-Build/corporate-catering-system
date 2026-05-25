#!/usr/bin/env bash
set -euo pipefail

# Seed a Kubernetes deployment with the complete TSMC enterprise demo data.
#
# Required:
#   kubectl access to the target cluster
#   tbite-db Secret in the target namespace with key rwUrl
#
# Optional env:
#   NS=tbite
#   DB_SECRET=tbite-db
#   DB_SECRET_KEY=rwUrl
#   PSQL_IMAGE=ghcr.io/cloudnative-pg/postgresql:17.2

NS="${NS:-tbite}"
DB_SECRET="${DB_SECRET:-tbite-db}"
DB_SECRET_KEY="${DB_SECRET_KEY:-rwUrl}"
PSQL_IMAGE="${PSQL_IMAGE:-ghcr.io/cloudnative-pg/postgresql:17.2}"

SEEDS=(
  scripts/dev/seed-p2.sql
  scripts/dev/seed-demo.sql
  scripts/dev/seed-tsmc.sql
  scripts/dev/seed-tsmc-scale.sql
)

for seed in "${SEEDS[@]}"; do
  if [ ! -f "$seed" ]; then
    echo "missing seed file: $seed" >&2
    exit 1
  fi
done

DATABASE_RW_URL="$(
  kubectl -n "$NS" get secret "$DB_SECRET" \
    -o "jsonpath={.data.${DB_SECRET_KEY}}" | base64 -d
)"

if [ -z "$DATABASE_RW_URL" ]; then
  echo "empty database URL from secret ${NS}/${DB_SECRET}:${DB_SECRET_KEY}" >&2
  exit 1
fi

run_seed() {
  local seed="$1"
  local name
  name="tbite-seed-$(basename "$seed" .sql | tr -cd 'a-z0-9-')-$(date +%s)"
  echo "==> applying $seed"
  kubectl -n "$NS" run "$name" \
    --rm -i \
    --restart=Never \
    --image="$PSQL_IMAGE" \
    --env="DATABASE_RW_URL=$DATABASE_RW_URL" \
    --command -- sh -ec 'psql "$DATABASE_RW_URL" -v ON_ERROR_STOP=1' < "$seed"
}

for seed in "${SEEDS[@]}"; do
  run_seed "$seed"
done

echo "==> verifying TSMC enterprise seed"
kubectl -n "$NS" run "tbite-seed-verify-$(date +%s)" \
  --rm -i \
  --restart=Never \
  --image="$PSQL_IMAGE" \
  --env="DATABASE_RW_URL=$DATABASE_RW_URL" \
  --command -- sh -ec 'psql "$DATABASE_RW_URL" -v ON_ERROR_STOP=1' <<'SQL'
SELECT plant, count(*) AS employees
FROM "user"
WHERE primary_email ~ '^tsmc[0-9]{5}@tbite\.test$'
GROUP BY plant
ORDER BY plant;

SELECT count(*) AS employees
FROM "user"
WHERE primary_email ~ '^tsmc[0-9]{5}@tbite\.test$';
SQL

echo "==> TSMC enterprise seed complete"
