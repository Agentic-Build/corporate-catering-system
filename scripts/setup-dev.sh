#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
COMPOSE_FILE="${ROOT_DIR}/ops/local/docker-compose.dev.yml"
ENV_DEVELOPMENT_FILE="${ROOT_DIR}/.env.development"
ENV_LOCAL_FILE="${ROOT_DIR}/.env.local"

usage() {
  cat <<'USAGE'
Usage: scripts/setup-dev.sh <command>

Commands:
  dev      Start local dependencies and run the runtime service in foreground
  up       Start local dependencies only (detached)
  app      Run the runtime service in foreground
  down     Stop local dependencies
  reset    Stop dependencies, remove volumes, and clear runtime state
  logs     Tail dependency logs (optional service name as second arg)
  ps       Show dependency container status
USAGE
}

require_command() {
  local cmd="$1"
  if ! command -v "${cmd}" >/dev/null 2>&1; then
    echo "missing required command: ${cmd}" >&2
    exit 1
  fi
}

ensure_prerequisites() {
  require_command docker
  require_command cargo
  if ! docker compose version >/dev/null 2>&1; then
    echo "docker compose v2 is required" >&2
    exit 1
  fi
}

ensure_env_files() {
  if [[ ! -f "${ENV_DEVELOPMENT_FILE}" ]]; then
    echo "missing required env baseline: ${ENV_DEVELOPMENT_FILE}" >&2
    exit 1
  fi
  if [[ ! -f "${ENV_LOCAL_FILE}" ]]; then
    cat >"${ENV_LOCAL_FILE}" <<'LOCAL'
# Local-only overrides for development.
# This file is ignored by git.
LOCAL
  fi
}

load_runtime_env() {
  # shellcheck disable=SC1090
  set -a
  source "${ENV_DEVELOPMENT_FILE}"
  # shellcheck disable=SC1090
  source "${ENV_LOCAL_FILE}"
  set +a
}

ensure_state_dirs() {
  mkdir -p "${ROOT_DIR}/ops/state"
}

run_database_migrations() {
  require_command sqlx
  if [[ -z "${DATABASE_URL:-}" ]]; then
    echo "missing required env: DATABASE_URL" >&2
    exit 1
  fi
  (
    cd "${ROOT_DIR}"
    DATABASE_URL="${DATABASE_URL}" sqlx migrate run --source migrations
  )
}

runtime_state_file() {
  local configured="${PRELAUNCH_AUDIT_TRAIL_PATH}"
  if [[ "${configured}" = /* ]]; then
    printf '%s\n' "${configured}"
  else
    printf '%s\n' "${ROOT_DIR}/${configured}"
  fi
}

reset_runtime_state_file() {
  local state_file
  state_file="$(runtime_state_file)"
  rm -f "${state_file}"
}

compose() {
  docker compose \
    --project-name "${DEV_STACK_PROJECT_NAME}" \
    --env-file "${ENV_DEVELOPMENT_FILE}" \
    --env-file "${ENV_LOCAL_FILE}" \
    -f "${COMPOSE_FILE}" \
    "$@"
}

command="${1:-dev}"
service_name="${2:-}"

ensure_prerequisites
ensure_env_files
load_runtime_env
ensure_state_dirs

case "${command}" in
  dev)
    compose up -d --wait
    run_database_migrations
    reset_runtime_state_file
    exec cargo run --bin observability_runtime_service
    ;;
  up)
    compose up -d --wait
    ;;
  app)
    run_database_migrations
    exec cargo run --bin observability_runtime_service
    ;;
  down)
    compose down --remove-orphans
    ;;
  reset)
    compose down --remove-orphans --volumes
    reset_runtime_state_file
    ;;
  logs)
    if [[ -n "${service_name}" ]]; then
      compose logs -f "${service_name}"
    else
      compose logs -f
    fi
    ;;
  ps)
    compose ps
    ;;
  *)
    usage
    exit 1
    ;;
esac
