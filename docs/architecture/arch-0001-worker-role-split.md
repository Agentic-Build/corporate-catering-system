# Architecture Specification 0001 — Worker Role Split into Independent Deployments

- **Status**: Accepted — 2026-05-25
- **Source issue**: [Agentic-Build/corporate-catering-system#56](https://github.com/Agentic-Build/corporate-catering-system/issues/56)
- **Parent baseline**: [`00-baseline.md`](00-baseline.md)

## Context

The current implementation runs a single worker process that combines
several background tasks whose ownership boundaries and scaling
properties differ materially. Outbox relay is inherently parallelizable
and benefits from horizontal replication when outbox age increases.
Payroll settlement, by contrast, may require batch-level locking and
cannot be safely parallelized across instances without a leader-lease
mechanism. Compliance evaluators may maintain in-memory state across
many events and therefore have a different restart profile than
stateless relays. Periodic sweepers (cutoff sweeper, no-show sweeper,
document-expiry scanner) are naturally singleton or lease-driven.
Treating these tasks as one deployment hides backpressure between
unrelated workloads, conflates failure domains, and prevents
horizontally-scalable work from absorbing load without dragging
singleton work along with it.

A monolithic worker also fails the autoscaling discipline established
by [`arch-0007`](arch-0007-cloud-native-readiness-and-autoscaling.md):
the correct scaling signal for outbox relay is outbox age, for queue
workers is consumer lag, and for sweepers is wall-clock schedule
adherence rather than load. A single deployment cannot consume more
than one of these signals coherently. The architectural pressure is
therefore to decompose the worker by role so that each role can carry
its own scaling policy, failure boundary, and operational ownership.

## Decision

The generic worker model is replaced by role-specific worker
deployments. Each role is realized as a distinct Kubernetes Deployment
(or a singleton Deployment with lease-based leader election where
required) and carries its own scaling rule, readiness contract,
metrics, resource requests, and failure domain.

## Worker Roles

The following roles are defined:

- `outbox-relay` — drains the transactional outbox introduced in
  [`arch-0002`](arch-0002-durable-event-plane-and-outbox.md) into
  NATS JetStream.
- `payroll-settler` — performs payroll settlement under batch-level
  locking.
- `on-time-evaluator` — evaluates on-time delivery and order
  completion signals.
- `cutoff-sweeper` — closes ordering windows at their configured
  cutoff times.
- `no-show-sweeper` — reconciles orders that were not collected.
- `document-expiry-scanner` — scans for expiring compliance documents.
- `feedback/anomaly-scanner` — scans feedback streams for anomalies.

## Rationale

Independent deployments make ownership visible at the Kubernetes
resource level. Each worker can define its own concurrency,
idempotency, retry policy, metrics, autoscaling signal, and failure
domain. A slow payroll export does not delay outbox publishing because
the two are scheduled and scaled independently; a compliance
evaluator restart does not alter scheduler behavior because the
scheduler runs in a separate pod. Horizontally scalable roles (outbox
relay, document scanners that can shard their work) can be scaled by
KEDA against queue or backlog metrics; singleton or lease-owned roles
(payroll settlement, cutoff sweepers) can be run with a fixed replica
count of one and a lease mechanism to prevent multiple leaders.

The principal alternative considered and rejected was to retain a
single worker process and to assign concurrency budgets to each task
within it. That alternative was rejected because it would couple
restart, deployment, and autoscaling decisions across unrelated
workloads; would prevent each task from emitting its own ready-state
signal; and would obscure operational ownership when an incident
affects one of the tasks but not the others. A second alternative
considered was to use Kubernetes CronJobs for periodic work and a
single deployment for streaming work. That alternative was partially
adopted (Jobs are used for migrations and provisioning under
[`arch-0007`](arch-0007-cloud-native-readiness-and-autoscaling.md)) but
was rejected for ongoing worker concerns because long-running stream
consumption is poorly served by CronJob semantics and because role
splitting at the Deployment level provides clearer operational
ownership.

## Design Implications

Each role's Deployment template is realized as
`chart/tbite-platform/templates/deployment-worker-<role>.yaml`, and
each scheduler role as
`chart/tbite-platform/templates/deployment-scheduler-<role>.yaml`.
Roles that are queue-backed and scale on backlog signals declare a
KEDA `ScaledObject` at
`chart/tbite-platform/templates/scaledobject-<role>.yaml`. Singleton
roles run at one replica and use lease-based leader election where the
underlying work demands it. Alerts for outbox age, consumer lag, and
role-specific backlog are declared in
`chart/tbite-platform/templates/vmalert-rules.yaml`.

The application binary is restructured so that each role is selected
through a command-line subcommand or a `ROLE` environment variable
honored by `services/api/cmd/tbite/main.go`. Each role registers only
its own startup dependencies and only its own readiness probe so that
the per-role readiness contract required by
[`arch-0007`](arch-0007-cloud-native-readiness-and-autoscaling.md) is
honored.

## Acceptance Criteria

- Each role has its own deployment, health checks, metrics, resource requests, and scaling rule.
- Each role documents idempotency, retry, and DLQ behavior.
- Horizontally scalable roles are separated from singleton or lease-owned roles.
- A slow or failing background task cannot stall unrelated async work.
- KEDA or custom metrics can scale queue-backed roles using backlog or lag signals.

## Compliance Evidence

| Acceptance criterion | Compliance |
| --- | --- |
| Each role has its own deployment, health checks, metrics, resource requests, and scaling rule. | `chart/tbite-platform/templates/deployment-worker-*.yaml` and `chart/tbite-platform/templates/deployment-scheduler-*.yaml` declare one Deployment per role; per-role health checks are wired through `services/api/internal/httpserver/health.go`. |
| Each role documents idempotency, retry, and DLQ behavior. | The event plane and DLQ contract are defined in [`arch-0002`](arch-0002-durable-event-plane-and-outbox.md); per-role idempotency and retry documentation under `docs/` is **Follow-up**. |
| Horizontally scalable roles are separated from singleton or lease-owned roles. | Horizontally scalable roles declare KEDA `ScaledObject` resources at `chart/tbite-platform/templates/scaledobject-*.yaml`; singleton roles run with `replicas: 1` and lease-based leader election in `services/api/cmd/tbite/main.go`. |
| A slow or failing background task cannot stall unrelated async work. | Roles are deployed independently and scaled independently; failure of one Deployment does not affect another. |
| KEDA or custom metrics can scale queue-backed roles using backlog or lag signals. | `chart/tbite-platform/templates/scaledobject-*.yaml` defines KEDA scalers against NATS JetStream consumer lag and outbox backlog. |

## Scope Boundary

This specification defines worker ownership boundaries. It does not
dictate implementation order or require every role to scale
horizontally on day one. It does not select specific concurrency
budgets or retry parameters; those are role-level concerns guided by
this decision rather than prescribed by it.

## References

- Source issue: [Agentic-Build/corporate-catering-system#56](https://github.com/Agentic-Build/corporate-catering-system/issues/56)
- Parent baseline: [`00-baseline.md`](00-baseline.md)
- Related: [`arch-0002-durable-event-plane-and-outbox.md`](arch-0002-durable-event-plane-and-outbox.md), [`arch-0007-cloud-native-readiness-and-autoscaling.md`](arch-0007-cloud-native-readiness-and-autoscaling.md), [`adr-0005-victoria-observability-stack.md`](adr-0005-victoria-observability-stack.md)
