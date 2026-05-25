#!/usr/bin/env bash
set -euo pipefail
# Upload static brand assets to MinIO under the brand/ prefix.
# Idempotent: mc mirror skips files that already exist at the destination.
#
# Reads S3 config from environment; defaults match the local docker-compose dev stack:
#   S3_ENDPOINT=http://127.0.0.1:9000
#   S3_ACCESS_KEY_ID=tbite
#   S3_SECRET_ACCESS_KEY=tbite-dev-secret
#   S3_BUCKET=tbite
#   S3_PUBLIC_BASE_URL=http://127.0.0.1:9000

S3_ENDPOINT="${S3_ENDPOINT:-http://127.0.0.1:9000}"
S3_ACCESS_KEY_ID="${S3_ACCESS_KEY_ID:-tbite}"
S3_SECRET_ACCESS_KEY="${S3_SECRET_ACCESS_KEY:-tbite-dev-secret}"
S3_BUCKET="${S3_BUCKET:-tbite}"

ASSET_SRC="apps/employee/static/brand"

if ! command -v mc >/dev/null 2>&1; then
  echo "mc (MinIO Client) not found; skipping asset upload." >&2
  echo "Install mc and re-run, or upload apps/employee/static/brand/ to ${S3_BUCKET}/brand/ manually." >&2
  exit 0
fi

# Register alias (idempotent).
export HOME="${HOME:-/tmp}"
mc alias set tbite-seed "${S3_ENDPOINT}" "${S3_ACCESS_KEY_ID}" "${S3_SECRET_ACCESS_KEY}" --quiet

# Ensure bucket exists.
mc mb --ignore-existing "tbite-seed/${S3_BUCKET}" || true

# Mirror brand assets into brand/ prefix.
mc mirror --overwrite "${ASSET_SRC}" "tbite-seed/${S3_BUCKET}/brand"

# Expose only the image asset prefixes for anonymous browser reads. The bucket
# itself stays private so vendor-docs/* (compliance documents) are not exposed.
mc anonymous set download "tbite-seed/${S3_BUCKET}/brand" || true
mc anonymous set download "tbite-seed/${S3_BUCKET}/menu-images" || true

echo "==> brand assets uploaded to ${S3_ENDPOINT}/${S3_BUCKET}/brand/"
