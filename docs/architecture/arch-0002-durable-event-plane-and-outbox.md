# Architecture Specification 0002 — Durable Event Plane and Outbox-Only Publishing

- **Status**: Accepted — 2026-05-25
- **Source issue**: [Agentic-Build/corporate-catering-system#57](https://github.com/Agentic-Build/corporate-catering-system/issues/57)
- **Parent baseline**: [`00-baseline.md`](00-baseline.md)

## Context

The corporate catering application already uses transactional state in
PostgreSQL and a nascent outbox-style event publication pattern. This
direction should now become the formal event boundary for the platform.
In a system that must self-host, scale horizontally, and remain
correct under partial failure of either the database or the broker,
the message broker must provide durable delivery and replay while
correctness remains anchored in database transactions and idempotent
handlers. Direct publication from request handlers to a broker couples
two concerns that should be independent: domain state changes (whose
correctness is governed by the database transaction) and event
emission (whose delivery may be subject to broker availability).
Coupling these concerns causes business operations to fail when the
broker is degraded, and it permits dual-write inconsistencies where
the database commits successfully but the broker publication fails.

The architectural pressure is therefore to commit to two patterns
simultaneously: the transactional outbox, in which event intent is
recorded in the same database transaction that records the state
change, and a durable broker that provides reliable delivery, replay,
consumer-lag observability, and a dead-letter mechanism. The
combination, with idempotent handlers, achieves an exactly-once
*effect* at the storage boundary without requiring exactly-once
*delivery* from the broker.

## Decision

NATS JetStream is the durable event plane, and the outbox pattern is
the only cross-boundary business-event exit.

## Rationale

Outbox publication keeps domain state changes and event intent in one
database transaction. The outbox relay (governed by
[`arch-0001`](arch-0001-worker-role-split.md)) drains the outbox into
JetStream asynchronously, which decouples request handlers from broker
availability. JetStream then provides durable transport, consumer-lag
visibility, replay capability, and dead-letter queue (DLQ) handling.
This split avoids coupling request handlers to broker availability
while still giving asynchronous workers a scalable event substrate.
Idempotency at the handler boundary, achieved through event identifiers,
aggregate versions, unique keys, dedupe tables, or advisory locks,
permits the system to accept the broker's at-least-once delivery
semantics while preserving exactly-once effect at the storage
boundary.

The principal alternative considered and rejected was direct broker
publication from request handlers. That alternative was rejected
because it couples request success to broker availability and admits
dual-write inconsistencies between database state and broker state. A
second alternative considered was to require exactly-once delivery
from the broker through transactional message semantics. That
alternative was rejected because exactly-once delivery semantics at
the broker boundary impose performance and operational cost
disproportionate to the benefit, and because exactly-once effect at
the storage boundary (through handler idempotency) is sufficient. A
third alternative considered was Kafka as the broker. That
alternative was rejected because the NATS JetStream selection in
[`adr-0004`](adr-0004-self-hosted-ha-data-plane.md) provides the
required durability properties with a lighter operational footprint
better suited to the self-hosted profile.

## Design Commitments

The following commitments follow from the decision:

- NATS runs as a clustered self-hosted service in production, per
  [`adr-0004`](adr-0004-self-hosted-ha-data-plane.md).
- JetStream streams use production HA replication.
- Durable consumers are used where replay matters.
- DLQ streams are part of the event contract.
- Stream and consumer provisioning is handled by a Kubernetes Job or
  controller, per
  [`arch-0007`](arch-0007-cloud-native-readiness-and-autoscaling.md).
- Business transactions write database state and outbox rows
  atomically within a single PostgreSQL transaction.
- API handlers do not directly publish business events.

The chart introduces `chart/tbite-platform/templates/job-provision-streams.yaml`
to materialize streams and consumers, and
`chart/tbite-platform/templates/vmalert-rules.yaml` to alert on
consumer lag and DLQ counts. The outbox relay role declared in
[`arch-0001`](arch-0001-worker-role-split.md) is the sole writer to
JetStream from the application boundary.

## Acceptance Criteria

- Outbox relay can scale horizontally without duplicate side effects.
- Event handlers are idempotent through event IDs, aggregate versions, unique keys, dedupe tables, or advisory locks.
- Consumer lag and DLQ counts are observable and alertable.
- Stream provisioning is explicit and repeatable.
- Broker outage does not make ordinary order placement publish directly from request handlers.

## Compliance Evidence

| Acceptance criterion | Compliance |
| --- | --- |
| Outbox relay can scale horizontally without duplicate side effects. | The outbox relay role is deployed at `chart/tbite-platform/templates/deployment-worker-outbox-relay.yaml` (under [`arch-0001`](arch-0001-worker-role-split.md)); duplicate suppression is achieved through outbox row claim semantics and JetStream message IDs. |
| Event handlers are idempotent through event IDs, aggregate versions, unique keys, dedupe tables, or advisory locks. | Idempotency is a contract observed in handler implementations; per-handler documentation under `docs/` is **Follow-up**. |
| Consumer lag and DLQ counts are observable and alertable. | `chart/tbite-platform/templates/vmalert-rules.yaml` declares alerts for consumer lag and DLQ depth; metrics flow through the OpenTelemetry Collector under [`adr-0005`](adr-0005-victoria-observability-stack.md). |
| Stream provisioning is explicit and repeatable. | `chart/tbite-platform/templates/job-provision-streams.yaml` runs as a Kubernetes Job on chart install and upgrade, creating and reconciling streams and consumers idempotently. |
| Broker outage does not make ordinary order placement publish directly from request handlers. | API handlers write to the transactional outbox only; the outbox relay performs the actual JetStream publication asynchronously. |

## Scope Boundary

The system does not rely on exactly-once delivery from the broker.
Exactly-once effect is achieved at handler and storage boundaries
through idempotency. This specification does not prescribe specific
stream names, consumer parameters, or retention windows; those are
implementation-level concerns guided by this decision rather than
prescribed by it.

## References

- Source issue: [Agentic-Build/corporate-catering-system#57](https://github.com/Agentic-Build/corporate-catering-system/issues/57)
- Parent baseline: [`00-baseline.md`](00-baseline.md)
- Related: [`adr-0004-self-hosted-ha-data-plane.md`](adr-0004-self-hosted-ha-data-plane.md), [`adr-0005-victoria-observability-stack.md`](adr-0005-victoria-observability-stack.md), [`arch-0001-worker-role-split.md`](arch-0001-worker-role-split.md), [`arch-0007-cloud-native-readiness-and-autoscaling.md`](arch-0007-cloud-native-readiness-and-autoscaling.md)
