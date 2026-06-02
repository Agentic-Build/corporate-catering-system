#!/usr/bin/env bash
set -euo pipefail

# One-shot seed for the COMPLETE TSMC demo on a Kubernetes deployment.
#
# Does everything needed for a fresh, fully-populated prod demo:
#   1. Uploads brand assets (apps/employee/static/brand) to MinIO so menu /
#      vendor / category images resolve via S3_PUBLIC_BASE_URL.
#   2. Applies the SQL seeds in order with __ASSET_BASE__ rewritten to the
#      public MinIO base ($S3_PUBLIC_BASE_URL/$S3_BUCKET):
#        seed-p2   (catalog: 4 categories, 10 vendors, 150 items + images, supply)
#        seed-demo (demo employees + a handful of recent orders)
#        seed-tsmc (real TSMC pickup locations -> vendor_plant_mapping)
#        seed-tsmc-scale (50k synthetic employees + demo-week supply capacity)
#   3. Handles the prod re-seed hazards around seed-p2:
#        - RESTRICT FKs from payroll_dispute / meal_rating / meal_complaint on
#          the 3 demo orders (deleted first).
#        - the order_state_event append-only triggers (disabled around seed-p2,
#          re-enabled on exit via a trap).
#
# Idempotent: safe to re-run. Run from the repo root with kubectl access to the
# target cluster. Config (S3 endpoint/bucket/public-base) is read from the
# cluster, so there is nothing to hand-configure.
#
# Optional env: NS, DB_SECRET, DB_SECRET_KEY, S3_SECRET, APP_ENV_CM,
#               PSQL_IMAGE, UPLOAD_IMAGE.

NS="${NS:-tbite}"
DB_SECRET="${DB_SECRET:-tbite-db}"
DB_SECRET_KEY="${DB_SECRET_KEY:-rwUrl}"
S3_SECRET="${S3_SECRET:-tbite-s3}"
APP_ENV_CM="${APP_ENV_CM:-tbite-app-env}"
PSQL_IMAGE="${PSQL_IMAGE:-ghcr.io/cloudnative-pg/postgresql:17.2}"
UPLOAD_IMAGE="${UPLOAD_IMAGE:-alpine:3.20}"

ASSET_SRC="apps/employee/static/brand"
SEEDS=(
  scripts/dev/seed-p2.sql
  scripts/dev/seed-demo.sql
  scripts/dev/seed-tsmc.sql
  scripts/dev/seed-tsmc-scale.sql
)
DEMO_ORDER_IDS="'d0000000-0000-0000-0000-000000000001','d0000000-0000-0000-0000-000000000002','d0000000-0000-0000-0000-000000000003'"

for f in "${SEEDS[@]}"; do
  [ -f "$f" ] || { echo "missing seed file: $f (run from repo root)" >&2; exit 1; }
done
[ -d "$ASSET_SRC" ] || { echo "missing asset dir: $ASSET_SRC (run from repo root)" >&2; exit 1; }

kv() { kubectl -n "$NS" get "$1" "$2" -o "jsonpath={$3}"; }

DATABASE_RW_URL="$(kv secret "$DB_SECRET" ".data.${DB_SECRET_KEY}" | base64 -d)"
S3_ENDPOINT="$(kv configmap "$APP_ENV_CM" ".data.S3_ENDPOINT")"
S3_BUCKET="$(kv configmap "$APP_ENV_CM" ".data.S3_BUCKET")"
S3_PUBLIC_BASE_URL="$(kv configmap "$APP_ENV_CM" ".data.S3_PUBLIC_BASE_URL")"
S3_KEY="$(kv secret "$S3_SECRET" ".data.accessKeyID" | base64 -d)"
S3_SECRET_VAL="$(kv secret "$S3_SECRET" ".data.secretAccessKey" | base64 -d)"

[ -n "$DATABASE_RW_URL" ] || { echo "empty DB url from ${NS}/${DB_SECRET}:${DB_SECRET_KEY}" >&2; exit 1; }
[ -n "$S3_BUCKET" ] && [ -n "$S3_PUBLIC_BASE_URL" ] || { echo "missing S3 config in ${NS}/${APP_ENV_CM}" >&2; exit 1; }

ASSET_BASE="${S3_PUBLIC_BASE_URL}/${S3_BUCKET}"
echo "==> target: ns=$NS  bucket=$S3_BUCKET  asset_base=$ASSET_BASE"

# --- 1. Upload brand assets to MinIO -----------------------------------------
# Stream a tar of the brand tree into a throwaway pod (busybox tar), fetch the
# mc client there, and mirror into the bucket's brand/ prefix. --remove makes
# the destination match the source exactly (clears any stale objects).
# COPYFILE_DISABLE keeps macOS AppleDouble (._*) files out of the tarball.
echo "==> uploading brand assets to MinIO"
COPYFILE_DISABLE=1 tar -C "$(dirname "$ASSET_SRC")" -cf - "$(basename "$ASSET_SRC")" \
  | kubectl -n "$NS" run "tbite-asset-upload-$(date +%s)" \
      --rm -i --restart=Never --image="$UPLOAD_IMAGE" \
      --env="EP=$S3_ENDPOINT" --env="K=$S3_KEY" --env="S=$S3_SECRET_VAL" --env="B=$S3_BUCKET" \
      --command -- sh -ec '
        mkdir -p /tmp/up && cd /tmp/up && tar xf -
        wget -qO /usr/bin/mc https://dl.min.io/client/mc/release/linux-amd64/mc
        chmod +x /usr/bin/mc
        mc alias set t "$EP" "$K" "$S" --quiet
        mc mb --ignore-existing "t/$B"
        mc mirror --overwrite --remove /tmp/up/brand "t/$B/brand"
        mc anonymous set download "t/$B/brand"
        mc anonymous set download "t/$B/menu-images" 2>/dev/null || true
      '

# --- 2/3. Apply SQL seeds with traps handled ---------------------------------
psql_stdin() { # reads SQL from stdin, applies via a throwaway psql pod
  kubectl -n "$NS" run "tbite-seed-$(date +%s)-$RANDOM" \
    --rm -i --restart=Never --image="$PSQL_IMAGE" \
    --env="U=$DATABASE_RW_URL" \
    --command -- sh -ec 'psql "$U" -v ON_ERROR_STOP=1'
}
toggle_triggers() { # $1 = ENABLE|DISABLE
  printf 'ALTER TABLE order_state_event %s TRIGGER order_state_event_no_delete;\n' "$1"
  printf 'ALTER TABLE order_state_event %s TRIGGER order_state_event_no_update;\n' "$1"
}

# Guarantee the append-only triggers are restored no matter how we exit.
restore_triggers() { toggle_triggers ENABLE | psql_stdin >/dev/null 2>&1 || true; }
trap restore_triggers EXIT

for seed in "${SEEDS[@]}"; do
  echo "==> applying $seed"
  if [ "$seed" = "scripts/dev/seed-p2.sql" ]; then
    # Preamble: clear RESTRICT deps on the demo orders + disable append-only
    # triggers so seed-p2's authoritative cleanup can delete/cascade.
    {
      echo "DELETE FROM payroll_dispute WHERE order_id IN ($DEMO_ORDER_IDS);"
      echo "DELETE FROM meal_rating     WHERE order_id IN ($DEMO_ORDER_IDS);"
      echo "DELETE FROM meal_complaint  WHERE order_id IN ($DEMO_ORDER_IDS);"
      toggle_triggers DISABLE
      sed "s|__ASSET_BASE__|${ASSET_BASE}|g" "$seed"
      toggle_triggers ENABLE
    } | psql_stdin
  else
    sed "s|__ASSET_BASE__|${ASSET_BASE}|g" "$seed" | psql_stdin
  fi
done

echo "==> done. catalog images now served from ${ASSET_BASE}/brand/"
