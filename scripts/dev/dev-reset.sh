#!/usr/bin/env bash
set -euo pipefail
SCRIPT_DIR="$(dirname "$0")"
"${SCRIPT_DIR}/dev-down.sh" || true
"${SCRIPT_DIR}/dev-up.sh"
