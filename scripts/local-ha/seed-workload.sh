#!/usr/bin/env bash
set -euo pipefail

cd "$(dirname "$0")/../.."

namespace="${NAMESPACE:-tbite}"
cluster="${CNPG_CLUSTER:-tbite-pg}"
database="${DATABASE_NAME:-tbite}"
asset_base="${LOCAL_HA_ASSET_BASE_URL:-http://minio.tbite.local/tbite-dev}"
seed_scale="${SEED_SCALE:-false}"

primary="$(kubectl -n "${namespace}" get cluster "${cluster}" -o jsonpath='{.status.currentPrimary}')"
if [[ -z "${primary}" ]]; then
  echo "CNPG cluster ${namespace}/${cluster} does not report a current primary." >&2
  exit 1
fi

kubectl -n "${namespace}" wait --for=condition=Ready "pod/${primary}" --timeout=2m >/dev/null

seeds=(
  scripts/dev/seed-p2.sql
  scripts/dev/seed-demo.sql
  scripts/dev/seed-tsmc.sql
)
if [[ "${seed_scale}" == "true" ]]; then
  seeds+=(scripts/dev/seed-tsmc-scale.sql)
fi

apply_seed() {
  local seed="$1"
  echo "==> seeding ${seed}"
  if grep -q '__ASSET_BASE__' "${seed}"; then
    sed "s|__ASSET_BASE__|${asset_base}|g" "${seed}" \
      | kubectl -n "${namespace}" exec -i "${primary}" -c postgres -- \
          psql -q -d "${database}" -v ON_ERROR_STOP=1
  else
    kubectl -n "${namespace}" exec -i "${primary}" -c postgres -- \
      psql -q -d "${database}" -v ON_ERROR_STOP=1 <"${seed}"
  fi
}

for seed in "${seeds[@]}"; do
  apply_seed "${seed}"
done

echo "==> ensuring future meal supply covers stress pickFutureDate()"
kubectl -n "${namespace}" exec -i "${primary}" -c postgres -- \
  psql -q -d "${database}" -v ON_ERROR_STOP=1 <<'SQL'
INSERT INTO meal_supply (menu_item_id, supply_date, capacity, remain, pickup_window, eta_label, cutoff_at)
SELECT
  mi.id,
  d::date AS supply_date,
  80 AS capacity,
  80 AS remain,
  '11:50-12:10' AS pickup_window,
  '11:50-12:10' AS eta_label,
  (d::date + INTERVAL '17 hours')::timestamptz AS cutoff_at
FROM menu_item mi
CROSS JOIN generate_series(CURRENT_DATE, CURRENT_DATE + INTERVAL '7 days', INTERVAL '1 day') AS d
WHERE mi.status = 'active'
ON CONFLICT (menu_item_id, supply_date) DO NOTHING;
SQL

echo "==> verifying workload seed"
kubectl -n "${namespace}" exec -i "${primary}" -c postgres -- \
  psql -d "${database}" -v ON_ERROR_STOP=1 <<'SQL'
SELECT 'approved_vendors' AS metric, count(*) FROM vendor WHERE status = 'approved'
UNION ALL
SELECT 'active_menu_items', count(*) FROM menu_item WHERE status = 'active'
UNION ALL
SELECT 'active_tsmc_mappings', count(*) FROM vendor_plant_mapping
 WHERE active AND plant IN ('hc-12a-1f','hc-12a-3f','hc-12b-1f','tc-15a-1f','tn-18p1-1f','tn-18p3-1f','tn-18p7-2f')
UNION ALL
SELECT 'future_supply_rows', count(*) FROM meal_supply
 WHERE supply_date BETWEEN CURRENT_DATE + INTERVAL '1 day' AND CURRENT_DATE + INTERVAL '7 days'
ORDER BY metric;
SQL
