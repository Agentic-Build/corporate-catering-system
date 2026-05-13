# P8 Hardening + Load Gate + Chaos Drill Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans (or superpowers:subagent-driven-development) to implement this plan task-by-task.

**Goal**：把 P0-P7 累積的所有功能升級為 production-ready — k6 hard-SLO load gate 阻擋退化 PR、OpenTelemetry instrumentation 完整覆蓋、scheduler 多副本 leader election、K8s 安全 baseline 驗證、chaos drill runbook。

**Architecture**：load gate 跑 k6 against 真實 dev stack (kind/k3d cluster) + assert percentiles；OTel collector + exporter via env；scheduler 用 `k8s.io/client-go` lease lock；chaos via `chaosmesh` (optional) or simpler `kubectl delete pod` script。

**Tech Stack**：`k6` + `github.com/open-telemetry/opentelemetry-go` + `k8s.io/client-go` Lease + `grafana/k6-action` (GitHub Action) + 沿用全部現有 stack。

**Branch**：`feat/p8-hardening`（已切）

**Scope boundary**：
- P8 **做**：k6 perf script (3 scenarios) + CI gate workflow + OTel HTTP/DB/NATS instrumentation + scheduler Lease leader + chaos drill markdown runbook + final docs
- P8 **不**做**：完整 chaos-mesh deployment manifests (out of scope local dev)、bank-level pentest、SOC2 compliance checklist (留 production phase)

---

## Task 1：k6 load script + dev stack helper

**Files**:
- Create: `ops/load/k6-lunch-peak.js`
- Create: `ops/load/run-loadtest.sh`

**k6 script** simulates lunch-time peak:
- Scenario 1: 100 employees browsing menu (`GET /api/employee/menu?plant=&day=`) — ramp 0→100 over 30s, hold 60s
- Scenario 2: 50 employees placing orders (`POST /api/employee/orders`) — staggered to avoid duplicate decrements
- Scenario 3: 200 employees fetching pickup codes (`GET /api/employee/orders/{id}/pickup-code`) — for READY orders

Thresholds (hard-SLO):
- `http_req_duration{operation:list_employee_menu} p(95) < 300ms`
- `http_req_duration{operation:place_order} p(95) < 500ms`
- `http_req_duration{operation:get_pickup_code} p(95) < 100ms`
- `http_req_failed rate < 0.01`

`ops/load/run-loadtest.sh` wraps:
1. Start docker-compose-style stack (Postgres + Redis + NATS + MinIO) — or skip if already running
2. Run go API + workers + scheduler
3. Seed N orders in different states (placed/cutoff/ready)
4. Run k6 with `--summary-export=summary.json`
5. Parse summary; fail if any threshold violated

**Commit**: `feat(load): k6 lunch-peak script + run-loadtest.sh harness`

---

## Task 2：CI hard-SLO load-gate workflow

**Files**:
- Create: `.github/workflows/ci-load-gate.yml`

Workflow steps:
1. Boot postgres + redis + nats services
2. Apply migrations
3. Build Go binary
4. Start API (background)
5. Start worker, scheduler (background)
6. Seed test data
7. Run k6 with the lunch-peak script
8. Parse `summary.json` and fail if thresholds violated

Manual trigger (`workflow_dispatch`) + nightly on `main`. Don't block every PR (too slow).

**Commit**: `ci: hard-SLO load gate (k6 lunch-peak) — nightly + manual`

---

## Task 3：OpenTelemetry instrumentation

**Files**:
- Modify: `services/api/internal/platform/observability/otel.go` (new) — initializer
- Modify: `services/api/cmd/tbite/main.go` — call observability.Init at boot
- Modify: HTTP middleware to add `otelhttp.NewMiddleware`
- Modify: pgx pool to register otel hooks via `otelpgx`
- Modify: NATS client to wrap with otel tracing (manual span emit on Publish/Consume)

`go get`:
```
go.opentelemetry.io/otel
go.opentelemetry.io/otel/sdk
go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp
go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp
github.com/exaring/otelpgx
```

`observability.Init(ctx, serviceName)` reads `OTEL_EXPORTER_OTLP_ENDPOINT` and sets up tracer provider with batched OTLP HTTP exporter. Returns a shutdown func.

Integration: every HTTP request, DB query, NATS publish/consume should produce a span. Manually instrument NATS (no off-the-shelf otelnats — use `tracer.Start` in client wrapper).

Test: spin OTel collector container (`testcontainers-go/modules/otelcol` or local mock), make a few HTTP requests, assert spans were exported.

**Commit**: `feat(observability): OpenTelemetry traces for HTTP/DB/NATS`

---

## Task 4：Scheduler Leader Election

**Files**:
- Create: `services/api/internal/platform/leader/lease.go`
- Modify: `services/api/cmd/tbite/main.go` `case config.RoleScheduler` to acquire lease before running jobs

`k8s.io/client-go/tools/leaderelection` standard pattern:

```go
package leader

import (
    "context"

    "k8s.io/client-go/kubernetes"
    "k8s.io/client-go/rest"
    "k8s.io/client-go/tools/leaderelection"
    "k8s.io/client-go/tools/leaderelection/resourcelock"
)

type Config struct {
    Namespace, LeaseName, Identity string
}

func RunWithLease(ctx context.Context, cfg Config, onLeading func(ctx context.Context)) error {
    k8sCfg, err := rest.InClusterConfig()
    if err != nil { return err }
    client := kubernetes.NewForConfigOrDie(k8sCfg)
    lock := &resourcelock.LeaseLock{
        LeaseMeta: metav1.ObjectMeta{ Name: cfg.LeaseName, Namespace: cfg.Namespace },
        Client: client.CoordinationV1(),
        LockConfig: resourcelock.ResourceLockConfig{ Identity: cfg.Identity },
    }
    leaderelection.RunOrDie(ctx, leaderelection.LeaderElectionConfig{
        Lock: lock,
        ReleaseOnCancel: true,
        LeaseDuration: 15 * time.Second,
        RenewDeadline: 10 * time.Second,
        RetryPeriod:   2 * time.Second,
        Callbacks: leaderelection.LeaderCallbacks{
            OnStartedLeading: onLeading,
            OnStoppedLeading: func() { /* log */ },
        },
    })
    return nil
}
```

Local dev fallback: if not running in K8s (`rest.InClusterConfig` fails), skip lease and just run directly. Same code path; no behavior change for local.

**Commit**: `feat(scheduler): K8s Lease leader election for multi-replica scheduler`

---

## Task 5：Security baseline verification + chaos drill runbook

**Files**:
- Create: `ops/security/checklist.md` — verify NetworkPolicy default-deny, securityContext readOnlyRootFilesystem/runAsNonRoot/drop ALL, no secret in image
- Create: `ops/chaos/drill-runbook.md` — kill API pod during lunch peak, verify order placements still succeed (within HA replicas)
- Create: `scripts/security-scan.sh` — runs trivy on images + kubesec on manifests

Run security-scan as part of `ci-build-images.yml` workflow.

**Commit**: `docs(security): security checklist + chaos drill runbook + trivy scan in CI`

---

## Task 6：Final docs + PR (project complete)

**Files**:
- Update: `README.md` — Phases section all ✅; add Architecture / Quick Start / Deployment / Operations sections
- Update: `docs/plans/2026-05-13-tbite-refactor-design.md` — §15 P8 ✅ Done; add "Implementation complete" note
- Create: `CHANGELOG.md` with P0-P8 phase summary
- Run final exit-criteria sweep
- Push + PR with title "P8: hardening + load gate + chaos drill (refactor complete)"

**Commit**: `docs: mark P8 done; refactor complete (P0-P8 all ✅)`

---

## Exit Criteria

- [ ] k6 load test runs to completion against local stack
- [ ] CI load-gate workflow registered (nightly + manual)
- [ ] OTel traces visible in OTel collector logs
- [ ] Scheduler Lease lock acquired + released cleanly (local fallback works)
- [ ] Security checklist + chaos runbook reviewable
- [ ] README + design doc all marked ✅
- [ ] PR opened with complete summary

---

## After P8

This phase completes the refactor. All 9 PRs (#1-#9) plus this PR (#10 likely) form the full migration from Rust monolith + single SPA → Go modular monolith + 3 SvelteKit apps + dual K8s overlay + MCP server.

The system now supports:
- 3 SvelteKit frontends (employee/merchant/admin)
- Go API + worker + scheduler binaries
- OIDC login (Google + GitHub)
- Vendor onboarding + menu + supply + orders + TOTP pickup + payroll + governance
- MCP for AI agents
- single-node K8s + GCP managed-services deployments
