# 00 — Self-Hostable Cloud-Native Scaling Baseline

- **Status**: Adopted — 2026-05-25
- **Source issue**: [Agentic-Build/corporate-catering-system#47](https://github.com/Agentic-Build/corporate-catering-system/issues/47)

## Context

The corporate catering system is delivered as an on-premise, self-hosted
product addressed to a single enterprise per stack. The platform already
possesses several mature foundations: stateless Go API roles, SvelteKit
frontends, PostgreSQL as the transactional source of truth, a
Redis-compatible session and cache layer, NATS JetStream for domain
events, and S3-compatible object storage for files. These foundations are
sufficient to add application replicas, yet a system-level scaling review
identified that horizontal replication of application processes alone
will not yield a productionable cloud-native baseline. Shared
infrastructure becomes the first bottleneck: database connection budgets
are exhausted before request capacity is, worker ownership boundaries
hide backpressure, realtime fanout collides with ordinary request
handling, bulk file transfer competes with business request handling for
API CPU and memory, and read-heavy pages re-aggregate transactional data
on every request.

The deployment target is decisive for the architectural shape. A
single-enterprise stack does not require a SaaS-style tenant control
plane and should not adopt the operational complexity that such a plane
implies. Instead, the relevant internal scaling dimensions are plants,
vendors, dates, and order volume within one enterprise installation.
Furthermore, the product must operate consistently across heterogeneous
self-hosted environments, including restricted networks and air-gapped
sites, without rendering the production path dependent on any single
managed cloud provider.

This document is deliberately framed as an architecture contract. It
does not rank implementation work, assign delivery order, or decide
ArgoCD application topology. Those choices belong to the implementation
and platform owners and are addressed in operational documentation.

## Decision

The system adopts a self-hostable, cloud-native scaling baseline whose
locked direction is summarized below and developed in fifteen
subordinate Architecture Decision Records and Architecture
Specifications.

- **Runtime**: CNCF-conformant Kubernetes.
- **Local development targets**: kind, k3d, and OrbStack Kubernetes.
- **Packaging**: Helm umbrella chart with a canonical batteries-included
  self-host mode and an optional bring-your-own-endpoint (BYO) mode.
- **Ingress and gateway**: Traefik with Kubernetes Gateway API where
  practical and cert-manager for TLS issuance.
- **Secrets**: SOPS plus age as the canonical mechanism.
- **Data plane**: CloudNativePG, PgBouncer, Valkey HA, NATS Helm chart
  with JetStream enabled, and MinIO Operator.
- **Observability**: VictoriaMetrics, VictoriaLogs, VictoriaTraces,
  Grafana, and the OpenTelemetry Collector.
- **Authentication**: Authentik as the reference self-host SSO provider;
  Hydra retained for MCP Dynamic Client Registration.
- **Realtime**: Server-Sent Events with topic-scoped fanout separated
  from ordinary API request handling.
- **Consistency model**: order placement and quota remain strongly
  consistent; menu, home, and recommendation surfaces use event-driven
  read models with bounded eventual consistency.

## Rationale

A self-hostable production baseline must satisfy three simultaneous
constraints: portability across diverse customer environments,
operational discipline for stateful components, and scaling behavior
that does not promote shared infrastructure to first bottleneck. The
locked direction satisfies these constraints through a deliberate
narrowing of choice at every layer. Kubernetes provides the substrate
discipline. The Helm umbrella chart provides packaging discipline. The
self-hosted data plane provides operational discipline. The Victoria
observability stack provides measurement discipline. The realtime
gateway, durable event plane, and read-model contracts provide
application-level discipline that turns horizontal replication into
genuine capacity rather than connection storms.

A central alternative considered and rejected was to allow a parallel
managed-cloud production path (for instance, a vendor-managed Postgres,
a vendor-managed object store, and a vendor-managed message broker) on
the grounds that customers with mature cloud accounts would prefer it.
That alternative was rejected because it would split the production
contract into two surfaces with materially different failure modes,
observability shapes, and capacity primitives, which would in turn
fracture the testing matrix and erode the self-hostable claim. The
baseline accepts BYO endpoints at the configuration boundary, but the
canonical production path is self-hosted and uniform.

## Design Implications

The baseline produces a single product-shaped artifact: a Helm umbrella
chart that installs the application services together with the canonical
self-hosted dependencies and that supports BYO endpoints through values
without introducing application-code branches. The same chart family is
used in development, staging, and production, with size and credentials
supplied through values files. Backup, restore, observability, and
provisioning are properties of the chart, not separate operational
manuals.

## Sub-decisions

The baseline is decomposed into the following fifteen subordinate
documents. Each is binding and each carries its own acceptance criteria
and compliance evidence. The summaries below are intentionally terse;
the documents themselves are authoritative.

| Document | Decision summary |
| --- | --- |
| [`adr-0001-kubernetes-only-runtime.md`](adr-0001-kubernetes-only-runtime.md) | Kubernetes is the only formal runtime; local clusters use kind, k3d, or OrbStack. |
| [`adr-0002-helm-umbrella-chart.md`](adr-0002-helm-umbrella-chart.md) | A Helm umbrella chart is the canonical packaging format with batteries-included and BYO modes. |
| [`adr-0003-traefik-gateway-api-ingress.md`](adr-0003-traefik-gateway-api-ingress.md) | Traefik is the canonical gateway controller; Gateway API is the primary route model; cert-manager owns TLS. |
| [`adr-0004-self-hosted-ha-data-plane.md`](adr-0004-self-hosted-ha-data-plane.md) | CloudNativePG, PgBouncer, Valkey HA, NATS JetStream, and MinIO Operator are the canonical data plane. |
| [`adr-0005-victoria-observability-stack.md`](adr-0005-victoria-observability-stack.md) | VictoriaMetrics, VictoriaLogs, VictoriaTraces, Grafana, and the OpenTelemetry Collector form the canonical observability backend. |
| [`adr-0006-secrets-and-air-gap.md`](adr-0006-secrets-and-air-gap.md) | SOPS plus age is the canonical secrets workflow; air-gapped operation is a baseline product requirement. |
| [`adr-0007-postgres-connection-and-backup.md`](adr-0007-postgres-connection-and-backup.md) | PgBouncer fronts Postgres; DATABASE_RW_URL and DATABASE_RO_URL are application contracts; PITR is required. |
| [`adr-0008-single-enterprise-plant-aware-scaling.md`](adr-0008-single-enterprise-plant-aware-scaling.md) | One enterprise per stack; plant and date are the dominant internal partition keys. |
| [`arch-0001-worker-role-split.md`](arch-0001-worker-role-split.md) | Background work is decomposed into role-specific deployments with independent scaling and failure domains. |
| [`arch-0002-durable-event-plane-and-outbox.md`](arch-0002-durable-event-plane-and-outbox.md) | NATS JetStream is the durable event plane; the transactional outbox is the only business-event exit. |
| [`arch-0003-realtime-sse-gateway.md`](arch-0003-realtime-sse-gateway.md) | A dedicated realtime gateway serves topic-scoped SSE fanout, isolated from the API request path. |
| [`arch-0004-read-models-and-caching.md`](arch-0004-read-models-and-caching.md) | Employee home, menu availability, and recommendations are served from event-driven read models with bounded eventual consistency. |
| [`arch-0005-direct-object-storage-path.md`](arch-0005-direct-object-storage-path.md) | Bulk file transfer flows directly to object storage; the API authorizes but does not proxy bytes. |
| [`arch-0006-authentik-hydra-identity-boundary.md`](arch-0006-authentik-hydra-identity-boundary.md) | Authentik is the reference enterprise SSO provider; Hydra retains MCP DCR responsibility. |
| [`arch-0007-cloud-native-readiness-and-autoscaling.md`](arch-0007-cloud-native-readiness-and-autoscaling.md) | Lifecycle tasks are explicit Kubernetes resources; readiness and autoscaling use workload-aware signals. |

## Compliance Evidence

The baseline-level acceptance criteria from the source issue are
satisfied by the existence and content of the subordinate documents and
by the artifacts they govern. A single mapping from every acceptance
criterion across the sixteen documents to the artifacts that satisfy it
is provided in [`compliance-matrix.md`](compliance-matrix.md). The
table below summarizes the baseline-level mapping.

| Acceptance criterion | Compliance |
| --- | --- |
| Each sub-issue records a concrete architecture decision with context, rationale, and acceptance criteria. | The fifteen subordinate documents listed above each carry Status, Context, Decision, Rationale, Design Implications, Acceptance Criteria, Compliance Evidence, Scope Boundary, and References sections. |
| The final baseline has no managed-cloud-only dependency. | Expressed by [`adr-0001`](adr-0001-kubernetes-only-runtime.md), [`adr-0004`](adr-0004-self-hosted-ha-data-plane.md), [`adr-0005`](adr-0005-victoria-observability-stack.md), and [`adr-0006`](adr-0006-secrets-and-air-gap.md). Realized in `chart/tbite-platform/` and `docs/deployment/airgapped.md`. |
| Development and production use the same architecture and chart, with size and credentials supplied through values. | Expressed by [`adr-0001`](adr-0001-kubernetes-only-runtime.md) and [`adr-0002`](adr-0002-helm-umbrella-chart.md). Realized in `chart/tbite-platform/values-dev.yaml` and `chart/tbite-platform/values-prod.yaml`. |
| The issue set remains compatible with ArgoCD without deciding ArgoCD ownership or topology. | Expressed by [`adr-0002`](adr-0002-helm-umbrella-chart.md) Scope Boundary and [`arch-0007`](arch-0007-cloud-native-readiness-and-autoscaling.md) Scope Boundary. The chart renders deterministically and contains no Argo-specific assumptions. |
| GitHub native sub-issues under this issue represent the tracked decisions. | The source issue [#47](https://github.com/Agentic-Build/corporate-catering-system/issues/47) hosts the sub-issues [#48](https://github.com/Agentic-Build/corporate-catering-system/issues/48) through [#62](https://github.com/Agentic-Build/corporate-catering-system/issues/62); the corresponding documents in this directory canonicalize each. |

## Scope Boundary

This baseline does not assign delivery order, rank implementation work,
decide ArgoCD application topology, certify any specific Kubernetes
distribution beyond the CNCF-conformant requirement, or introduce a
service mesh. Multi-tenant SaaS shapes, cross-stack distributed
transactions, OpenShift-specific support, and managed-cloud-only
production topologies are explicitly out of scope. Each subordinate
document carries its own Scope Boundary and may exclude additional
concerns relevant to its decision.

## References

- Source issue: [Agentic-Build/corporate-catering-system#47](https://github.com/Agentic-Build/corporate-catering-system/issues/47)
- Subordinate ADRs: [#48](https://github.com/Agentic-Build/corporate-catering-system/issues/48), [#49](https://github.com/Agentic-Build/corporate-catering-system/issues/49), [#50](https://github.com/Agentic-Build/corporate-catering-system/issues/50), [#51](https://github.com/Agentic-Build/corporate-catering-system/issues/51), [#52](https://github.com/Agentic-Build/corporate-catering-system/issues/52), [#53](https://github.com/Agentic-Build/corporate-catering-system/issues/53), [#54](https://github.com/Agentic-Build/corporate-catering-system/issues/54), [#55](https://github.com/Agentic-Build/corporate-catering-system/issues/55)
- Subordinate architecture specifications: [#56](https://github.com/Agentic-Build/corporate-catering-system/issues/56), [#57](https://github.com/Agentic-Build/corporate-catering-system/issues/57), [#58](https://github.com/Agentic-Build/corporate-catering-system/issues/58), [#59](https://github.com/Agentic-Build/corporate-catering-system/issues/59), [#60](https://github.com/Agentic-Build/corporate-catering-system/issues/60), [#61](https://github.com/Agentic-Build/corporate-catering-system/issues/61), [#62](https://github.com/Agentic-Build/corporate-catering-system/issues/62)
- Compliance index: [`compliance-matrix.md`](compliance-matrix.md)
