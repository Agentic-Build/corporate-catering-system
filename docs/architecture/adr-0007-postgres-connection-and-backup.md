# ADR 0007 — PostgreSQL Connection, Read/Write Routing, and Backup Architecture

- **Status**: Accepted — 2026-05-25
- **Source issue**: [Agentic-Build/corporate-catering-system#54](https://github.com/Agentic-Build/corporate-catering-system/issues/54)
- **Parent baseline**: [`00-baseline.md`](00-baseline.md)

## Context

Horizontal scaling of application processes reduces request pressure
per pod, but it can simultaneously amplify pressure on PostgreSQL.
PostgreSQL is process-per-connection; each additional connection
consumes a non-trivial amount of backend memory and scheduling
overhead, and a high connection count can degrade throughput even when
no individual connection is heavily used. The corporate catering
platform's current codebase already exposes the risk: database
connection pool limits are fixed in the application source, while
deployment-level scaling can add many API, worker, and scheduler
processes whose collective demand on PostgreSQL is not bounded by any
shared budget.

The architectural pressure is therefore twofold. First, the
application's connection topology must be made explicit so that
scaling the number of pods does not implicitly scale the number of
PostgreSQL backend processes. Second, the application must distinguish
read-only paths from read-write paths so that high-volume read traffic
can be routed to read replicas where the consistency model permits,
relieving the primary of unnecessary load. Finally, because PostgreSQL
holds the transactional source of truth, recovery objectives must be
explicit product properties rather than implicit operational
aspirations.

## Decision

All application roles connect to PostgreSQL through PgBouncer, and the
application contract includes read/write database routing.

## Rationale

PgBouncer protects PostgreSQL from connection storms and decouples
application concurrency from database backend process counts. With
PgBouncer in front, the application may legitimately operate many
short-lived sessions while PgBouncer multiplexes them onto a small,
bounded set of backend connections. Read replicas allow high-volume
read paths to scale away from the primary when consistency
requirements permit; for example, the read-model paths governed by
[`arch-0004`](arch-0004-read-models-and-caching.md) can target a
read-only endpoint without compromising correctness. Point-in-time
recovery (PITR) and the associated recovery objectives make database
restoration an explicit, measurable product property rather than an
unstated assumption.

The principal alternative considered and rejected was to omit PgBouncer
and to scale the application pool sizes directly against the primary.
That alternative was rejected because it would either constrain
application concurrency to whatever the primary's `max_connections`
budget admits or, conversely, would expose the primary to connection
storms during pod restarts and scaling events. A second alternative
considered was to require all reads, including those served by the
read models, to target the primary. That alternative was rejected
because it would prevent the platform from absorbing the read pressure
implied by [`arch-0004`](arch-0004-read-models-and-caching.md) and
would re-introduce the bottleneck the read-model decision was intended
to relieve.

## Design Commitments

The decision yields the following commitments, all realized in the
chart governed by [`adr-0002`](adr-0002-helm-umbrella-chart.md):

- Production topology estimate: CloudNativePG with one primary and two
  replicas, with asynchronous physical replication.
- PgBouncer is mandatory in front of PostgreSQL.
- pgx pool sizes are configurable and derived from the database
  connection budget rather than hard-coded in the application source.
- `DATABASE_RW_URL` and `DATABASE_RO_URL` are first-class application
  contracts.
- Production requires PITR.
- Target recovery objectives: RPO at most 5 minutes, RTO at most 30
  minutes.

The application is modified to honor the two-URL contract. The
configuration loader at `services/api/internal/config/config.go`
introduces `DATABASE_RO_URL` and a pool budget environment variable;
the database initialization at
`services/api/internal/platform/db/pgx.go` derives pool size from the
configured budget. Each role's connection-pool size is bounded so that
the sum across replicas does not exceed the PgBouncer-and-primary
connection budget. Read-only paths target `DATABASE_RO_URL`; the
remainder of the application targets `DATABASE_RW_URL`.

## Acceptance Criteria

- Application code no longer assumes only DATABASE_RW_URL.
- Read-heavy paths can route to read replicas where their consistency model permits it.
- HPA/KEDA limits account for database connection budget.
- Backup and restore drills are documented for production values.
- Dashboards expose pool saturation, primary load, replica lag, query latency, and deadlocks.

## Compliance Evidence

| Acceptance criterion | Compliance |
| --- | --- |
| Application code no longer assumes only DATABASE_RW_URL. | `services/api/internal/config/config.go` introduces `DATABASE_RO_URL` alongside the existing `DATABASE_RW_URL`; `services/api/internal/platform/db/pgx.go` constructs both pools from configuration. |
| Read-heavy paths can route to read replicas where their consistency model permits it. | The read-model package at `services/api/internal/menu/readmodel/` and the surfaces governed by [`arch-0004`](arch-0004-read-models-and-caching.md) consume `DATABASE_RO_URL`. |
| HPA/KEDA limits account for database connection budget. | `chart/tbite-platform/templates/hpa-*.yaml` and `chart/tbite-platform/templates/scaledobject-*.yaml` bound replica counts; pool size environment variables in the deployment templates derive from the database connection budget. |
| Backup and restore drills are documented for production values. | `chart/tbite-platform/templates/cnpg-backup-schedule.yaml` declares scheduled backups; backup and restore drill documentation under `docs/deployment/` is **Follow-up**. |
| Dashboards expose pool saturation, primary load, replica lag, query latency, and deadlocks. | `chart/tbite-platform/templates/vmalert-rules.yaml` declares alerts for these conditions; dashboards are deployed via the Victoria stack governed by [`adr-0005`](adr-0005-victoria-observability-stack.md). |

## Scope Boundary

This ADR does not introduce distributed SQL or synchronous cross-stack
transactions. Order and quota correctness remain inside a single
PostgreSQL transactional boundary. The ADR does not prescribe specific
pool sizes, specific replica counts, or specific backup retention
windows; these are environment-dependent and supplied through values.

## References

- Source issue: [Agentic-Build/corporate-catering-system#54](https://github.com/Agentic-Build/corporate-catering-system/issues/54)
- Parent baseline: [`00-baseline.md`](00-baseline.md)
- Related: [`adr-0004-self-hosted-ha-data-plane.md`](adr-0004-self-hosted-ha-data-plane.md), [`adr-0005-victoria-observability-stack.md`](adr-0005-victoria-observability-stack.md), [`arch-0004-read-models-and-caching.md`](arch-0004-read-models-and-caching.md), [`arch-0007-cloud-native-readiness-and-autoscaling.md`](arch-0007-cloud-native-readiness-and-autoscaling.md)
