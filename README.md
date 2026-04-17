# corporate-catering-system

## Local Dev Stack and Seed Baseline

Canonical local development entrypoints:

- `make dev` starts core stateful dependencies and runs the runtime service.
- `make dev-up` starts dependencies only.
- `make dev-app` runs the runtime service only.
- `make dev-down` stops dependencies.
- `make dev-reset` tears down dependencies, removes dependency volumes, and clears runtime state.
- `make dev-logs service=<name>` tails dependency logs (`service` is optional).
- `make dev-ps` shows dependency status.

Environment baseline:

- `.env.development` is committed and is the default local baseline.
- `.env.local` is local-only (gitignored) and overrides `.env.development`.

Compose local stack (`ops/local/docker-compose.dev.yml`) includes core stateful dependencies:

- `postgres`
- `redis` (Valkey-compatible cache backend)
- `nats` (JetStream event backbone)
- `minio`
- `otel-collector`
- all service images are pinned to immutable digests for deterministic startup

Runtime bootstrap seeds deterministic scenarios for:

- lifecycle/compliance: approved baseline vendor plus lifecycle reminder/suspension/reinstatement transitions
- dispute: seeded payroll dispute opened, assigned, and refund-resolved
- anomaly: seeded anomaly alert triggered, triaged, and closed with evidence
- mapping: seeded delivery allow + deny mapping rules

## PostgreSQL Schema and Migration Lifecycle

Database schema is managed only via `sqlx migrate` migrations under `migrations/`.

- Global PK standard: `UUID` (`global_pk`) on every table primary key.
- Monetary values: `BIGINT` minor units (`money_minor` domain), never float/double.
- State enums: PostgreSQL `ENUM` types.
- Append-only protections: trigger-enforced guards on `audit_event` and `payroll_ledger_entry` for `UPDATE`/`DELETE`/`TRUNCATE`.

Canonical commands:

- `pnpm run db:migrate` applies pending migrations.
- `pnpm run db:migrate:revert` reverts the latest migration.
- `pnpm run db:migrate:verify` runs clean-database up/down validation and invariant checks.

Default startup behavior:

- `make dev` and `make dev-app` automatically run `sqlx migrate run` before starting the runtime service.

CI gate:

- `.github/workflows/postgresql-migration-foundation.yml` runs the real-PostgreSQL migration up/down and invariant verifier on pull requests and `main`.

## OpenAPI Contract Platform

Canonical HTTP contract artifacts are generated from the Rust contract module and committed under `contract/`.

- Sync committed spec/docs + generated TS client: `pnpm run contract:sync`
- Output artifacts:
  - `contract/openapi/openapi.json` (machine-readable)
  - `contract/openapi/openapi.yaml` (machine-readable)
  - `contract/openapi/index.html` (browsable Redoc docs)
  - `contract/generated/ts-client/**` (OpenAPI-generated TypeScript client/types)
- Generate and type-check TS client/types from OpenAPI:
  - `pnpm run contract:verify`
  - This command fails if regeneration changes committed contract artifacts.

CI (`.github/workflows/openapi-contract.yml`) enforces:
- runtime HTTP route parity vs OpenAPI contract
- generated artifact drift detection
- MCP contract parity checks (auto-activated once runtime MCP tools are declared)

## Observability and Kubernetes SLO Baseline

Baseline artifacts are committed and release-gated under `ops/`:

- OpenTelemetry + Victoria stack wiring:
  - `ops/observability/otel/collector.yaml`
  - `ops/observability/otel/instrumentation-baseline.yaml`
  - Runtime instrumentation hooks are implemented in `src/observability.rs` and wired into HTTP/MCP/compliance execution paths, including OTLP traces, metrics, and logs export bootstrap.
- Hard-SLO policy, dashboard, and alerts:
  - `ops/observability/slo/hard-slo-policy.yaml`
  - `ops/observability/slo/grafana-dashboard-hard-slo.json`
  - `ops/observability/slo/alerts.yaml`
- Pre-launch load-test thresholds:
  - `ops/observability/load/prelaunch-thresholds.yaml`
  - `ops/observability/load/k6-prelaunch.js`
  - `src/bin/observability_runtime_service.rs` (runtime service used by the hard-SLO load gate)
  - `scripts/check-observability-slo-baseline.sh` enforces thresholds from `hard-slo-policy.yaml` and `prelaunch-thresholds.yaml` as source-of-truth.
- Kubernetes baseline with health/scaling signals:
  - `ops/kubernetes/base/*.yaml`
  - `ops/kubernetes/components/**` (multi-AZ topology + KEDA worker autoscaling strategy)
  - `ops/kubernetes/overlays/{dev,staging,production}` (environment-promotion overlays)
  - runtime infrastructure security and access baseline:
    - PostgreSQL topology + PgBouncer transaction pools (`ops/kubernetes/base/postgres-topology.yaml`, `ops/kubernetes/base/pgbouncer.yaml`)
    - pooled runtime DB endpoints (`DATABASE_RW_URL` / `DATABASE_RO_URL`) in all runtime deployments
    - edge + network isolation + secret externalization controls (`ops/kubernetes/base/gateway.yaml`, `ops/kubernetes/base/networkpolicy-*.yaml`, `ops/kubernetes/base/external-secrets.yaml`)
    - SvelteKit adapter-node frontend runtime deployment/service (`ops/kubernetes/base/deployment-web.yaml`, `ops/kubernetes/base/service-web.yaml`)
    - environment-specific autoscaling/topology policy rendered through overlays (`kustomize build ops/kubernetes/overlays/<env>`)

Verification commands:

- `pnpm run observability:verify` (runs baseline gate checks + integration tests)
- `pnpm run release:verify` (contract conformance + observability hard-SLO gates)

CI (`.github/workflows/observability-slo-gate.yml`) blocks merges if hard-SLO release-gate assets are missing or weakened.
