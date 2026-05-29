# High Availability Research Summary

This summarizes the HA research and local experiments that shaped the
production chart changes.

## Goal

Validate behavior, not production capacity, on one workstation:

- model three availability zones with kind worker labels
- keep full observability online
- exercise HPA, KEDA, drain, quorum failover, and telemetry-path outages
- carry the proven behavior shape into `values-prod-ha.yaml`

## Local HA Harness

The local harness uses `values.yaml + values-local-ha.yaml` on a six-worker
kind cluster with three modeled zones: `local-a`, `local-b`, and `local-c`.
It keeps the production HA topology visible while shrinking storage, resource
requests, and retention so a 32 GiB OrbStack allocation can run repeated
behavior drills.

The harness includes:

- cluster bootstrap and zone labels in `ops/local-ha/kind-multiaz.yaml`
- lifecycle targets in `Makefile` under `local-ha-*`
- scenario scripts in `scripts/local-ha/`
- a Grafana dashboard in `chart/tbite-platform/dashboards/local-ha-drills.json`
- the operating playbook in `docs/deployment/local-ha.md`

## Scenarios Exercised

The local experiments covered:

- API HPA CPU scale-up and scale-down
- worker KEDA backlog scale-up, pending-pod behavior, and recovery
- app-only node and zone drains
- deliberate PDB and pinned local-path PVC drain blockers
- CNPG primary loss, RW pooler loss, and primary-on-cordoned-node switchover
- NATS pod loss and JetStream quorum signals
- Valkey Sentinel primary loss and primary-service routing
- MinIO Tenant pod loss plus S3 write/read/delete probes
- API object-storage dependency failure
- metrics-server and KEDA metrics apiserver outages
- vmagent, vmalert, kube-state-metrics, OTel Collector, VictoriaLogs, and
  VictoriaTraces outages

The final local HA run recovered to a clean baseline with `make local-ha-wait`,
zero top-level dashboard availability faults, and a full dashboard PromQL
smoke pass.

## Production Chart Application

`values-prod-ha.yaml` now represents the production HA shape:

- app roles run multi-replica with PDBs, HPA/KEDA ceilings, and role-scoped
  topology spread across zones and hostnames
- CNPG runs three instances with PgBouncer pooler replicas, backup retention,
  and PITR enabled
- Valkey uses replication plus Sentinel quorum
- NATS runs a five-node cluster while JetStream streams are provisioned with
  three replicas
- MinIO switches from standalone mode to a distributed Tenant
- Authentik, Hydra, Traefik, cert-manager, Grafana, and OTel Collector have HA
  replica counts where the chart owns those workloads
- kube-state-metrics exposes zone and component labels needed by the HA
  dashboard and drills
- OTel logs and traces export into VictoriaLogs and VictoriaTraces while
  metrics continue to VictoriaMetrics

The base `values.yaml` remains the single-enterprise production sizing. Local
HA and production HA are overlays, not a replacement for the base profile.

## Application Runtime Changes

The API and split-role binaries now expose dependency-aware readiness:

- API readiness checks Postgres RW, Valkey, and object storage
- worker readiness checks the dependencies each role actually needs
- background JetStream consumers become explicit readiness dependencies and
  retry with backoff instead of silently stopping
- realtime pods expose `/drainz`, mark readiness false during preStop, and keep
  a configurable drain delay for long-lived SSE connections
- readiness exports `tbite_dependency_ready` with dependency, pod, and role
  labels so dashboards can identify client-impact directly

Startup DB and Redis connections use bounded retry windows from chart values.
This lets local HA failover drills absorb short data-plane transitions without
adding unbounded startup fallback paths.

## Capacity Boundary

The local HA profile is valid for behavior testing on a 32 GiB workstation.
It does not prove production capacity. Production capacity validation should
use `values-prod-ha.yaml` on a real multi-AZ cluster with production-class
storage and load generation.
