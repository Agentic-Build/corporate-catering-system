# Architecture Specification 0004 — Read Models for Employee Home, Menu, and Recommendations

- **Status**: Accepted — 2026-05-25
- **Source issue**: [Agentic-Build/corporate-catering-system#59](https://github.com/Agentic-Build/corporate-catering-system/issues/59)
- **Parent baseline**: [`00-baseline.md`](00-baseline.md)

## Context

Employee-facing surfaces of the corporate catering platform include
several high-frequency read paths: the employee home page, menu
availability, recommendations, favorites, and popularity rankings.
These surfaces are computed from many of the same transactional tables
that serve order placement and quota enforcement. Computing them
directly from transactional tables on each request creates read
amplification: a single page load can issue numerous joins and
aggregations that, in aggregate across thousands of concurrent users
during a lunch peak, make PostgreSQL the first bottleneck even when
the application tier has been scaled horizontally. The same tables
that authoritatively record orders are then under contention from both
the write path (which must remain strongly consistent) and the read
path (which would tolerate bounded staleness).

The architectural pressure is to align computation with change
frequency rather than with request frequency. Order placement and
quota enforcement must remain strongly consistent because their
correctness directly affects users; menu availability, employee home,
recommendations, and popularity may tolerate bounded eventual
consistency provided the staleness budget is small and well-defined.
Read models and caches achieve this alignment by maintaining
denormalized projections that are updated when underlying state
changes and that are served to read requests directly.

## Decision

Hot read paths use read models and caches instead of request-time
aggregation over raw transactional tables. The consistency model is
explicit and surface-specific.

## Consistency Model

- Order placement and quota: strong consistency.
- Menu availability: eventual consistency with target sub-second
  propagation.
- Employee home, recommendation, and popularity: event-driven read
  models with TTL or invalidation.
- Payroll and export: batch locking and idempotency define
  correctness.

## Rationale

Read models align computation with change frequency: orders and
supply changes update menu availability; popularity and recommendation
signals can be refreshed from events or scheduled aggregation; the
employee home page can serve a compact projection rather than a
recomputed join graph. This makes scaling behavior depend on the
number of changes and viewers, not on the cost of recomputing the
same joins for every page request. The event plane governed by
[`arch-0002`](arch-0002-durable-event-plane-and-outbox.md) provides
the substrate for read-model maintenance: domain events drive
projections; the realtime gateway governed by
[`arch-0003`](arch-0003-realtime-sse-gateway.md) drives fragment-level
invalidation at clients.

The principal alternative considered and rejected was to scale
PostgreSQL read replicas under
[`adr-0007`](adr-0007-postgres-connection-and-backup.md) and to
continue serving each page request through joins against the
transactional tables. That alternative was rejected because it would
require the application to recompute the same projections for every
viewer, which scales poorly with viewer count and which would not
relieve the database of repeated aggregation cost even after replicas
were added. A second alternative considered was to materialize the
projections in PostgreSQL itself (for example through `MATERIALIZED VIEW`
resources refreshed on a schedule). That alternative was considered
acceptable for some surfaces but rejected as the sole mechanism
because materialized-view refresh is not naturally event-driven and
because the freshness budget for menu availability is too tight to be
met by scheduled refresh alone. The decision permits a hybrid: where
materialized-view refresh suffices, it may be used; where event-driven
maintenance is required, a cache or read-model table maintained by
consumers of the event plane is used.

## Design Implications

The application introduces a read-model package at
`services/api/internal/menu/readmodel/` that maintains projections
for the employee home, menu availability, and recommendation
surfaces. Projections are keyed by plant and date per
[`adr-0008`](adr-0008-single-enterprise-plant-aware-scaling.md).
Read-side handlers query the projections rather than the transactional
tables. Invalidation is event-driven: events on the durable event
plane drive both projection updates and the realtime gateway's
topic-scoped fanout. Strong-consistency paths (order placement, quota
enforcement) continue to operate against the transactional tables and
are not weakened by this specification.

The synchronous server-side rendering path is restructured to avoid
repeated database-heavy aggregation. Each page request consults the
read-model projections and the cache layer; fallback to the
transactional path is permitted on cache miss but is subject to
rate limiting and is monitored as a degradation signal.

## Acceptance Criteria

- Employee home has a read-model/cache contract.
- Menu availability has a read-model/cache contract.
- Popularity and recommendation aggregates are not recomputed from raw orders on every request.
- Invalidation is event-driven through the durable event plane.
- Read models are keyed by plant/date where useful.
- The synchronous SSR path avoids repeated database-heavy aggregation.

## Compliance Evidence

| Acceptance criterion | Compliance |
| --- | --- |
| Employee home has a read-model/cache contract. | `services/api/internal/menu/readmodel/` defines the employee-home projection contract; per-surface implementations are exposed through the menu HTTP package. |
| Menu availability has a read-model/cache contract. | `services/api/internal/menu/readmodel/` defines the menu-availability projection contract. |
| Popularity and recommendation aggregates are not recomputed from raw orders on every request. | Popularity and recommendation projections are maintained from events on the durable event plane; per-projection implementation details are **Follow-up** as code progresses. |
| Invalidation is event-driven through the durable event plane. | The event plane governed by [`arch-0002`](arch-0002-durable-event-plane-and-outbox.md) is the source of projection updates and fragment invalidations. |
| Read models are keyed by plant/date where useful. | Per [`adr-0008`](adr-0008-single-enterprise-plant-aware-scaling.md); the read-model package uses plant and date as primary keys for menu and home projections. |
| The synchronous SSR path avoids repeated database-heavy aggregation. | The SSR path consults the read-model and cache layer first; database fallback is permitted only on miss and is observable as a degradation signal. |

## Scope Boundary

This specification does not weaken quota or order correctness. Cache
and read-model staleness applies only to surfaces whose workflows can
tolerate it. The specification does not prescribe a specific cache
backend or a specific TTL; the cache layer relies on the Valkey HA
deployment governed by [`adr-0004`](adr-0004-self-hosted-ha-data-plane.md),
and TTLs are surface-specific and supplied through configuration.

## References

- Source issue: [Agentic-Build/corporate-catering-system#59](https://github.com/Agentic-Build/corporate-catering-system/issues/59)
- Parent baseline: [`00-baseline.md`](00-baseline.md)
- Related: [`adr-0004-self-hosted-ha-data-plane.md`](adr-0004-self-hosted-ha-data-plane.md), [`adr-0007-postgres-connection-and-backup.md`](adr-0007-postgres-connection-and-backup.md), [`adr-0008-single-enterprise-plant-aware-scaling.md`](adr-0008-single-enterprise-plant-aware-scaling.md), [`arch-0002-durable-event-plane-and-outbox.md`](arch-0002-durable-event-plane-and-outbox.md), [`arch-0003-realtime-sse-gateway.md`](arch-0003-realtime-sse-gateway.md)
