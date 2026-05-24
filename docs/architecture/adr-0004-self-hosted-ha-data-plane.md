# ADR 0004 — Self-Hosted High-Availability Data Plane Providers

- **Status**: Accepted — 2026-05-25
- **Source issue**: [Agentic-Build/corporate-catering-system#51](https://github.com/Agentic-Build/corporate-catering-system/issues/51)
- **Parent baseline**: [`00-baseline.md`](00-baseline.md)

## Context

The product's self-hostable claim is meaningful only if the production
data plane has a first-class self-hosted topology. Development
environments can run a smaller stack and may legitimately reduce
component topology to a minimum viable configuration; production
environments, however, must provide high availability, scheduled
backups, dedicated storage isolation, observability, and explicit
ownership for each stateful service. Managed cloud services may be
convenient drop-in replacements for individual components, and the
chart governed by [`adr-0002`](adr-0002-helm-umbrella-chart.md) permits
them through BYO mode, but they cannot be the only complete production
path without undermining the self-hostable claim.

The architectural pressure is therefore to select a single canonical
self-hosted provider for each stateful concern (transactional state,
session and cache state, durable asynchronous events, S3-compatible
object storage) and to commit the chart to that selection.
Narrowing the design surface in this way pays compound dividends:
backup procedures, observability dashboards, readiness probes, capacity
planning notes, and incident playbooks can each be written against a
single concrete provider rather than against an open-ended set of
candidates.

## Decision

The canonical self-hosted data plane uses the following providers:

- **PostgreSQL**: CloudNativePG operator with primary plus replicas in
  production topologies.
- **Connection pooling**: PgBouncer in front of PostgreSQL.
- **Cache and session store**: Valkey, deployed in high-availability
  topology (initially Sentinel-style; cluster sharding is reserved for
  later, evidence-driven decisions).
- **Messaging**: the NATS Helm chart with JetStream enabled.
- **Object storage**: MinIO Operator in distributed mode.

## Rationale

These components match the application's existing boundaries.
PostgreSQL is already the transactional source of truth and remains so.
A Redis-compatible store is already used for sessions, caches, and
rate-limiting state, and Valkey provides a true open-source successor
to that contract. NATS JetStream is already used for domain events and
remains so. S3-compatible object storage is already used for files and
remains so. Selecting canonical providers narrows the design surface
for charts, backup procedures, monitoring dashboards, readiness probes,
and capacity planning notes, and it permits each concern to be modeled
against one provider rather than against a generic abstraction.

The principal alternative considered and rejected was to keep the data
plane abstract and to require operators to bring their own providers
for each stateful concern. That alternative was rejected because it
would have shifted the burden of integration testing onto each
customer, would have prevented the chart from offering meaningful
default backup and observability behavior, and would have effectively
made the product an application-only deliverable rather than a
self-hostable platform. A second alternative considered was to select
different providers for some concerns (for example, PostgreSQL
operators other than CloudNativePG, or single-binary cache stores
other than Valkey HA). Those alternatives were not selected because
CloudNativePG, the NATS Helm chart, MinIO Operator, and Valkey HA are
the operators with the most mature operational behaviors aligned with
the platform's requirements; specifically, CloudNativePG supports
declarative backup hooks and physical replication, and MinIO Operator
provides distributed mode with explicit tenant resources.

## Design Commitments

The following commitments follow from the decision and are realized in
the chart:

- PostgreSQL is provided by CloudNativePG.
- PgBouncer is the connection pooler in front of PostgreSQL.
- Valkey is deployed in HA topology, Sentinel-style, until workload
  evidence justifies cluster sharding.
- The NATS Helm chart is deployed with JetStream enabled and clustered.
- MinIO is deployed via the MinIO Operator in distributed mode.
- Storage relies on dynamic provisioning, a production SSD-capable RWO
  StorageClass where applicable, and a `VolumeSnapshotClass` for
  production backup workflows.

Production values declare HA topology, storage classes,
`PodDisruptionBudget` resources, resource requests, and backup hooks
for each stateful component. Development values use the same service
contracts with smaller topology. Managed replacements can be supplied
through BYO values without application-code changes.

## Acceptance Criteria

- Production values define HA topology, storage classes, PDBs, resource requests, and backup hooks for each stateful component.
- Development values use the same service contracts with smaller topology.
- Managed replacements can be supplied through BYO values without app-code changes.
- Each data-plane component has dashboards, alerts, and restore documentation.

## Compliance Evidence

| Acceptance criterion | Compliance |
| --- | --- |
| Production values define HA topology, storage classes, PDBs, resource requests, and backup hooks for each stateful component. | `chart/tbite-platform/values-prod.yaml` declares HA topology, storage classes, and resource requests; `chart/tbite-platform/templates/cnpg-cluster.yaml`, `chart/tbite-platform/templates/cnpg-pooler.yaml`, and `chart/tbite-platform/templates/cnpg-backup-schedule.yaml` render the CloudNativePG cluster, pooler, and scheduled backup resources. |
| Development values use the same service contracts with smaller topology. | `chart/tbite-platform/values-dev.yaml` reduces topology while preserving the same service contracts; pinned dependency versions in `chart/tbite-platform/Chart.yaml` are shared with production. |
| Managed replacements can be supplied through BYO values without app-code changes. | BYO endpoints are exposed through `chart/tbite-platform/values.yaml` and validated by `chart/tbite-platform/values.schema.json`; application code consumes the same connection URLs regardless of source. |
| Each data-plane component has dashboards, alerts, and restore documentation. | `chart/tbite-platform/templates/vmalert-rules.yaml` provides alerts for data-plane saturation; dashboards are deployed via the Victoria stack governed by [`adr-0005`](adr-0005-victoria-observability-stack.md). Restore documentation for CloudNativePG is a **Follow-up** to be added under `docs/deployment/`. |

## Scope Boundary

Single-pod PostgreSQL, Redis or Valkey, NATS, or MinIO instances may
exist only in local or explicitly non-production values. They are not
the production baseline. This ADR does not decide the precise number of
replicas, the precise storage size, or the precise resource requests,
each of which is environment-dependent and supplied through values.

## References

- Source issue: [Agentic-Build/corporate-catering-system#51](https://github.com/Agentic-Build/corporate-catering-system/issues/51)
- Parent baseline: [`00-baseline.md`](00-baseline.md)
- Related: [`adr-0002-helm-umbrella-chart.md`](adr-0002-helm-umbrella-chart.md), [`adr-0005-victoria-observability-stack.md`](adr-0005-victoria-observability-stack.md), [`adr-0007-postgres-connection-and-backup.md`](adr-0007-postgres-connection-and-backup.md), [`arch-0002-durable-event-plane-and-outbox.md`](arch-0002-durable-event-plane-and-outbox.md), [`arch-0005-direct-object-storage-path.md`](arch-0005-direct-object-storage-path.md)
