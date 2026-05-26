# T-Bite

> 員工 / 商家 / 福委會三端 + Go modular monolith API

A monorepo: 3 SvelteKit frontends + a Go modular monolith + a self-hostable cloud-native Helm umbrella chart + an MCP server for AI agents.

## Architecture

The canonical architecture is recorded in [`docs/architecture/`](docs/architecture/). The
system-level contract ([`00-baseline.md`](docs/architecture/00-baseline.md)) and
the fifteen sub-decisions (ADRs 0001–0008 and architecture specs 0001–0007)
describe the locked deployment
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

Pre-reqs: Node 20.11+, pnpm 9, Go 1.23, `kubectl`, `helm`, and a
local Kubernetes runtime such as kind, k3d, or OrbStack.

```bash
pnpm install
go mod download
kubectl config current-context   # confirm the local cluster context
make dev
```

`make dev` installs or upgrades the Helm umbrella chart with
`chart/tbite-platform/values-dev.yaml` in the current Kubernetes context.
Fresh clusters and host mappings are covered in
[`docs/deployment/local-clusters.md`](docs/deployment/local-clusters.md).
Docker Compose is not a supported runtime path.

URLs:

- http://app.tbite.local — 員工
- http://merchant.tbite.local — 商家
- http://admin.tbite.local — 福委會
- http://api.tbite.local/healthz — Go API
- http://api.tbite.local/docs — API reference (Stoplight Elements, served by huma)
- http://api.tbite.local/openapi.yaml — machine-readable OpenAPI 3.1 spec
- http://minio.tbite.local — MinIO console, when enabled by chart values
- http://grafana.tbite.local — Grafana, when enabled by chart values

Seed data is applied against an explicit database URL:

```bash
export DATABASE_URL='postgres://...'
export S3_ENDPOINT='http://minio.tbite.local'
export S3_ACCESS_KEY_ID='...'
export S3_SECRET_ACCESS_KEY='...'
export S3_BUCKET='tbite-dev'
export S3_PUBLIC_BASE_URL='http://minio.tbite.local'
make seed
```

Release lifecycle:

```bash
make dev-down                  # uninstall Helm release
make dev-reset                 # delete namespace and volumes
make dev-logs component=api
```

## Production deployment

### Helm umbrella chart (canonical)

The Helm umbrella chart at [`chart/tbite-platform`](chart/tbite-platform/) is the
canonical packaging per [ADR-0002](docs/architecture/adr-0002-helm-umbrella-chart.md).
It ships the application, the self-hosted data plane (CloudNativePG, PgBouncer,
Valkey HA, NATS JetStream, MinIO Operator), the Traefik gateway with cert-manager,
the Victoria observability stack, the OpenTelemetry Collector, KEDA, and the
optional Authentik + Hydra identity providers. The base `values.yaml` is the
single-enterprise production profile; `values-dev.yaml` and
`values-prod-ha.yaml` are overlays for laptop and multi-AZ HA shapes. BYO
endpoints are supplied through values without changing application code.

```bash
make chart-deps              # one-shot, populates chart/tbite-platform/charts/
make chart-lint              # lints against values-dev + values-prod-ha
make chart-render            # dry-renders to stdout (VALUES=… to override)
make chart-install           # installs into current kubectl context (interactive)
make chart-upgrade           # upgrades the release
```

Secrets are managed with SOPS + age — see [`docs/deployment/secrets.md`](docs/deployment/secrets.md)
and [`docs/deployment/airgapped.md`](docs/deployment/airgapped.md).

### ArgoCD

`ops/argocd/application-helm.yaml` declares the environment-specific ArgoCD
`Application` pointing at `chart/tbite-platform`; `application-tsmc-demo.yaml`
is the generic TSMC demo variant. Sync options enable `ServerSideApply` plus
a five-retry exponential backoff so the chart-of-charts CRD bootstrap
converges in a single Application. Apply `ops/argocd/project.yaml` plus the
Application you want on a cluster that already runs ArgoCD.

## Operations

| Area | Entry point |
| --- | --- |
| Migrations | `make migrate-up` (golang-migrate), SQL in `migrations/` |
| Load testing | `ops/load/run-loadtest.sh` against an already deployed chart (`API_BASE_URL` + `K6_*` env) |
| Load-gate CI | `.github/workflows/ci-load-gate.yml` (nightly + manual_dispatch) |
| Security scan | `scripts/security-scan.sh` (trivy + kubesec) |
| SQL-injection guard | `scripts/security/check-sql-strings.sh` (runs in `ci-lint-test`) |
| Chaos drill | [`ops/chaos/drill-runbook.md`](ops/chaos/drill-runbook.md) |
| TSMC applied demo | [`docs/demo/tsmc-applied-playbook.md`](docs/demo/tsmc-applied-playbook.md) |
| Security baseline | [`ops/security/checklist.md`](ops/security/checklist.md) |
| MCP server | [`docs/mcp.md`](docs/mcp.md) |
| Branding policy | [`docs/branding.md`](docs/branding.md) |

## Repository layout

```
apps/{employee,merchant,admin}/      SvelteKit frontends
packages/{ui,tokens,api-client,web-auth}/   shared
services/api/                        Go modular monolith (12 roles via --role=<name>)
migrations/                          golang-migrate SQL
chart/tbite-platform/                Helm umbrella chart (canonical deployment)
ops/argocd/                          ArgoCD AppProject + Application
ops/{load,chaos,demo,security}/      runbooks + scripts
ops/secrets/                         SOPS-encrypted operator secrets
contract/openapi/                    generated OpenAPI artifacts
docs/architecture/                   ADRs + architecture specifications
docs/                                deployment runbooks, MCP, branding, plans
```

## Common make targets

| Target | Description |
| --- | --- |
| `make dev` | Install/upgrade the local Kubernetes dev chart |
| `make dev-down` / `make dev-reset` | Uninstall release / delete namespace |
| `make migrate-up` / `migrate-down` / `migrate-new name=xxx` | Schema |
| `make seed-tsmc` | Seed `DATABASE_URL` with the 50k-person TSMC demo |
| `make contract-sync` | Regenerate OpenAPI + TS client from Go |
| `make test-go` / `make test-web` / `make test-e2e` | Tests |
| `make chart-deps` / `chart-lint` / `chart-render` / `chart-install` / `chart-upgrade` / `chart-uninstall` | Helm umbrella chart lifecycle |
| `make sops-encrypt` / `sops-decrypt` / `sops-edit` | SOPS workflow |
| `make image-build-local` | Build the platform image for the local Docker daemon |
| `make demo-seed-tsmc` / `demo-load-tsmc` / `demo-crisis component=api` | Kubernetes TSMC demo operations |
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
