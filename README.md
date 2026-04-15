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
