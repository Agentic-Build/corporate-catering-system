# Architecture Specification 0003 — Realtime Gateway and Topic-Scoped SSE Fanout

- **Status**: Accepted — 2026-05-25
- **Source issue**: [Agentic-Build/corporate-catering-system#58](https://github.com/Agentic-Build/corporate-catering-system/issues/58)
- **Parent baseline**: [`00-baseline.md`](00-baseline.md)

## Context

Realtime menu and order updates materially improve the user
experience of the corporate catering platform, but broad realtime
fanout can turn a single order event into a burst of page reloads at
clients. The current employee menu path is vulnerable to this pattern
because any order event can broadcast a generic change signal that
causes clients to invalidate broad page surfaces, after which the
clients refetch a substantial fraction of the visible page. Under
lunch-peak ordering traffic, this behavior amplifies database and
SSR load at precisely the moment when the application should be
serving business requests rather than serving repeated full-page
recomputations.

A second source of pressure arises from the placement of long-lived
realtime connections on ordinary API request pods. Server-Sent Events
(SSE) connections are long-lived and consume per-connection memory and
file-descriptor budget for the duration of the client session.
Multiplexing thousands of long-lived SSE connections onto API pods
that are simultaneously serving short-lived request traffic competes
for the same resources and complicates per-pod scaling. The
architectural pressure is therefore twofold: to separate the
long-lived realtime path from the request path, and to scope realtime
fanout so that the invalidation a client receives is proportional to
the event's actual audience.

## Decision

Realtime fanout is separated from the ordinary API request path and
uses SSE as the default transport. A dedicated realtime gateway
deployment serves SSE connections and performs topic-scoped fanout
keyed by plant, date, menu item, order, or vendor as appropriate.

## Rationale

SSE fits the product's current realtime needs: server-to-client
notifications for menu availability, order board updates, and
operational state changes. SSE is unidirectional, which matches the
current workflow; it operates over plain HTTP/1.1 or HTTP/2 and is
therefore compatible with the Traefik ingress governed by
[`adr-0003`](adr-0003-traefik-gateway-api-ingress.md); and it is
straightforward to implement on the server side without introducing a
WebSocket runtime. A separate realtime gateway prevents long-lived
connections from consuming ordinary API request capacity. Topic
scoping keeps invalidation proportional to the event's actual
audience: an order placed at plant A on a particular date does not
need to invalidate menu views at plant B.

The principal alternative considered and rejected was to use
WebSocket as the realtime transport. WebSocket would have permitted
bidirectional communication and would have integrated with the same
Traefik gateway, but bidirectional communication is not currently
required by any product workflow, and adopting WebSocket would have
introduced a separate runtime concern (connection upgrade, ping/pong
handling, framing) without a corresponding benefit. WebSocket remains
available for future workflows whose semantics demand it. A second
alternative considered was to retain realtime fanout on the API pods
and to scale the API horizontally to absorb the additional load. That
alternative was rejected because it conflates two qualitatively
different workload shapes (long-lived connections and short-lived
requests) on the same scaling unit and would prevent each from being
sized correctly. A third alternative considered was to broadcast a
single global change signal to all clients on every event. That
alternative was rejected because it is the very pattern the decision
exists to eliminate.

## Design Commitments

- The realtime gateway is its own Kubernetes Deployment.
- Default transport is SSE.
- Topics are scoped by plant and date at minimum, and by menu item,
  order, or vendor where useful, per
  [`adr-0008`](adr-0008-single-enterprise-plant-aware-scaling.md).
- Clients invalidate affected data fragments, not the entire page.
- Event bursts are debounced or batched before fanout.

The chart introduces
`chart/tbite-platform/templates/deployment-realtime.yaml` to deploy
the realtime gateway. A new `realtime-gateway` role is added to the
application binary at `services/api/cmd/tbite/main.go`. Per-role
readiness checks at `services/api/internal/httpserver/health.go`
include realtime-gateway dependencies (NATS connectivity and topic
subscription health). Alerts for SSE connection count, outbound event
rate, and fanout lag are declared at
`chart/tbite-platform/templates/vmalert-rules.yaml`. The Traefik
HTTPRoute for the realtime endpoint is declared in
`chart/tbite-platform/templates/httproute-*.yaml` with timeout and
buffering settings appropriate for long-lived connections.

## Acceptance Criteria

- No global menu broadcast is used for all employee clients.
- SSE connection count, outbound event rate, and fanout lag are observable.
- Traefik route settings support long-lived SSE connections.
- Client-side invalidation is scoped to menu/home/order fragments affected by the event.
- API request pods do not carry the primary long-connection load.

## Compliance Evidence

| Acceptance criterion | Compliance |
| --- | --- |
| No global menu broadcast is used for all employee clients. | The realtime gateway publishes on topic-scoped channels keyed by plant, date, and finer-grained entities; no global broadcast topic is defined. |
| SSE connection count, outbound event rate, and fanout lag are observable. | `chart/tbite-platform/templates/vmalert-rules.yaml` declares alerts for these metrics; metrics flow through the OpenTelemetry Collector under [`adr-0005`](adr-0005-victoria-observability-stack.md). |
| Traefik route settings support long-lived SSE connections. | The HTTPRoute for the realtime endpoint in `chart/tbite-platform/templates/httproute-*.yaml` is configured with extended idle and read timeouts appropriate for SSE; per [`adr-0003`](adr-0003-traefik-gateway-api-ingress.md). |
| Client-side invalidation is scoped to menu/home/order fragments affected by the event. | Client implementation is **Follow-up** at the frontend code boundary; this specification commits the server-side fanout to topic scoping that supports fragment-level invalidation. |
| API request pods do not carry the primary long-connection load. | The realtime gateway is a separate Deployment at `chart/tbite-platform/templates/deployment-realtime.yaml`; API request pods do not handle SSE connections. |

## Scope Boundary

WebSocket is outside the baseline until a bidirectional workflow
requires it. This specification does not prescribe specific debounce
windows or batch sizes; those are implementation parameters guided by
this decision rather than prescribed by it.

## References

- Source issue: [Agentic-Build/corporate-catering-system#58](https://github.com/Agentic-Build/corporate-catering-system/issues/58)
- Parent baseline: [`00-baseline.md`](00-baseline.md)
- Related: [`adr-0003-traefik-gateway-api-ingress.md`](adr-0003-traefik-gateway-api-ingress.md), [`adr-0005-victoria-observability-stack.md`](adr-0005-victoria-observability-stack.md), [`adr-0008-single-enterprise-plant-aware-scaling.md`](adr-0008-single-enterprise-plant-aware-scaling.md), [`arch-0002-durable-event-plane-and-outbox.md`](arch-0002-durable-event-plane-and-outbox.md), [`arch-0004-read-models-and-caching.md`](arch-0004-read-models-and-caching.md), [`arch-0007-cloud-native-readiness-and-autoscaling.md`](arch-0007-cloud-native-readiness-and-autoscaling.md)
