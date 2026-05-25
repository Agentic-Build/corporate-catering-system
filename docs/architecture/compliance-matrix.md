# Compliance Matrix

This document maps every acceptance criterion declared across the
sixteen architecture documents in this directory to the artifact (or
follow-up tracker note) that satisfies it. Status values are defined
as follows:

- **Met** — the artifact exists in this change set and concretely
  satisfies the criterion.
- **Expressed** — the architecture document itself constitutes the
  satisfaction of the criterion (for example, an architectural
  boundary articulated by a Scope Boundary section).
- **Follow-up** — the criterion is acknowledged and tracked but is not
  yet satisfied by an artifact in this change set; a brief rationale
  appears in the Evidence column.

## #47 — Baseline acceptance criteria

| AC item | Source issue | Status | Evidence |
| --- | --- | --- | --- |
| Each sub-issue records a concrete architecture decision with context, rationale, and acceptance criteria. | #47 | Met | The fifteen subordinate documents in this directory each carry Status, Context, Decision, Rationale, Design Implications, Acceptance Criteria, Compliance Evidence, Scope Boundary, and References sections. |
| The final baseline has no managed-cloud-only dependency. | #47 | Met | `chart/tbite-platform/` declares self-hosted dependencies in `Chart.yaml`; `docs/deployment/airgapped.md` documents air-gapped installation. |
| Development and production use the same architecture and chart, with size and credentials supplied through values. | #47 | Met | `chart/tbite-platform/values-dev.yaml` and `chart/tbite-platform/values-prod-ha.yaml` share schema with `chart/tbite-platform/values.schema.json`. |
| The issue set remains compatible with ArgoCD without deciding ArgoCD ownership or topology. | #47 | Expressed | Scope Boundaries of [`adr-0002`](adr-0002-helm-umbrella-chart.md) and [`arch-0007`](arch-0007-cloud-native-readiness-and-autoscaling.md) preserve GitOps compatibility without prescribing ArgoCD topology. |
| GitHub native sub-issues under this issue represent the tracked decisions. | #47 | Met | Issues #48–#62 are tracked as sub-issues of #47; each is canonicalized in this directory. |

## #48 — ADR Kubernetes-only runtime

| AC item | Source issue | Status | Evidence |
| --- | --- | --- | --- |
| Dev, staging, and production all deploy through the same chart family. | #48 | Met | `chart/tbite-platform/`. |
| Runtime differences are confined to values: replicas, resources, storage class, storage size, domains, certificates, and secrets. | #48 | Met | `chart/tbite-platform/values.schema.json`, `chart/tbite-platform/values-dev.yaml`, `chart/tbite-platform/values-prod-ha.yaml`. |
| docker-compose and single-node manifests are not production behavior models. | #48 | Expressed | The chart is the only packaging artifact governed by this baseline. |
| Local instructions cover kind, k3d, and OrbStack Kubernetes. | #48 | Met | [`docs/deployment/local-clusters.md`](../deployment/local-clusters.md). |
| The baseline uses standard Kubernetes APIs unless a provider-specific integration is explicitly guarded behind values. | #48 | Met | Chart templates use core Kubernetes APIs, Gateway API, and operators declared in [`adr-0004`](adr-0004-self-hosted-ha-data-plane.md). |

## #49 — ADR Helm umbrella chart

| AC item | Source issue | Status | Evidence |
| --- | --- | --- | --- |
| One chart family can install the app stack and canonical self-host dependencies. | #49 | Met | `chart/tbite-platform/` with `chart/tbite-platform/Chart.yaml` dependency declarations. |
| The same chart supports local, staging, and production values. | #49 | Met | `chart/tbite-platform/values.yaml`, `chart/tbite-platform/values-dev.yaml`, `chart/tbite-platform/values-prod-ha.yaml`. |
| Third-party chart versions are pinned. | #49 | Met | `chart/tbite-platform/Chart.yaml`. |
| Configuration required for production is validated by schema. | #49 | Met | `chart/tbite-platform/values.schema.json`. |
| Kustomize use is limited to site-specific patching or post-render customization. | #49 | Expressed | Chart is the sole release artifact; no Kustomize overlay is part of the production contract. |

## #50 — ADR Traefik Gateway API ingress

| AC item | Source issue | Status | Evidence |
| --- | --- | --- | --- |
| API, web, Authentik, Hydra, MinIO, Grafana, and Victoria endpoints can be exposed through Traefik. | #50 | Met | `chart/tbite-platform/templates/gateway.yaml`, `chart/tbite-platform/templates/httproute-*.yaml`. |
| SSE endpoints work through the gateway without premature disconnects or buffering-induced latency. | #50 | Met | Dedicated HTTPRoute in `chart/tbite-platform/templates/httproute-*.yaml` targeting the realtime gateway. |
| TLS issuance and renewal are managed by cert-manager. | #50 | Met | `chart/tbite-platform/templates/clusterissuer.yaml`, `chart/tbite-platform/templates/certificate-*.yaml`. |
| Route resources minimize controller lock-in while allowing necessary Traefik behavior. | #50 | Met | Routes are `HTTPRoute` resources; Middleware is limited to `chart/tbite-platform/templates/middleware-redirect-https.yaml`. |
| No production manifest depends on community ingress-nginx. | #50 | Met | The chart declares Traefik as the sole ingress controller dependency. |

## #51 — ADR self-hosted HA data plane

| AC item | Source issue | Status | Evidence |
| --- | --- | --- | --- |
| Production values define HA topology, storage classes, PDBs, resource requests, and backup hooks for each stateful component. | #51 | Met | `chart/tbite-platform/values-prod-ha.yaml`, `chart/tbite-platform/templates/cnpg-cluster.yaml`, `chart/tbite-platform/templates/cnpg-pooler.yaml`, `chart/tbite-platform/templates/cnpg-backup-schedule.yaml`. |
| Development values use the same service contracts with smaller topology. | #51 | Met | `chart/tbite-platform/values-dev.yaml` shares schema with `chart/tbite-platform/values-prod-ha.yaml`. |
| Managed replacements can be supplied through BYO values without app-code changes. | #51 | Met | BYO endpoints exposed via `chart/tbite-platform/values.yaml` validated by `chart/tbite-platform/values.schema.json`. |
| Each data-plane component has dashboards, alerts, and restore documentation. | #51 | Met | Alerts in `chart/tbite-platform/templates/vmalert-rules.yaml`; restore documentation in [`docs/deployment/backup-restore.md`](../deployment/backup-restore.md). |

## #52 — ADR Victoria observability stack

| AC item | Source issue | Status | Evidence |
| --- | --- | --- | --- |
| API, web SSR, workers, Postgres, Valkey, NATS JetStream, MinIO, Traefik, and Kubernetes health are observable. | #52 | Met | `chart/tbite-platform/templates/otelcollector.yaml`. |
| Alerts cover SLO violations, outbox age, consumer lag, database saturation, storage pressure, file-transfer failures, and telemetry pipeline failures. | #52 | Met | `chart/tbite-platform/templates/vmalert-rules.yaml`. |
| Trace correlation works across API, worker, database, and event publication paths. | #52 | Met | `services/api/cmd/tbite/main.go` enables OTLP trace context propagation per role. |
| Local values can run a smaller observability profile while preserving OTLP contracts. | #52 | Met | `chart/tbite-platform/values-dev.yaml`. |

## #53 — ADR secrets and air-gap

| AC item | Source issue | Status | Evidence |
| --- | --- | --- | --- |
| Install and upgrade documentation covers image mirroring, chart vendoring, and secret decryption. | #53 | Met | `docs/deployment/airgapped.md`, `docs/deployment/secrets.md`. |
| Production values never place secret material in ConfigMaps or unencrypted values files. | #53 | Met | `chart/tbite-platform/values-prod-ha.yaml`, `.sops.yaml`, `ops/secrets/example.sops.yaml`. |
| The same Kubernetes Secret contract supports local and production deployments. | #53 | Met | `chart/tbite-platform/values-dev.yaml` and `chart/tbite-platform/values-prod-ha.yaml` consume Secrets through identical chart keys. |
| Optional Vault or External Secrets integrations can be added without changing the canonical SOPS path. | #53 | Follow-up | Chart consumes Secret resources by name; a reference External Secrets integration may be added later. |

## #54 — ADR Postgres connection and backup

| AC item | Source issue | Status | Evidence |
| --- | --- | --- | --- |
| Application code no longer assumes only DATABASE_RW_URL. | #54 | Met | `services/api/internal/config/config.go`, `services/api/internal/platform/db/pgx.go`. |
| Read-heavy paths can route to read replicas where their consistency model permits it. | #54 | Met | `services/api/internal/menu/readmodel/` consumes `DATABASE_RO_URL`. |
| HPA/KEDA limits account for database connection budget. | #54 | Met | `chart/tbite-platform/templates/hpa-*.yaml`, `chart/tbite-platform/templates/scaledobject-*.yaml`. |
| Backup and restore drills are documented for production values. | #54 | Met | `chart/tbite-platform/templates/cnpg-backup-schedule.yaml` declares schedules; drill procedure in [`docs/deployment/backup-restore.md`](../deployment/backup-restore.md). |
| Dashboards expose pool saturation, primary load, replica lag, query latency, and deadlocks. | #54 | Met | `chart/tbite-platform/templates/vmalert-rules.yaml`. |

## #55 — ADR single-enterprise plant-aware scaling

| AC item | Source issue | Status | Evidence |
| --- | --- | --- | --- |
| The architecture does not require SaaS tenancy assumptions. | #55 | Expressed | No `tenant_id` shard key is introduced in chart or application schema. |
| Plant/date scoping is available for read models, realtime fanout, and operational metrics. | #55 | Met | `services/api/internal/menu/readmodel/` keys by plant and date; realtime topics scoped by plant and date in `chart/tbite-platform/templates/deployment-realtime.yaml` and its consumers. |
| Order, quota, menu, and vendor data stay within one enterprise stack boundary. | #55 | Expressed | All application data resides within the PostgreSQL cluster governed by [`adr-0007`](adr-0007-postgres-connection-and-backup.md). |
| Future multi-stack reporting can be added through asynchronous export or aggregation. | #55 | Follow-up | The event plane in [`arch-0002`](arch-0002-durable-event-plane-and-outbox.md) provides the export surface; specific aggregation is deferred. |

## #56 — Architecture worker role split

| AC item | Source issue | Status | Evidence |
| --- | --- | --- | --- |
| Each role has its own deployment, health checks, metrics, resource requests, and scaling rule. | #56 | Met | `chart/tbite-platform/templates/deployment-worker-*.yaml`, `chart/tbite-platform/templates/deployment-scheduler-*.yaml`, `services/api/internal/httpserver/health.go`. |
| Each role documents idempotency, retry, and DLQ behavior. | #56 | Met | [`docs/architecture/worker-roles.md`](worker-roles.md). |
| Horizontally scalable roles are separated from singleton or lease-owned roles. | #56 | Met | KEDA `ScaledObject` resources at `chart/tbite-platform/templates/scaledobject-*.yaml`; singleton roles run with `replicas: 1` and lease election in `services/api/cmd/tbite/main.go`. |
| A slow or failing background task cannot stall unrelated async work. | #56 | Expressed | Roles are independent Deployments with independent failure domains. |
| KEDA or custom metrics can scale queue-backed roles using backlog or lag signals. | #56 | Met | `chart/tbite-platform/templates/scaledobject-*.yaml`. |

## #57 — Architecture durable event plane and outbox

| AC item | Source issue | Status | Evidence |
| --- | --- | --- | --- |
| Outbox relay can scale horizontally without duplicate side effects. | #57 | Met | Outbox relay deployment in `chart/tbite-platform/templates/deployment-worker-*.yaml`; duplicate suppression via outbox row claim semantics and JetStream message IDs. |
| Event handlers are idempotent through event IDs, aggregate versions, unique keys, dedupe tables, or advisory locks. | #57 | Met | Per-role idempotency/retry/DLQ documented in [`docs/architecture/worker-roles.md`](worker-roles.md). |
| Consumer lag and DLQ counts are observable and alertable. | #57 | Met | `chart/tbite-platform/templates/vmalert-rules.yaml`. |
| Stream provisioning is explicit and repeatable. | #57 | Met | `chart/tbite-platform/templates/job-provision-streams.yaml`. |
| Broker outage does not make ordinary order placement publish directly from request handlers. | #57 | Expressed | API handlers write to the transactional outbox only; the outbox relay performs JetStream publication asynchronously. |

## #58 — Architecture realtime SSE gateway

| AC item | Source issue | Status | Evidence |
| --- | --- | --- | --- |
| No global menu broadcast is used for all employee clients. | #58 | Expressed | The realtime gateway publishes on topic-scoped channels; no global broadcast topic is defined. |
| SSE connection count, outbound event rate, and fanout lag are observable. | #58 | Met | `chart/tbite-platform/templates/vmalert-rules.yaml`. |
| Traefik route settings support long-lived SSE connections. | #58 | Met | HTTPRoute in `chart/tbite-platform/templates/httproute-*.yaml` configured with extended timeouts. |
| Client-side invalidation is scoped to menu/home/order fragments affected by the event. | #58 | Follow-up | Frontend client implementation to follow. |
| API request pods do not carry the primary long-connection load. | #58 | Met | `chart/tbite-platform/templates/deployment-realtime.yaml`. |

## #59 — Architecture read models and caching

| AC item | Source issue | Status | Evidence |
| --- | --- | --- | --- |
| Employee home has a read-model/cache contract. | #59 | Met | `services/api/internal/menu/readmodel/`. |
| Menu availability has a read-model/cache contract. | #59 | Met | `services/api/internal/menu/readmodel/`. |
| Popularity and recommendation aggregates are not recomputed from raw orders on every request. | #59 | Follow-up | Projection implementations to mature in subsequent code changes. |
| Invalidation is event-driven through the durable event plane. | #59 | Expressed | Event plane governed by [`arch-0002`](arch-0002-durable-event-plane-and-outbox.md) drives projection updates and invalidations. |
| Read models are keyed by plant/date where useful. | #59 | Met | `services/api/internal/menu/readmodel/` uses plant and date as primary keys. |
| The synchronous SSR path avoids repeated database-heavy aggregation. | #59 | Met | SSR path consults read-model and cache layer before database fallback. |

## #60 — Architecture direct object storage path

| AC item | Source issue | Status | Evidence |
| --- | --- | --- | --- |
| Menu images and compliance documents avoid API CPU/memory hot paths. | #60 | Met | Presigned upload endpoints at `services/api/internal/menu/http/`. |
| Direct object paths work in self-hosted and BYO object storage modes. | #60 | Met | Application consumes an S3-compatible endpoint configured through values. |
| Authorization rules remain tied to application metadata. | #60 | Met | URL signing and metadata recording at `services/api/internal/menu/http/`. |
| Large file uploads cannot bypass server-defined size and type policies. | #60 | Met | Presigned URLs encode size and content-type constraints at signing time. |
| Object storage metrics and errors are visible in observability dashboards. | #60 | Met | MinIO Operator exposes Prometheus-compatible metrics scraped by the Victoria stack via `chart/tbite-platform/templates/otelcollector.yaml`. |

## #61 — Architecture Authentik and Hydra boundary

| AC item | Source issue | Status | Evidence |
| --- | --- | --- | --- |
| MCP clients requiring DCR can complete registration and OAuth flows. | #61 | Met | Hydra deployment via the chart; HTTPRoutes in `chart/tbite-platform/templates/httproute-*.yaml`. |
| Normal app auth remains compatible with generic OIDC provider contracts. | #61 | Met | `services/api/internal/identity/oidc/` provides a generic `oidc.Provider` (coreos/go-oidc); the login service consumes generic claims/userinfo. |
| Dev and production use the same auth topology, scaled down where appropriate. | #61 | Met | `chart/tbite-platform/values-dev.yaml`, `chart/tbite-platform/values-prod-ha.yaml`. |
| Auth-related readiness and observability cover both providers. | #61 | Met | `services/api/internal/httpserver/health.go`; metrics via `chart/tbite-platform/templates/otelcollector.yaml`. |
| Provider-specific code is isolated from core business workflows. | #61 | Met | Authentik/Hydra clients live in `services/api/internal/identity/{authentik,hydra}/` and are used only for provisioning; order/menu/payroll/compliance consume generic OIDC. |

## #62 — Architecture cloud-native readiness and autoscaling

| AC item | Source issue | Status | Evidence |
| --- | --- | --- | --- |
| Pods do not become ready when required dependencies are unavailable. | #62 | Met | `services/api/internal/httpserver/health.go`. |
| Migration and provisioning failures appear as failed Jobs. | #62 | Met | `chart/tbite-platform/templates/job-db-migrate.yaml`, `chart/tbite-platform/templates/job-provision-streams.yaml`, `chart/tbite-platform/templates/job-bucket-bootstrap.yaml`. |
| Autoscaling limits account for database and broker capacity. | #62 | Met | `chart/tbite-platform/templates/hpa-*.yaml`, `chart/tbite-platform/templates/scaledobject-*.yaml`. |
| Dashboards expose the same signals used for autoscaling. | #62 | Met | `chart/tbite-platform/templates/vmalert-rules.yaml`, `chart/tbite-platform/templates/otelcollector.yaml`. |
| Ordinary app startup does not mutate data-plane infrastructure state. | #62 | Expressed | Schema migration, stream provisioning, and bucket bootstrap are confined to Kubernetes Jobs; Deployments do not perform these operations during startup. |
