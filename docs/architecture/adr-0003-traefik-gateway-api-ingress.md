# ADR 0003 — Traefik Gateway API Ingress Baseline

- **Status**: Accepted — 2026-05-25
- **Source issue**: [Agentic-Build/corporate-catering-system#50](https://github.com/Agentic-Build/corporate-catering-system/issues/50)
- **Parent baseline**: [`00-baseline.md`](00-baseline.md)

## Context

Ingress is part of the product surface for a self-hosted enterprise
deployment of the corporate catering system. The gateway is responsible
for routing the application API, the SvelteKit frontends, the Authentik
identity provider, the Hydra OAuth2 server, the MinIO object storage
console and S3 endpoints, the Grafana dashboards, and the Victoria
observability endpoints. It is also responsible for terminating TLS,
issuing and renewing certificates, and sustaining long-lived
Server-Sent Events (SSE) connections without imposing buffering or
timeout behavior that would compromise realtime semantics. A
single-enterprise self-hosted product cannot delegate ingress to a
managed cloud load balancer; the gateway must be part of the chart and
must operate consistently in development clusters, staging clusters,
and production clusters.

The Kubernetes ecosystem has been transitioning from the legacy
Ingress resource toward the Gateway API as the preferred declarative
route model. Adopting Gateway API as the primary expression of routes
aligns the platform with the longer-term direction of the ecosystem and
keeps the route model closer to a portable Kubernetes standard. Some
controller-specific behavior, however, cannot be expressed cleanly in
Gateway API today and is more idiomatically expressed in
controller-native resources such as Traefik Middleware. The
architecture should embrace Gateway API as the default while
acknowledging that controller-specific extensions may be used where the
standard does not yet suffice.

## Decision

Traefik is the canonical ingress and gateway controller for the
platform. The primary routing model is the Kubernetes Gateway API where
practical. Traefik custom resources and Traefik Middleware may be used
for controller-specific behavior that the Gateway API cannot express
cleanly. cert-manager owns certificate issuance and renewal. The
baseline does not depend on the community ingress-nginx controller.

## Rationale

Traefik fits the self-hosted and local-development profile. It is
straightforward to operate on lightweight Kubernetes distributions
including kind, k3d, and OrbStack; it supports Kubernetes-native
routing models including Gateway API; and it handles common enterprise
routing needs (TLS termination, HTTP redirection, header manipulation,
forward authentication, rate limiting) without requiring a service mesh
to be present. Using Gateway API as the default expression of routes
keeps the platform aligned with the trajectory of the upstream
ecosystem and reduces controller lock-in: a future replacement of
Traefik with a different Gateway API implementation would not require
the route model to be rewritten. Where Gateway API does not yet express
a controller-specific behavior cleanly, Traefik's first-class CRDs
provide a well-supported extension surface without resorting to
annotations of dubious provenance.

The principal alternative considered and rejected was to adopt the
community ingress-nginx controller. That controller has historically
served the role admirably, but its Ingress resource model is in slow
retreat relative to Gateway API, and it does not provide a
controller-native middleware mechanism comparable to Traefik's. A
second alternative considered was to require a service mesh such as
Istio or Linkerd to serve as both the ingress and the east-west
identity plane. That alternative was rejected as disproportionate to
the present requirements: the system does not yet require mTLS between
internal services, the operational cost of a service mesh is
significant on lightweight clusters, and the additional complexity
would compromise the local development profile required by
[`adr-0001`](adr-0001-kubernetes-only-runtime.md). A service mesh
remains available as a future, separately decided concern with a
concrete need.

## Design Implications

Public routes should be modeled as `Gateway` and `HTTPRoute` resources
where practical. Traefik Middleware should be used deliberately for
redirects, header manipulation, forward authentication, or rate
limiting when those behaviors require controller support. SSE routes,
which carry long-lived connections governed by
[`arch-0003`](arch-0003-realtime-sse-gateway.md), must be tested
through Traefik with production-like timeout and buffering settings to
prevent premature disconnects or buffering-induced latency.
cert-manager `ClusterIssuer` and `Certificate` resources are part of
the baseline deployment contract and are rendered by the chart.

The chart introduces the following ingress artifacts:
`chart/tbite-platform/templates/gateway.yaml` for the `Gateway`
resource, `chart/tbite-platform/templates/httproute-*.yaml` for the
HTTPRoutes that expose each service surface,
`chart/tbite-platform/templates/middleware-redirect-https.yaml` for
HTTP-to-HTTPS redirection,
`chart/tbite-platform/templates/certificate-*.yaml` for per-service
certificate requests, and
`chart/tbite-platform/templates/clusterissuer.yaml` for the
cert-manager issuer.

## Acceptance Criteria

- API, web, Authentik, Hydra, MinIO, Grafana, and Victoria endpoints can be exposed through Traefik.
- SSE endpoints work through the gateway without premature disconnects or buffering-induced latency.
- TLS issuance and renewal are managed by cert-manager.
- Route resources minimize controller lock-in while allowing necessary Traefik behavior.
- No production manifest depends on community ingress-nginx.

## Compliance Evidence

| Acceptance criterion | Compliance |
| --- | --- |
| API, web, Authentik, Hydra, MinIO, Grafana, and Victoria endpoints can be exposed through Traefik. | `chart/tbite-platform/templates/gateway.yaml` and `chart/tbite-platform/templates/httproute-*.yaml` declare HTTPRoutes for each public surface. |
| SSE endpoints work through the gateway without premature disconnects or buffering-induced latency. | Realtime SSE routing is declared as a dedicated HTTPRoute in `chart/tbite-platform/templates/httproute-*.yaml` targeting the realtime-gateway deployment governed by [`arch-0003`](arch-0003-realtime-sse-gateway.md); Traefik options for long-lived connections are configured at the Gateway level. |
| TLS issuance and renewal are managed by cert-manager. | `chart/tbite-platform/templates/clusterissuer.yaml` and `chart/tbite-platform/templates/certificate-*.yaml` install the cert-manager ClusterIssuer and per-host Certificate resources. |
| Route resources minimize controller lock-in while allowing necessary Traefik behavior. | Routes are expressed as `HTTPRoute` resources; Traefik Middleware is limited to `chart/tbite-platform/templates/middleware-redirect-https.yaml` and is invoked from HTTPRoutes through Gateway API extension references. |
| No production manifest depends on community ingress-nginx. | The chart declares Traefik as the sole ingress controller dependency; no `ingressClassName: nginx` references appear in production templates. |

## Scope Boundary

This ADR does not introduce a service mesh, east-west mutual TLS, or an
API-management product. Those concerns require separate decisions
articulating a concrete need. This ADR also does not select a public
DNS or external load-balancer integration; both are values-supplied and
environment-specific.

## References

- Source issue: [Agentic-Build/corporate-catering-system#50](https://github.com/Agentic-Build/corporate-catering-system/issues/50)
- Parent baseline: [`00-baseline.md`](00-baseline.md)
- Related: [`adr-0001-kubernetes-only-runtime.md`](adr-0001-kubernetes-only-runtime.md), [`adr-0002-helm-umbrella-chart.md`](adr-0002-helm-umbrella-chart.md), [`arch-0003-realtime-sse-gateway.md`](arch-0003-realtime-sse-gateway.md), [`arch-0006-authentik-hydra-identity-boundary.md`](arch-0006-authentik-hydra-identity-boundary.md)
