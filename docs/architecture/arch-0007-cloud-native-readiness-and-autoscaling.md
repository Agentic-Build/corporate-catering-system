# Architecture Specification 0007 — Cloud-Native Readiness, Migration, Provisioning, and Autoscaling Contracts

- **Status**: Accepted — 2026-05-25
- **Source issue**: [Agentic-Build/corporate-catering-system#62](https://github.com/Agentic-Build/corporate-catering-system/issues/62)
- **Parent baseline**: [`00-baseline.md`](00-baseline.md)

## Context

Cloud-native operation requires explicit lifecycle ownership. Three
operational facts must be encoded in the deployment baseline. First, a
pod that is listening on its TCP port is not necessarily ready to
serve traffic: readiness must depend on the availability of the
specific downstream dependencies that the pod's role actually
requires. Second, application processes should not secretly repair
infrastructure state during startup; database migrations, message
stream provisioning, and object storage bucket bootstrap are
infrastructure operations whose success or failure should be visible
as Kubernetes resources, not as opaque application logs. Third, CPU
utilization alone is a weak signal for queue-backed and event-driven
workers; meaningful autoscaling for such workers requires
workload-aware signals such as queue depth, outbox age, consumer lag,
or active connection count.

The architectural pressure is therefore to express each of these
operational facts as a first-class artifact: per-role readiness probes
that check the right dependencies, Kubernetes Jobs for migration and
provisioning, and autoscaling resources (HPA or KEDA `ScaledObject`)
that consume workload-aware metrics.

## Decision

Operational lifecycle tasks are expressed as explicit Kubernetes
resources, and readiness and autoscaling use workload-aware signals.

## Rationale

Role-specific readiness protects users from partial outages. An API
pod whose database dependency is unavailable should not be marked
ready, regardless of whether its TCP port is open; a realtime gateway
pod whose JetStream subscription has failed should not be marked
ready, regardless of whether its TCP port is open. Kubernetes Jobs
make migration, stream provisioning, and bucket bootstrap observable
and repeatable: success appears as a completed Job, failure appears
as a failed Job, and the operator workflow is identical across
environments. Workload-aware autoscaling lets the system respond to
real pressure: request concurrency for API and SSR, outbox age for
event publication, consumer lag for queue workers, and active
connection count for the realtime gateway governed by
[`arch-0003`](arch-0003-realtime-sse-gateway.md).

The principal alternative considered and rejected was to retain a
shallow `/readyz` endpoint that simply confirms the HTTP server is
running. That alternative was rejected because it provides no
protection against the partial-outage mode in which a pod is process-
healthy but cannot serve requests due to a downstream dependency
failure. A second alternative considered was to perform migrations
and stream provisioning during application startup. That alternative
was rejected because conflating data-plane mutation with application
startup makes startup non-idempotent, hides failure modes inside
application logs, and produces race conditions when multiple replicas
start simultaneously. A third alternative considered was to scale all
workloads on CPU utilization alone. That alternative was rejected
because queue-backed workers exhibit low CPU utilization while
falling behind on real work and would never scale up under CPU
signals; conversely, a CPU-bound activity unrelated to backlog could
cause needless scale-up.

## Design Commitments

The decision yields the following commitments, realized in the chart
and the application binary:

- `/readyz` checks required dependencies per role.
- Database migrations run as Kubernetes Jobs.
- NATS stream and consumer provisioning runs as a Kubernetes Job or
  controller.
- Bucket and bootstrap validation runs outside ordinary API startup.
- API and SSR scaling uses request concurrency, latency, requests
  per second, and CPU as appropriate.
- Worker scaling uses outbox age, pending count, NATS consumer lag,
  or role-specific backlog.
- KEDA is acceptable for queue-driven and event-driven autoscaling.
- No service mesh is included by default.

The chart introduces `chart/tbite-platform/templates/job-db-migrate.yaml`
for database migrations, `chart/tbite-platform/templates/job-provision-streams.yaml`
for NATS stream and consumer provisioning, and
`chart/tbite-platform/templates/job-bucket-bootstrap.yaml` for object
storage bucket bootstrap. HPA resources for request-concurrency-based
roles are declared at `chart/tbite-platform/templates/hpa-*.yaml`,
and KEDA `ScaledObject` resources for queue-backed roles are declared
at `chart/tbite-platform/templates/scaledobject-*.yaml`. The
application's per-role readiness check is implemented at
`services/api/internal/httpserver/health.go` and is wired by role in
`services/api/cmd/tbite/main.go`, which also introduces the
`realtime-gateway` and `provision-streams` roles required by
[`arch-0003`](arch-0003-realtime-sse-gateway.md) and the present
specification.

## Acceptance Criteria

- Pods do not become ready when required dependencies are unavailable.
- Migration and provisioning failures appear as failed Jobs.
- Autoscaling limits account for database and broker capacity.
- Dashboards expose the same signals used for autoscaling.
- Ordinary app startup does not mutate data-plane infrastructure state.

## Compliance Evidence

| Acceptance criterion | Compliance |
| --- | --- |
| Pods do not become ready when required dependencies are unavailable. | `services/api/internal/httpserver/health.go` implements per-role readiness checks; each Deployment in `chart/tbite-platform/templates/` references the role-specific readiness endpoint. |
| Migration and provisioning failures appear as failed Jobs. | `chart/tbite-platform/templates/job-db-migrate.yaml`, `chart/tbite-platform/templates/job-provision-streams.yaml`, and `chart/tbite-platform/templates/job-bucket-bootstrap.yaml` declare Kubernetes Jobs whose status surfaces success or failure to the operator and to GitOps tooling. |
| Autoscaling limits account for database and broker capacity. | HPA and KEDA resources at `chart/tbite-platform/templates/hpa-*.yaml` and `chart/tbite-platform/templates/scaledobject-*.yaml` bound replica counts; pool size environment variables in deployment templates are derived from the database connection budget per [`adr-0007`](adr-0007-postgres-connection-and-backup.md). |
| Dashboards expose the same signals used for autoscaling. | Metrics consumed by KEDA scalers (NATS consumer lag, outbox age) and HPA scalers (request concurrency, RPS) are emitted through the OpenTelemetry Collector under [`adr-0005`](adr-0005-victoria-observability-stack.md); alerts and dashboards reference the same signals. |
| Ordinary app startup does not mutate data-plane infrastructure state. | Schema migration, stream provisioning, and bucket bootstrap are confined to Kubernetes Jobs; application Deployments do not perform these operations during startup. |

## Scope Boundary

This specification does not decide ArgoCD ownership or topology. It
does not introduce a service mesh; that requires a separate decision
with a concrete need such as mTLS, traffic policy, or multi-cluster
identity. This specification does not prescribe specific HPA targets,
specific KEDA thresholds, or specific Job retry parameters; those are
environment-dependent values guided by this decision.

## References

- Source issue: [Agentic-Build/corporate-catering-system#62](https://github.com/Agentic-Build/corporate-catering-system/issues/62)
- Parent baseline: [`00-baseline.md`](00-baseline.md)
- Related: [`adr-0005-victoria-observability-stack.md`](adr-0005-victoria-observability-stack.md), [`adr-0007-postgres-connection-and-backup.md`](adr-0007-postgres-connection-and-backup.md), [`arch-0001-worker-role-split.md`](arch-0001-worker-role-split.md), [`arch-0002-durable-event-plane-and-outbox.md`](arch-0002-durable-event-plane-and-outbox.md), [`arch-0003-realtime-sse-gateway.md`](arch-0003-realtime-sse-gateway.md), [`arch-0005-direct-object-storage-path.md`](arch-0005-direct-object-storage-path.md)
