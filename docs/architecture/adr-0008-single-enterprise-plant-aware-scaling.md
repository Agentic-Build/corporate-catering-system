# ADR 0008 — Single-Enterprise Stack and Plant-Aware Scaling Model

- **Status**: Accepted — 2026-05-25
- **Source issue**: [Agentic-Build/corporate-catering-system#55](https://github.com/Agentic-Build/corporate-catering-system/issues/55)
- **Parent baseline**: [`00-baseline.md`](00-baseline.md)

## Context

The corporate catering platform is deployed as one enterprise per
stack. The product contract is therefore not a multi-tenant
software-as-a-service control plane; it is a self-hosted installation
addressed to one enterprise customer. This product shape removes the
need for a SaaS-style tenant control plane and, with it, the
operational complexity that such a plane implies: a tenant onboarding
service, a tenant-aware authorization layer, a cross-tenant resource
governance plane, and a billing-and-quota plane. The architectural
pressure is therefore to choose an internal partition key that
matches the natural scaling dimensions of one enterprise installation
rather than to adopt a tenant identifier that would not carry semantic
weight within the deployment boundary.

The natural internal dimensions of one enterprise installation are
plants, vendors, dates, and order volume. Menu availability ranges
over plant and date. Ordering windows are plant-specific. Realtime
invalidation is naturally scoped to plant and date because employees
typically belong to a single plant and care about today's menu.
Operational dashboards aggregate per plant, per vendor, and per date.
Many read models naturally key on the same dimensions.

## Decision

The baseline is a single-enterprise stack with plant-aware internal
routing, caching, partitioning, and realtime topic design where useful.

## Rationale

A `tenant_id` shard key would add complexity that the product model
does not require. Introducing a tenant identifier when no second
tenant exists in a deployment would force every query, every cache
key, every event, and every realtime topic to carry an
operationally-irrelevant dimension; would require an authorization
plane that distinguishes tenants in a system where the entire stack
belongs to one customer; and would suggest, misleadingly, that
cross-tenant operations are a supported workflow. Plant is the more
useful internal boundary. Menu availability, ordering windows,
realtime invalidation, operational dashboards, and many read models
naturally group by plant and date. Selecting plant and date as the
dominant internal keys keeps the architecture simpler while still
leaving room for large enterprise installations to scale internally:
plant-level partitioning of high-volume tables, plant-scoped cache
keys, and plant-scoped realtime topics all reduce contention and
fanout.

The principal alternative considered and rejected was to introduce a
`tenant_id` from the outset on the grounds that doing so would
preserve optionality should the product later be repositioned as a
multi-tenant SaaS. That alternative was rejected because preserving
optionality of that kind imposes pervasive cost on every part of the
system that touches data, and because a future repositioning of the
product is a separate, larger decision that would entail many other
architectural changes besides the addition of a tenant identifier. A
second alternative considered was to choose vendor or date as the
dominant key. That alternative was rejected because vendor and date
alone do not match the realtime fanout pattern (employees care about
their plant's menu) and because both are subordinate to plant in the
natural query patterns observed across the platform.

## Design Implications

Schema design should index or partition by plant and date where it
supports high-volume paths, particularly menu availability, order
tables, and read-model tables. Cache keys and read-model keys must
include plant and date for menu and home surfaces governed by
[`arch-0004`](arch-0004-read-models-and-caching.md). Realtime topics
governed by [`arch-0003`](arch-0003-realtime-sse-gateway.md) must
include plant and date at minimum, and may include vendor, menu item,
or order identifier where useful. Cross-plant reporting can use
asynchronous aggregation when immediate consistency is not required by
the workflow.

Because the product is one enterprise per stack, the application does
not need to enforce a tenant isolation boundary at the authorization
layer. Authentication and authorization concerns governed by
[`arch-0006`](arch-0006-authentik-hydra-identity-boundary.md) operate
within the enterprise's identity provider rather than against an
internal tenant catalogue.

## Acceptance Criteria

- The architecture does not require SaaS tenancy assumptions.
- Plant/date scoping is available for read models, realtime fanout, and operational metrics.
- Order, quota, menu, and vendor data stay within one enterprise stack boundary.
- Future multi-stack reporting can be added through asynchronous export or aggregation.

## Compliance Evidence

| Acceptance criterion | Compliance |
| --- | --- |
| The architecture does not require SaaS tenancy assumptions. | The chart governed by [`adr-0002`](adr-0002-helm-umbrella-chart.md) is parameterized for a single enterprise; no `tenant_id` shard key is introduced in application schema or templates. |
| Plant/date scoping is available for read models, realtime fanout, and operational metrics. | The read-model package at `services/api/internal/menu/readmodel/` keys entries by plant and date; realtime topics are scoped by plant and date in the realtime gateway governed by [`arch-0003`](arch-0003-realtime-sse-gateway.md). |
| Order, quota, menu, and vendor data stay within one enterprise stack boundary. | Application data is held in the PostgreSQL cluster governed by [`adr-0007`](adr-0007-postgres-connection-and-backup.md); no cross-stack data plane is introduced. |
| Future multi-stack reporting can be added through asynchronous export or aggregation. | The event plane governed by [`arch-0002`](arch-0002-durable-event-plane-and-outbox.md) provides a durable export surface; multi-stack aggregation is a **Follow-up** for future product decisions. |

## Scope Boundary

This ADR does not design a multi-tenant SaaS platform. It does not
require cross-stack distributed transactions. It does not select a
specific partitioning scheme for any particular table; partition
design is a schema-level concern guided by this decision rather than
prescribed by it.

## References

- Source issue: [Agentic-Build/corporate-catering-system#55](https://github.com/Agentic-Build/corporate-catering-system/issues/55)
- Parent baseline: [`00-baseline.md`](00-baseline.md)
- Related: [`arch-0003-realtime-sse-gateway.md`](arch-0003-realtime-sse-gateway.md), [`arch-0004-read-models-and-caching.md`](arch-0004-read-models-and-caching.md), [`arch-0006-authentik-hydra-identity-boundary.md`](arch-0006-authentik-hydra-identity-boundary.md)
