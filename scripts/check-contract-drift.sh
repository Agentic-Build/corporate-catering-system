#!/usr/bin/env bash
set -euo pipefail

status="$(git status --porcelain --untracked-files=all -- contract/openapi contract/generated/ts-client)"
if [[ -n "${status}" ]]; then
  echo "Contract artifacts drift detected in tracked OpenAPI or generated client outputs."
  echo "Run 'pnpm run contract:sync' and commit all resulting changes under contract/."
  echo
  printf '%s\n' "${status}"
  exit 1
fi
