# Changelog

All notable changes to T-Bite, by phase. From 2026-05-19 the project moved to
closed-loop GitOps CD (push → CI sha write-back → ArgoCD), so this file is
maintained by theme rather than per-PR; the authoritative fine-grained history
is the git log / GitHub PRs.

## Platform, deploy & architecture baseline (2026-05-19 → 2026-05-27)

Architecture & delivery (Issue #47)
- Locked a self-hostable cloud-native scaling baseline (ADRs + arch specs) (#64, #67)
- Closed-loop GitOps CD: immutable `sha-<commit>` image tags + CI git write-back, ArgoCD auto-deploy (#28, #43, #44); GHCR image publish
- Read/write split wired through (RO pool), MCP auth-failure metric (#95)
- JetStream stream replicas configurable via `NATS_STREAM_REPLICAS` (#92)
- SSE events fragment-scoped invalidation, replacing `invalidateAll` (#93)
- Popularity / recommendation aggregation moved onto a read model (Issue #59) (#94)

Product
- Merchant revamp: card-based menu, board/sticker integration, two-state menu, badges removed (+ DB columns), service-area management (#85, #74, #91)
- Plant registry (addresses + merchant self-select) and image object storage on S3/MinIO (#72, #73)
- Pickup redesign: employees scan a meal-sticker QR for self-service verification (#26)
- Three frontends re-aligned to the mobile design (RWD); native/Tauri employee app and the docker-compose runtime removed (#24, #29, #88)

Quality, observability & hardening
- Internal backend test coverage raised from ~46% to 79.6% (#87)
- Backfilled missing Grafana metrics, fixed silent alerts and dashboard metric sources (#75, #77, #78, #79, #45, #46)
- Deployment convergence + NYCU in-cluster hardening: drift-proof OIDC client_id/secret, cloudflared tunnel in GitOps, automatic DB migration, service rename to `tbite-<role>` (#66, #68, #69, #70, #76, #31–#41)
- Bug-fix waves: 12-issue functional audit (#80), demo-playbook fixes (#81), order-cutoff timezone + unified tag filters (#83), team-reported bugs (#84), HackMD checklist follow-ups (#89, #90, #91)

## Remaining Audit Gaps — Menu / Settings / Prep / Governance (2026-05-18)
- G 菜單複製: `POST /api/merchant/menu-items/{id}/copy` clones a menu item into a fresh draft
- H 臨時缺貨: `meal_supply.sold_out` flag + `POST /api/merchant/supply/{itemID}/{date}/sold-out`; employee menu and `in_stock` filter honour it (migration 000014)
- I 商家級截單設定: `vendor.cutoff_hour` + `preorder_window_days`; order cutoff is now per-vendor local time (fixes the hardcoded 17:00 UTC); `GET/PUT /api/merchant/settings` (migration 000015)
- J 備餐與配送輸出: `GET /api/merchant/prep-sheet` — per-plant breakdown, meal labels, basket lists; print-friendly page + CSV export
- K 員工扣款明細: `GET /api/employee/payroll` — the employee's salary-deduction history across batches
- L 員工週檢視: a 7-day week-calendar date picker leads the employee home page
- M 售完即時反應: `order.MenuHub` + `GET /api/employee/menu/events` SSE — the menu refetches live when stock moves
- N 廠區時段: `vendor_plant_mapping.service_window` + `PUT /api/admin/vendors/{id}/plants/{plant}/window` (migration 000016)
- O 異常治理動作: anomaly triage can carry a `warn` / `suspend` governance action against the target vendor
- P API 文件: documented the built-in `/docs` (Stoplight Elements) + `/openapi.yaml` entry points
- Q MCP 治理 tools: 5 new admin tools — `document.list/review`, `anomaly.list/triage/close` (21 tools total)
- 9 new HTTP endpoints + 1 SSE endpoint + 5 MCP tools; migrations 000014–000016
- design: `docs/plans/2026-05-18-remaining-audit-gaps-design.md`

## Order Modify / Live Board / Resupply / Settlement Exceptions (2026-05-18)
- A 員工修改訂單: `PUT /api/employee/orders/{id}` — add/remove items + change quantity before cutoff; quota adjusted by per-item delta in one transaction; `order.modify` MCP tool (16 tools total)
- B 訂單備註: per-order free-text `notes` carried from cart/edit to the merchant prep board (migration 000011)
- C 文件補件: merchant self-service `POST /api/merchant/documents`; `vendor_document.supersedes` links a resupply to the document it replaces (migration 000012)
- D 即時看板: `GET /api/merchant/orders/events` Server-Sent Events backed by an ephemeral NATS `ORDERS_V1` tap (`order.BoardHub`); the board now pushes instead of polling every 15s
- E 月結例外: `payroll_exception` — departed-employee auto-detection + manual deduction-failed flagging + resolve/exclude workflow; the settler CSV gains an `exception` column and drops excluded entries (migration 000013)
- 6 new HTTP endpoints + 1 SSE endpoint + 1 MCP tool; migrations 000011–000013
- design: `docs/plans/2026-05-18-order-modify-board-compliance-settlement-design.md`

## Feedback / Settlement / Menu Search / Compliance (2026-05-17)
- F1 員工回饋: `meal_rating` + `meal_complaint` workflow (open → vendor_responded → escalated → resolved, 24h escalation gate)
- FeedbackScanner opens `satisfaction_drop` / `complaint_spike` anomalies — the governance engine's two previously unbacked signals
- F2 商家對帳: admin monthly close (`vendor_settlement`), merchant live reconciliation view + closed statement history
- F3 菜單搜尋: optional keyword / health-tag / price-range / in-stock / sort filtering on the employee menu endpoint
- F4 商家合規自查: `GET /api/merchant/compliance` — vendor status, documents, computed warnings
- 15 new HTTP endpoints + 3 MCP tools (`feedback.rate_order`, `feedback.file_complaint`, `settlement.close_period`)
- migrations 000009 (feedback) + 000010 (vendor_settlement)

## P8 — Hardening (2026-05-13)
- k6 lunch-peak load test (3 scenarios) + run-loadtest.sh harness
- Hard-SLO CI load gate workflow (nightly + manual_dispatch)
- OpenTelemetry instrumentation (HTTP, DB via otelpgx, NATS via manual spans)
- Scheduler K8s Lease leader election (multi-replica HA, with local fallback)
- Security baseline checklist + chaos drill runbook
- Trivy scan added to ci-build-images workflow
- Final README rewrite + CHANGELOG covering P0-P8

## P7 — MCP Server (2026-05-13)
- MCP server mounted at `/mcp` with auth middleware
- 12 MCP tools (8 employee + 4 admin write): order / vendor / payroll / audit
- HTTP /mcp/sse + `--role=mcp-stdio` entry points
- `request_id="mcp:<tool>"` audit trail for every MCP call
- `docs/mcp.md` reference doc

## P6 — Vendor Governance (2026-05-13)
- `vendor_document` lifecycle + expiry scanner generating `anomaly_alert`
- Rolling-window on-time-rate evaluator (anomaly dedup constraint)
- `dlq_event` table + admin replay / resolve endpoints
- Admin compliance UIs (documents, anomalies, audit, DLQ)
- Employee dispute submission flow

## P5 — Payroll (2026-05-13)
- `payroll_batch` / `payroll_entry` / `payroll_dispute` schema (append-only entries)
- Build / Lock / OpenDispute / ResolveRefund service
- payroll-settler worker exporting HR CSV to S3
- S3 storage abstraction (MinIO single-node + GCS production)
- Admin payroll UI + employee dispute list
- payroll_entry no-delete trigger

## P4 — Pickup TOTP (2026-05-13)
- `order.totp_secret` column + ready / picked_up / no_show transitions
- Per-order TOTP (HMAC-SHA256, 30s window)
- Merchant 備餐看板 (mark-ready + verify pickup)
- Employee pickup QR page
- NoShowSweep scheduler job
- 1000-concurrent VerifyPickup atomicity proof

## P3 — Order Lifecycle (2026-05-13)
- `order` / `order_item` / `order_state_event` / `audit_event` / `outbox_event` tables
- State machine + conditional UPDATE for atomic transitions
- NATS JetStream `ORDERS_V1` stream + outbox-relay worker
- Cutoff scheduler job
- Employee cart submit + orders list + cancel
- audit_event append-only triggers

## P2 — Menu / Vendor / Quota (2026-05-13)
- `vendor` / `vendor_plant_mapping` / `menu_item` / `meal_supply` schema
- Postgres-anchored conditional decrement (500-concurrent atomicity proof)
- Employee menu aggregation + merchant CRUD + admin vendor approval
- 4 UI ports (MealCard, StateTag, StatCard, LocationBar)

## P1 — Identity + OIDC (2026-05-13)
- `user` / `user_identity` schema with configurable OIDC provider slugs
- Authentik-only OIDC + role-aware claim bootstrap
- Redis session store + OIDC state store
- Three SvelteKit apps share `@tbite/web-auth` hooks
- OpenAPI + TS client auto-generation (`make contract-sync`)

## P0 — Skeleton (2026-05-13)
- pnpm workspace with 3 SvelteKit apps + shared packages (`ui`, `tokens`, `api-client`, `web-auth`)
- Go module + multi-role binary (`api` / `worker` / `scheduler`)
- K8s base + `overlays/single-node` + `overlays/gcp`
- Makefile + dev scripts (`dev-up` / `dev-app` / `dev-down` / `dev-reset`)
- 3 CI workflows (lint-test / render-overlay / build-images)
