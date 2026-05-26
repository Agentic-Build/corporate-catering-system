#!/usr/bin/env bash
set -euo pipefail
# Upload static brand assets to MinIO under the brand/ prefix.
# Idempotent: mc mirror skips files that already exist at the destination.
#
# Reads S3 config from environment. For local Kubernetes, point these at
# the chart-managed MinIO endpoint or a port-forwarded MinIO service.

: "${S3_ENDPOINT:?S3_ENDPOINT is required}"
: "${S3_ACCESS_KEY_ID:?S3_ACCESS_KEY_ID is required}"
: "${S3_SECRET_ACCESS_KEY:?S3_SECRET_ACCESS_KEY is required}"
: "${S3_BUCKET:?S3_BUCKET is required}"

ASSET_SRC="apps/employee/static/brand"

if ! command -v mc >/dev/null 2>&1; then
  echo "mc (MinIO Client) is required to seed brand assets." >&2
  exit 1
fi

# Register alias (idempotent).
export HOME="${HOME:-/tmp}"
mc alias set tbite-seed "${S3_ENDPOINT}" "${S3_ACCESS_KEY_ID}" "${S3_SECRET_ACCESS_KEY}" --quiet

# Ensure bucket exists.
mc mb --ignore-existing "tbite-seed/${S3_BUCKET}"

# Mirror brand assets into brand/ prefix.
mc mirror --overwrite "${ASSET_SRC}" "tbite-seed/${S3_BUCKET}/brand"

# Expose only the image asset prefixes for anonymous browser reads. The bucket
# itself stays private so vendor-docs/* (compliance documents) are not exposed.
mc anonymous set download "tbite-seed/${S3_BUCKET}/brand"
mc anonymous set download "tbite-seed/${S3_BUCKET}/menu-images"

echo "==> brand assets uploaded to ${S3_ENDPOINT}/${S3_BUCKET}/brand/"
