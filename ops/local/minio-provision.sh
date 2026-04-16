#!/usr/bin/env bash
set -euo pipefail

: "${DEV_STACK_PROJECT_NAME:?missing DEV_STACK_PROJECT_NAME}"
: "${MINIO_ROOT_USER:?missing MINIO_ROOT_USER}"
: "${MINIO_ROOT_PASSWORD:?missing MINIO_ROOT_PASSWORD}"

menu_bucket="${MINIO_BUCKET_MENU_IMAGES:-menu-assets}"
compliance_bucket="${MINIO_BUCKET_COMPLIANCE_EVIDENCE:-compliance-evidence}"
fulfillment_bucket="${MINIO_BUCKET_FULFILLMENT_EXPORTS:-fulfillment-exports}"
network_name="${DEV_STACK_PROJECT_NAME}_default"

run_mc() {
  docker run --rm \
    --network "${network_name}" \
    -e "MC_HOST_local=http://${MINIO_ROOT_USER}:${MINIO_ROOT_PASSWORD}@minio:9000" \
    minio/mc "$@"
}

for attempt in $(seq 1 30); do
  if run_mc ls local >/dev/null 2>&1; then
    break
  fi
  if [[ "${attempt}" -eq 30 ]]; then
    echo "minio provisioning failed: MinIO API is not reachable after waiting" >&2
    exit 1
  fi
  sleep 1
done

for bucket in "${menu_bucket}" "${compliance_bucket}" "${fulfillment_bucket}"; do
  run_mc mb --ignore-existing "local/${bucket}" >/dev/null
  run_mc anonymous set none "local/${bucket}" >/dev/null
done
