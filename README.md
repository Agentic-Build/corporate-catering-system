# T-Bite

> 員工 / 商家 / 福委會三端 + Go modular monolith API + 雙 K8s overlay (single-node / GCP)

A monorepo refactor of the T-Bite platform: 3 SvelteKit frontends + Go modular monolith + dual K8s deployment + MCP server for AI agents.

## Status

**Refactor complete (P0-P8 all delivered).** See [`docs/plans/2026-05-13-tbite-refactor-design.md`](docs/plans/2026-05-13-tbite-refactor-design.md) for the full design.

| Phase | Scope |
|---|---|
| P0 | monorepo skeleton + Go API multi-role binary + dual K8s overlay |
| P1 | identity + OIDC (Google/GitHub) + 三端登入流 |
| P2 | menu / vendor / quota + 員工瀏覽今日菜單 + Postgres-anchored decrement |
| P3 | order lifecycle + audit + outbox + cutoff scheduler |
| P4 | TOTP pickup + 商家備餐 + ready/picked_up/no_show |
| P5 | payroll batches + HR CSV export + dispute / refund flow |
| P6 | vendor governance + anomaly alerts + DLQ admin + employee disputes |
| P7 | MCP server for AI agents (12 tools, HTTP/SSE + stdio) |
| P8 | hardening + k6 load gate + OTel + scheduler leader election + chaos runbook |

## Architecture

```
3 SvelteKit apps        <-->  Go API (chi + huma)  <-->  Postgres / Redis / NATS / S3
employee/merchant/admin       same binary as
                              worker / scheduler / mcp-stdio
```

- **Frontend**: SvelteKit 2 + Svelte 5 + Tailwind 3 (adapter-node, SSR)
- **Backend**: Go 1.23, modular monolith, single binary 4 roles (`api`/`worker`/`scheduler`/`mcp-stdio`)
- **Data**: Postgres (orders/menu/vendor/payroll), Redis (sessions/state/cache), NATS JetStream (events), S3-compat (HR CSV / vendor docs)
- **Observability**: OTel traces (HTTP/DB/NATS) via OTLP HTTP collector
- **Deployment**: dual K8s overlay (`single-node` for k3d, `gcp` for managed services)

## Quick start (local dev)

Pre-reqs: Node 20.11+, `pnpm` 9, Go 1.23, Docker. Full K8s mode also needs `k3d`, `kubectl`, `kustomize`.

### Option A: `make dev-app` — host processes (fastest)

Starts the Go API plus the three SvelteKit dev servers without K8s:

```bash
pnpm install
( cd services/api && go mod download )
make dev-app
```

URLs:
- `http://localhost:8080/healthz` — Go API
- `http://localhost:5173` — 員工 app
- `http://localhost:5174` — 商家 app
- `http://localhost:5175` — 福委會 app

### Option B: `make dev-up` — k3d cluster (closer to prod)

```bash
make dev-up        # k3d cluster + single-node overlay applied
make dev-down      # tear down
make dev-reset     # dev-down + clean volumes + restart
```

The single-node overlay bundles Postgres / Redis / NATS / MinIO as
single-pod deployments. Seeded test users:

- Employee: `e2e-employee@tbite.test` (via fake OIDC provider)
- Admin: `e2e-admin@tbite.test` (whitelisted)

See [`docs/mcp.md`](docs/mcp.md) for MCP setup.

## Operations

| Area | Entry point |
| --- | --- |
| Migrations | `make migrate-up` (golang-migrate wrapper), SQL in `migrations/` |
| Load testing | `ops/load/run-loadtest.sh` (k6 lunch-peak, 3 scenarios) |
| Load-gate CI | `.github/workflows/ci-load-gate.yml` (nightly + manual_dispatch) |
| Security scan | `scripts/security-scan.sh` (trivy on images + kubesec on manifests) |
| Chaos drill | [`ops/chaos/drill-runbook.md`](ops/chaos/drill-runbook.md) |
| Security baseline | [`ops/security/checklist.md`](ops/security/checklist.md) |
| Render overlay | `make render-overlay env=single-node` / `env=gcp` |

## Repository layout

```
apps/{employee,merchant,admin}/   SvelteKit frontends
packages/{ui,tokens,api-client,web-auth}/   shared
services/api/                     Go API + worker + scheduler + mcp-stdio
migrations/                       golang-migrate SQL
ops/kubernetes/{base,overlays}/   K8s manifests
ops/{load,chaos,security}/        runbooks + scripts
ops/observability/                OTel collector config
contract/openapi/                 generated OpenAPI artifacts
docs/plans/                       phase plans + design doc
docs/mcp.md                       MCP server reference
```

## Common make targets

| Target | Description |
| --- | --- |
| `make dev-up` | k3d cluster + apply single-node overlay |
| `make dev-down` | Tear down k3d cluster |
| `make dev-app` | Parallel run Go API + 3 SvelteKit dev servers |
| `make migrate-up` | Apply pending migrations |
| `make contract-sync` | Regenerate OpenAPI + TS client from Go (gated in CI) |
| `make test-go` | `go test ./...` |
| `make test-web` | `pnpm -r check && pnpm -r lint` |
| `make render-overlay env=single-node` | `kustomize build` the overlay |
| `make render-overlay env=gcp` | Same, GCP overlay |

Full list: `make help`.

## Contributing

1. Pick a P0-P8 plan in `docs/plans/`.
2. `git checkout -b feat/<scope>-<topic>` (e.g. `feat/identity-oidc`).
3. Match existing patterns: Service + Repo + huma handlers (Go), SvelteKit form actions (web).
4. Run `make contract-sync` before pushing — CI fails on drift.
5. Commit messages follow Conventional Commits.

## License

Internal. (c) Agentic-Build.
