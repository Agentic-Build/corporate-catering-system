# ADR 0005 — Victoria Observability Stack

- **Status**: Accepted — 2026-05-25
- **Source issue**: [Agentic-Build/corporate-catering-system#52](https://github.com/Agentic-Build/corporate-catering-system/issues/52)
- **Parent baseline**: [`00-baseline.md`](00-baseline.md)

## Context

The platform requires observability that is self-hosted, consistent
across environments, and sufficiently capable to support scaling
decisions. Metrics, logs, and traces must flow through a common
telemetry ingress layer so that application code does not bind to any
specific backend. Storage backends, however, must remain part of the
self-hosted product rather than be supplied by an external managed
dependency, because doing otherwise would weaken the self-hostable
claim established by [`00-baseline.md`](00-baseline.md). The
architectural pressure is to commit to a coherent backend family while
preserving the freedom to migrate or extend at the ingestion boundary.

Observability is not optional decoration on this platform. The
autoscaling signals required by [`arch-0007`](arch-0007-cloud-native-readiness-and-autoscaling.md)
(request concurrency, outbox age, consumer lag, realtime connection
count) and the operational alerts required by every data-plane and
async-plane decision in this baseline depend on a working metrics and
logs pipeline. The observability stack is therefore a load-bearing
production dependency that must be installable, queryable, and
alertable in every environment the chart targets.

## Decision

The canonical observability backend is the Victoria stack:

- **Metrics**: VictoriaMetrics.
- **Logs**: VictoriaLogs.
- **Traces**: VictoriaTraces.
- **Dashboards**: Grafana.
- **Telemetry ingress**: OpenTelemetry Collector.
- **Alerting**: VMAlert with compatible alert routing.

## Rationale

The Victoria stack keeps metrics, logs, and traces within the same
operational family and aligns with the self-hosting requirement. The
components share a common operational idiom and a common storage
philosophy that simplifies capacity planning and backup procedures.
OpenTelemetry Collector provides the stable ingestion boundary,
allowing application instrumentation to remain backend-agnostic: the
applications emit OTLP, and the collector routes signals into the
Victoria backends. Grafana remains the user-facing visualization layer
without obliging Loki or Tempo to become part of the canonical data
plane.

A principal alternative considered and rejected was a Loki and Tempo
backend pairing under Grafana. That alternative is widely deployed and
would have been functionally adequate, but it introduces a second
operational family alongside Prometheus-compatible metrics storage, it
increases the number of distinct backends to operate, and it lacks the
unified family identity that the Victoria stack provides. A second
alternative considered was a managed observability platform such as a
SaaS APM offering. That alternative was rejected because it directly
contradicts the air-gap requirement of
[`adr-0006`](adr-0006-secrets-and-air-gap.md) and would have placed
production telemetry behind an external network dependency. A third
alternative considered was to defer the choice of backend and to
publish only an OTLP ingestion contract. That alternative was rejected
because deferring the backend choice would have prevented the chart
from providing canonical dashboards and alerts, which are themselves
acceptance criteria of subordinate ADRs.

## Design Implications

Application services emit OTLP telemetry to the OpenTelemetry Collector
deployed by the chart. The collector routes metrics to VictoriaMetrics,
logs to VictoriaLogs, and traces to VictoriaTraces. Dashboards in
Grafana cover application, infrastructure, data-plane, and async-plane
health. Scaling signals come from measured request latency,
concurrency, queue lag, and resource saturation rather than CPU alone,
which is the operational expression required by
[`arch-0007`](arch-0007-cloud-native-readiness-and-autoscaling.md).

The chart introduces `chart/tbite-platform/templates/otelcollector.yaml`
to deploy the OpenTelemetry Collector and
`chart/tbite-platform/templates/vmalert-rules.yaml` to declare the
alert rules covering data-plane saturation, outbox age, consumer lag,
storage pressure, and file-transfer failures.

## Acceptance Criteria

- API, web SSR, workers, Postgres, Valkey, NATS JetStream, MinIO, Traefik, and Kubernetes health are observable.
- Alerts cover SLO violations, outbox age, consumer lag, database saturation, storage pressure, file-transfer failures, and telemetry pipeline failures.
- Trace correlation works across API, worker, database, and event publication paths.
- Local values can run a smaller observability profile while preserving OTLP contracts.

## Compliance Evidence

| Acceptance criterion | Compliance |
| --- | --- |
| API, web SSR, workers, Postgres, Valkey, NATS JetStream, MinIO, Traefik, and Kubernetes health are observable. | `chart/tbite-platform/templates/otelcollector.yaml` deploys the OpenTelemetry Collector and routes signals to the Victoria backends; service-level scrape and OTLP exporters cover each named component. |
| Alerts cover SLO violations, outbox age, consumer lag, database saturation, storage pressure, file-transfer failures, and telemetry pipeline failures. | `chart/tbite-platform/templates/vmalert-rules.yaml` declares VMAlert rules for these conditions and is referenced by [`adr-0007`](adr-0007-postgres-connection-and-backup.md), [`arch-0001`](arch-0001-worker-role-split.md), [`arch-0002`](arch-0002-durable-event-plane-and-outbox.md), and [`arch-0003`](arch-0003-realtime-sse-gateway.md). |
| Trace correlation works across API, worker, database, and event publication paths. | Application services emit OTLP traces through the OpenTelemetry Collector; trace context propagation is enabled in `services/api/cmd/tbite/main.go` for each role. |
| Local values can run a smaller observability profile while preserving OTLP contracts. | `chart/tbite-platform/values-dev.yaml` reduces observability replica counts and retention while preserving the OTLP ingestion contract from `chart/tbite-platform/templates/otelcollector.yaml`. |

## Scope Boundary

Loki and Tempo are not canonical backends for this stack. They may
appear only through a separate replacement decision. This ADR does not
prescribe specific dashboard taxonomies, specific alert thresholds, or
specific retention windows; those are environment-specific and
supplied through values.

## References

- Source issue: [Agentic-Build/corporate-catering-system#52](https://github.com/Agentic-Build/corporate-catering-system/issues/52)
- Parent baseline: [`00-baseline.md`](00-baseline.md)
- Related: [`adr-0001-kubernetes-only-runtime.md`](adr-0001-kubernetes-only-runtime.md), [`adr-0006-secrets-and-air-gap.md`](adr-0006-secrets-and-air-gap.md), [`adr-0007-postgres-connection-and-backup.md`](adr-0007-postgres-connection-and-backup.md), [`arch-0001-worker-role-split.md`](arch-0001-worker-role-split.md), [`arch-0002-durable-event-plane-and-outbox.md`](arch-0002-durable-event-plane-and-outbox.md), [`arch-0003-realtime-sse-gateway.md`](arch-0003-realtime-sse-gateway.md), [`arch-0007-cloud-native-readiness-and-autoscaling.md`](arch-0007-cloud-native-readiness-and-autoscaling.md)
