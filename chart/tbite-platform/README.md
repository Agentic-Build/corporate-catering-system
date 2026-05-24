# tbite-platform Helm umbrella chart

Self-hostable, cloud-native umbrella chart for the tbite corporate catering
platform. This chart bundles the application workloads (api, realtime SSE
gateway, three SSR web frontends, queue-driven workers and singleton
schedulers) with the full self-hosted dependency baseline:

- CloudNativePG for Postgres + PgBouncer pooler
- Valkey (Bitnami) with Sentinel HA
- NATS JetStream cluster
- MinIO operator + tenant
- cert-manager + Traefik (Gateway API enabled)
- goauthentik/authentik OIDC IdP and ory/hydra OAuth2 server
- Victoria-Metrics k8s stack (vm-operator, vmagent, vmalert, alertmanager,
  grafana) + victoria-logs-single + victoria-traces-single
- OpenTelemetry Collector
- KEDA for queue-driven autoscaling

Every dependency is conditional on `<key>.enabled` so that operators can
BYO (bring-your-own) external endpoints in production.

## Profiles

The same chart targets both local single-node clusters (kind, k3d, OrbStack)
and full production HA via the `profile.size` switch and overlay files.

```bash
# Local single-node
helm template tbite . -f values-dev.yaml

# Production HA
helm template tbite . -f values-prod.yaml
```

## Install (after generating the lock file)

The chart bundles fourteen subcharts that own CustomResourceDefinitions
(VictoriaMetrics-operator's `VMServiceScrape` / `VMRule` / `VMSingle` /
`VMAlert`, CloudNativePG's `Cluster` / `Pooler`, cert-manager's
`Issuer` / `Certificate`, MinIO's `Tenant`, Traefik's `Middleware` /
`GatewayClass`, KEDA's `ScaledObject`, etc.). Helm renders the
umbrella as a single release and submits CRDs and the resources that
use them in one `apply` pass — the Kubernetes API server cannot
establish a CRD and accept resources of that kind within the same
batch, so a fresh-cluster `helm install` of the full umbrella fails
with `ensure CRDs are installed first` errors. This is the standard
chart-of-charts CRD-bootstrap pitfall, not a chart bug.

### Two-pass install (recommended for fresh clusters)

```bash
# Phase 1 — bootstrap CRD-providing operators only. Disable the
# observability and CRD-consuming layers.
helm dependency update chart/tbite-platform

helm install tbite-bootstrap chart/tbite-platform \
  -f chart/tbite-platform/values-dev.yaml \
  --namespace tbite --create-namespace \
  --set observability.victoriaMetrics.enabled=false \
  --set observability.victoriaLogs.enabled=false \
  --set observability.victoriaTraces.enabled=false

# Phase 2 — once CRDs are established, upgrade to the full chart.
helm upgrade tbite-bootstrap chart/tbite-platform \
  -f chart/tbite-platform/values-dev.yaml \
  --namespace tbite
```

### Single-pass install (clusters with pre-installed CRDs)

When the operator CRDs are already present on the cluster (e.g. via a
platform-team install or ArgoCD sync-wave ordering at a higher
layer), the single-command install works:

```bash
helm dependency update chart/tbite-platform
helm template tbite chart/tbite-platform -f chart/tbite-platform/values-prod.yaml
helm install tbite chart/tbite-platform -f chart/tbite-platform/values-prod.yaml --namespace tbite --create-namespace
```

ArgoCD users typically express phase 1 / phase 2 as two
`argoproj.io/sync-wave` annotations on the umbrella subchart toggles
or as two separate `Application` objects in an `app-of-apps`
layout; that ordering is intentionally out of scope of this chart.

## Helm hooks

The chart ships three pre-install/pre-upgrade hook Jobs:

| Job | Purpose | Image |
| --- | --- | --- |
| `db-migrate` | runs `migrate -path /migrations -database $DATABASE_RW_URL up` | `migrate/migrate` |
| `provision-streams` | runs the platform binary with `--role=provision-streams` (idempotent) | platform image |
| `bucket-bootstrap` | ensures S3 buckets exist and policies are applied | `minio/mc` |

## ArgoCD

The chart renders cleanly through `helm template`, which is what ArgoCD calls
under the hood. Argo `Application`/`ApplicationSet` layouts are out of scope
of this chart and live in `ops/argocd/`.

## Layout

See `templates/` for application workloads, Gateway API routes, NetworkPolicies,
HPAs, PDBs, KEDA ScaledObjects, CNPG `Cluster`, MinIO `Tenant`, OTel
`OpenTelemetryCollector` and Victoria-Metrics scrape configs and alerts.

## Schema-validated production fields

`values.schema.json` enforces in the `prod` profile:

- `postgres.cluster.instances >= 3`
- `nats.cluster.replicas >= 3`
- `valkey.replicaCount >= 3`
- `minio.tenant.tenant.pools[*].servers >= 4`
- `api.replicaCount >= 2`, `realtime.replicaCount >= 2`
- `web.*.replicaCount >= 2`

And in every profile:

- `global.baseURL.{api,employee,merchant,admin}` must be valid URLs
- `global.oidcIssuerURL` must be a URL
- `global.s3.*` credentials secret ref required
- `api.database.rwUrlSecretRef` required when `api.enabled`

## Secret references

The chart never embeds secrets. The following secret references are expected
to be created out-of-band (via SOPS, Sealed Secrets, External Secrets, etc):

- `tbite-db` — keys: `rwUrl`, `roUrl`
- `tbite-s3` — keys: `accessKeyID`, `secretAccessKey`
- `tbite-nats` — key: `creds`
- `tbite-valkey` — key: `password`
- `tbite-sops-age` — key: `pub`
- `tbite-pg-backup-s3` — keys: `ACCESS_KEY_ID`, `ACCESS_SECRET_KEY`
- `tbite-grafana-admin` — key: `password`
- `tbite-minio-env` — keys: `accessKey`, `secretKey`
