# Worker & scheduler roles — idempotency, retry, and DLQ

Companion to [`arch-0001-worker-role-split.md`](arch-0001-worker-role-split.md)
(#56) and [`arch-0002-durable-event-plane-and-outbox.md`](arch-0002-durable-event-plane-and-outbox.md)
(#57). It documents, per role, the scaling signal, idempotency
mechanism, retry behaviour, and dead-letter handling — the
acceptance-criteria items those issues defer to "per-role
documentation".

The generic worker is split into seven independent Deployments. Each is
one process (`--role=<name>`), wired in
[`services/api/cmd/tbite/roles.go`](../../services/api/cmd/tbite/roles.go),
with its own `/healthz` + `/readyz` on `PROBE_ADDR` (`:2112`).

## Queue / event-driven workers (KEDA-scaled)

These are horizontally scalable. KEDA `ScaledObject`s
(`chart/tbite-platform/templates/scaledobject-*.yaml`) drive replica
count from backlog, not CPU.

| Role | Scaling signal | KEDA scaler | Bounds |
| --- | --- | --- | --- |
| `outbox-relay` | unpublished outbox rows: `SELECT count(*) FROM outbox_event WHERE published_at IS NULL` ≥ 100 | `postgresql` | 1 → 6 |
| `payroll-settler` | `PAYROLL_V1` consumer `payroll-settler` lag ≥ 500 | `nats-jetstream` | 1 → 4 |
| `on-time-evaluator` | `ORDERS_V1` consumer `on-time-evaluator` lag ≥ 500 | `nats-jetstream` | 1 → 4 |

### outbox-relay

- **Idempotency / horizontal safety.** Each cycle calls
  `Outbox.LockBatch` to claim a batch of unpublished rows under a
  transaction (row-level advisory lock), so concurrent replicas take
  disjoint batches. A row is marked `published_at` only after a
  successful JetStream publish (`MarkPublished`); a failed publish
  leaves `published_at = NULL` and increments `attempts`
  (`MarkFailed`). One row is therefore published **at-least-once**, and
  duplicates downstream are expected — consumers dedupe.
- **Retry.** Failed rows stay claimable and are retried on the next
  cycle; the relay never drops a row.
- **DLQ.** The relay itself has no DLQ — an unpublishable row simply
  remains pending and visible via the `outbox_event` backlog metric
  and the consumer-lag alerts.

### payroll-settler / on-time-evaluator (JetStream consumers)

- **Delivery.** Durable JetStream consumers over `PAYROLL_V1` /
  `ORDERS_V1`. Delivery is at-least-once; **exactly-once effect is
  achieved at the storage boundary** (unique keys / aggregate version /
  dedupe rows), per #57's scope boundary.
- **Retry.** JetStream redelivers un-acked messages (`AckWait` /
  `MaxDeliver`).
- **DLQ.** When `MaxDeliver` is exceeded, or a message is judged
  irrecoverable, the worker calls
  [`messaging.WriteDLQ`](../../services/api/internal/platform/messaging/dlq.go)
  to persist it to the `dlq_message` table (source stream/subject/
  consumer + payload + last error). Operators inspect and replay via
  `GET /api/admin/dlq`. DLQ depth is alertable.

## Scheduler singletons (lease-elected)

These are **not** horizontally scaled. Each runs `replicaCount: 1` and
acquires a distinct `coordination.k8s.io/Lease`
(`runWithLeaseSingleton`), so exactly one replica is active and a
restart fails over independently. Each tick is idempotent: it sweeps
state inside a time window and writes results transactionally
(including outbox rows), so re-running a tick is safe.

| Role | Lease | Cadence (default; env-overridable) |
| --- | --- | --- |
| `cutoff-sweeper` | `tbite-cutoff-sweeper` | every 60s (`CUTOFF_INTERVAL`) |
| `no-show-sweeper` | `tbite-no-show-sweeper` | every 5m, age > 12h (`NO_SHOW_INTERVAL` / `NO_SHOW_MAX_AGE`) |
| `document-expiry-scanner` | `tbite-doc-expiry-scanner` | every 1h, 14-day window (`DOC_EXPIRY_INTERVAL`) |
| `feedback-scanner` | `tbite-feedback-scanner` | every 1h, 14-day window (`FEEDBACK_SCAN_INTERVAL` / `FEEDBACK_SCAN_WINDOW`) |

## Event plane

Streams are declared by the `provision-streams` Job
(`--role=provision-streams`, `CreateOrUpdateStream` — idempotent and
repeatable), not by worker startup:

| Stream | Subjects | Retention |
| --- | --- | --- |
| `ORDERS_V1` | `order.>` | 30 days |
| `PAYROLL_V1` | `payroll.>` | 90 days |

> **Known limitation (#57 follow-up).** `ProvisionStreams` currently
> sets `Replicas: 1` for both streams
> (`services/api/internal/platform/messaging/nats.go`). On a clustered
> production NATS this leaves the streams unreplicated, which does not
> yet meet #57's "JetStream streams use production HA replication"
> commitment. Sourcing stream replicas from a value is tracked
> separately.
