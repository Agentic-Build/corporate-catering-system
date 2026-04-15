# corporate-catering-system

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
- Hard-SLO policy, dashboard, and alerts:
  - `ops/observability/slo/hard-slo-policy.yaml`
  - `ops/observability/slo/grafana-dashboard-hard-slo.json`
  - `ops/observability/slo/alerts.yaml`
- Pre-launch load-test thresholds:
  - `ops/observability/load/prelaunch-thresholds.yaml`
  - `ops/observability/load/k6-prelaunch.js`
- Kubernetes baseline with health/scaling signals:
  - `ops/kubernetes/base/*.yaml`

Verification commands:

- `pnpm run observability:verify` (runs baseline gate checks + integration tests)
- `pnpm run release:verify` (contract conformance + observability hard-SLO gates)

CI (`.github/workflows/observability-slo-gate.yml`) blocks merges if hard-SLO release-gate assets are missing or weakened.
