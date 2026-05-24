# T-Bite

> 員工 / 商家 / 福委會三端 + Go modular monolith API

A monorepo: 3 SvelteKit frontends + a Go modular monolith + a self-hostable cloud-native Helm umbrella chart + an MCP server for AI agents.

## Architecture

The canonical architecture is recorded in [`docs/architecture/`](docs/architecture/). The
baseline ([#47](docs/architecture/00-baseline.md)) and the fifteen sub-decisions (ADRs
0001–0008 and architecture specs 0001–0007) describe the locked deployment
shape: Kubernetes-only runtime, Helm umbrella chart, CloudNativePG + PgBouncer
+ Valkey HA + NATS JetStream + MinIO Operator data plane, Victoria
observability stack, SOPS+age secrets, plant-aware scaling, role-split
workers, outbox-only publishing, dedicated SSE realtime gateway, read-model
caching, direct object-storage path, Authentik+Hydra identity boundary, and
workload-aware autoscaling.

```
3 SvelteKit apps        <-->  Go binary (one image, many roles)  <-->  Postgres / Valkey / NATS / S3
employee/merchant/admin       api / realtime-gateway / outbox-relay /
                              payroll-settler / on-time-evaluator /
                              cutoff-sweeper / no-show-sweeper /
                              document-expiry-scanner / feedback-scanner /
                              mcp-stdio / provision-streams
```

- **Frontend**: SvelteKit 2 + Svelte 5 + Tailwind 3 (adapter-node, SSR)
- **Backend**: Go 1.23 modular monolith dispatched into per-role Deployments by `--role=<name>`
- **Data**: Postgres (state of record, RW + RO routing), Valkey HA (sessions / cache / read models), NATS JetStream (durable events + outbox-only publication), S3-compatible object storage (presigned upload/download)
- **Observability**: OpenTelemetry → VictoriaMetrics + VictoriaLogs + VictoriaTraces + Grafana
- **Deployment**: Helm umbrella chart [`chart/tbite-platform`](chart/tbite-platform/) is the sole canonical deployment path. The previous kustomize overlays (`single-node`, `gcp`) have been removed.

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

### Helm umbrella chart (canonical)

The Helm umbrella chart at [`chart/tbite-platform`](chart/tbite-platform/) is the
canonical packaging per [ADR-0002](docs/architecture/adr-0002-helm-umbrella-chart.md).
It ships the application, the self-hosted data plane (CloudNativePG, PgBouncer,
Valkey HA, NATS JetStream, MinIO Operator), the Traefik gateway with cert-manager,
the Victoria observability stack, the OpenTelemetry Collector, KEDA, and the
optional Authentik + Hydra identity providers. The same chart renders for
dev (`values-dev.yaml`) and prod (`values-prod.yaml`); BYO endpoints are
supplied through values without changing application code.

```bash
make chart-deps              # one-shot, populates chart/tbite-platform/charts/
make chart-lint              # lints against values-dev + values-prod
make chart-render            # dry-renders to stdout (VALUES=… to override)
make chart-install           # installs into current kubectl context (interactive)
make chart-upgrade           # upgrades the release
```

Secrets are managed with SOPS + age — see [`docs/deployment/secrets.md`](docs/deployment/secrets.md)
and [`docs/deployment/airgapped.md`](docs/deployment/airgapped.md).

### ArgoCD

`ops/argocd/application-helm.yaml` declares the ArgoCD `Application` pointing
at `chart/tbite-platform`. Sync options enable `ServerSideApply` plus a
five-retry exponential backoff so the chart-of-charts CRD bootstrap converges
in a single Application. `kubectl apply -k ops/argocd/` bootstraps the
AppProject + Application on a cluster that already runs ArgoCD.

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
services/api/                        Go modular monolith (12 roles via --role=<name>)
migrations/                          golang-migrate SQL
ops/local/                           docker-compose dev stack
chart/tbite-platform/                Helm umbrella chart (canonical deployment)
ops/argocd/                          ArgoCD AppProject + Application
ops/{load,chaos,security}/           runbooks + scripts
ops/secrets/                         SOPS-encrypted operator secrets
contract/openapi/                    generated OpenAPI artifacts
docs/architecture/                   ADRs + architecture specifications
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
| `make chart-deps` / `chart-lint` / `chart-render` / `chart-install` / `chart-upgrade` / `chart-uninstall` | Helm umbrella chart lifecycle |
| `make sops-encrypt` / `sops-decrypt` / `sops-edit` | SOPS workflow |
| `make image-build-local` | Build the platform image for the local Docker daemon |
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
