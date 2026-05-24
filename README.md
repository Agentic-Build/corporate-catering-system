# T-Bite

> 員工 / 商家 / 福委會三端 + Go modular monolith API

A monorepo: 3 SvelteKit frontends + a Go modular monolith + dual K8s overlays (single-node / GCP) + an MCP server for AI agents.

## Architecture

```
3 SvelteKit apps         <-->  Go API (chi + huma)  <-->  Postgres / Redis / NATS / S3
employee/merchant/admin        same binary, 4 roles:
                               api / worker / scheduler / mcp-stdio
```

- **Frontend**: SvelteKit 2 + Svelte 5 + Tailwind 3 (adapter-node, SSR)
- **Backend**: Go 1.23 modular monolith
- **Data**: Postgres (state of record), Redis (sessions / cache), NATS JetStream (events), S3-compatible object storage (HR CSV / vendor docs)
- **Observability**: OpenTelemetry traces via OTLP HTTP
- **Deployment**: dual kustomize overlay — `single-node` for any self-managed cluster, `gcp` for GKE + Cloud SQL + Memorystore + GCS

## Local development

Pre-reqs: Node 20.11+, pnpm 9, Go 1.23, Docker.

```bash
pnpm install
go mod download
make dev
```

`make dev` starts Postgres / Redis / NATS / MinIO / Authentik via `docker compose`, applies migrations, seeds p2 fixtures, then runs the Go API and the three SvelteKit dev servers on the host. Ctrl-C stops the host processes; deps stay up.

URLs:

- http://localhost:5173 — 員工
- http://localhost:5174 — 商家
- http://localhost:5175 — 福委會
- http://localhost:8080/healthz — Go API
- http://localhost:8080/docs — API reference (Stoplight Elements, served by huma)
- http://localhost:8080/openapi.yaml — machine-readable OpenAPI 3.1 spec
- http://localhost:9002 — Authentik (`akadmin` / `tbite-dev-admin`)
- http://localhost:9001 — MinIO console (`tbite` / `tbite-dev-secret`)

Seeded Authentik identities (`tbite-dev-pass`):

- Employee: `e2e-employee@tbite.test`
- Admin (福委會): `e2e-admin@tbite.test`
- Merchant: `e2e-merchant@tbite.test`

Stop / reset deps:

```bash
make dev-down      # stop deps; volumes persisted
make dev-reset     # stop deps and wipe volumes
make dev-logs svc=postgres
```

> **Upgrading Postgres across major versions** (e.g. the 16 → 18 bump): Postgres
> will not start on a data directory written by an older major and exits with
> "database files are incompatible with server". Major upgrades have no
> in-place path for the dev stack — run `make dev-reset` to wipe the volumes,
> then `make dev` to re-init on the new version (migrations + seed re-run
> automatically; the Authentik blueprint re-applies its dev users).

## Production deployment

Two overlays, same `make` interface:

```bash
make prod-up env=single-node    # any reachable k8s cluster
make prod-up env=gcp            # GKE + Cloud SQL + Memorystore + GCS
```

Both targets print the active `kubectl` context and require interactive confirmation before applying.

- [`docs/deployment/single-node.md`](docs/deployment/single-node.md) — self-managed k8s
- [`docs/deployment/gcp.md`](docs/deployment/gcp.md) — GCP runbook (Workload Identity, External Secrets, Cloud Armor, managed certs)

To check what kustomize will produce without applying:

```bash
make render-overlay env=single-node
make render-overlay env=gcp
```

## Operations

| Area | Entry point |
| --- | --- |
| Migrations | `make migrate-up` (golang-migrate), SQL in `migrations/` |
| Load testing | `ops/load/run-loadtest.sh` (k6 lunch-peak, 3 scenarios) |
| Load-gate CI | `.github/workflows/ci-load-gate.yml` (nightly + manual_dispatch) |
| Security scan | `scripts/security-scan.sh` (trivy + kubesec) |
| SQL-injection guard | `scripts/security/check-sql-strings.sh` (runs in `ci-lint-test`) |
| Chaos drill | [`ops/chaos/drill-runbook.md`](ops/chaos/drill-runbook.md) |
| Security baseline | [`ops/security/checklist.md`](ops/security/checklist.md) |
| MCP server | [`docs/mcp.md`](docs/mcp.md) |
| Branding policy | [`docs/branding.md`](docs/branding.md) |

## Repository layout

```
apps/{employee,merchant,admin}/      SvelteKit frontends
packages/{ui,tokens,api-client,web-auth}/   shared
services/api/                        Go API + worker + scheduler + mcp-stdio
migrations/                          golang-migrate SQL
ops/local/                           docker-compose dev stack
ops/kubernetes/{base,overlays}/      K8s manifests
ops/{load,chaos,security}/           runbooks + scripts
contract/openapi/                    generated OpenAPI artifacts
docs/                                deployment runbooks, MCP, branding, plans
```

## Common make targets

| Target | Description |
| --- | --- |
| `make dev` | One-stop dev — compose deps + migrate + seed + host processes |
| `make dev-down` / `make dev-reset` | Stop deps (keep / wipe volumes) |
| `make migrate-up` / `migrate-down` / `migrate-new name=xxx` | Schema |
| `make contract-sync` | Regenerate OpenAPI + TS client from Go |
| `make test-go` / `make test-web` / `make test-e2e` | Tests |
| `make render-overlay env=…` | Dry-render a kustomize overlay |
| `make prod-up env=…` / `prod-status env=…` / `prod-down env=…` | Apply / inspect / remove overlay |
| `make build` / `make clean` | Bundle + binary / wipe artifacts |

Full list: `make help`.

## Contributing

1. Branch as `feat/<scope>-<topic>` or `fix/<scope>-<topic>`.
2. Match existing patterns: Service + Repo + huma handlers (Go); SvelteKit form actions (web); raw `pgx` with `$N` parameterized binding (SQL — CI grep guards against `fmt.Sprintf("SELECT…")`).
3. Run `make contract-sync` after touching Go HTTP handlers — CI fails on drift.
4. Commit messages follow Conventional Commits.
5. See [`docs/branding.md`](docs/branding.md) before introducing new product-name strings.

## License

Internal. (c) Agentic-Build.
