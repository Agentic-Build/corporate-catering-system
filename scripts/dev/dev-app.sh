#!/usr/bin/env bash
set -euo pipefail
trap 'kill 0' EXIT

# Local dev: run Go API + three SvelteKit dev servers in parallel.
# Each owns its own port (8080 / 5173 / 5174 / 5175). Ctrl-C kills the group.

cd "$(dirname "$0")/../.."

( cd . && go run ./services/api/cmd/tbite --role=api ) &
pnpm --filter @tbite/employee dev &
pnpm --filter @tbite/merchant dev &
pnpm --filter @tbite/admin dev &
wait
