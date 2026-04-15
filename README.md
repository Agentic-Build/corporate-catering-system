# corporate-catering-system

## OpenAPI Contract Platform

Canonical HTTP contract artifacts are generated from the Rust contract module.

- Export spec and docs: `cargo run --bin export_openapi_contract -- artifacts/openapi`
- Output artifacts:
  - `artifacts/openapi/openapi.json` (machine-readable)
  - `artifacts/openapi/openapi.yaml` (machine-readable)
  - `artifacts/openapi/index.html` (browsable Redoc docs)
- Generate and type-check TS client/types from OpenAPI:
  - `pnpm run contract:verify`

CI (`.github/workflows/openapi-contract.yml`) enforces contract checks and publishes artifacts.
