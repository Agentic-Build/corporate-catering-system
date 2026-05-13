# P6 Vendor Governance + Anomaly Alerts + DLQ Admin Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans (or superpowers:subagent-driven-development) to implement this plan task-by-task.

**Goal**：交付福委會治理功能 — 商家文件生命週期（上傳/到期/停權）、anomaly_alert 自動產生（準時率下降）、DLQ admin（NATS 死信查詢 + 重送）、稽核查詢 API + UI、員工申訴提交（從 P5 遞延）。

**Architecture**：`vendor_document` 表 + 文件到期 scheduler；`anomaly_alert` 由 worker 訂閱 order events 計算 rolling 7d 準時率，超過閾值寫入。DLQ 用 NATS JetStream 自帶 max_deliver 失敗的訊息 → 死信流 `*.dlq` → admin API 透過 NATS Stream Info 列出 + manual replay。

**Tech Stack**：沿用 — 不新增依賴。

**Branch**：`feat/p6-governance`（已切）

**Scope boundary**：
- P6 **做**：vendor_document CRUD + 文件到期掃描 + 自動停權通知（不自動停權，由 admin 確認）、anomaly_alert 自動產生（準時率）、anomaly admin UI、DLQ list + replay endpoint、audit 查詢 endpoint + admin UI timeline、員工 dispute 提交 UI
- P6 **不**做**：客訴升高/滿意度 anomaly（待 P7+ 加滿意度 schema）、自動扣款失敗復原、押金管理

---

## Task 1：Migration — vendor_document + anomaly_alert

`migrations/000006_governance.up.sql`:

```sql
CREATE TYPE vendor_document_kind AS ENUM (
    'business_license', 'food_safety_permit', 'tax_registration', 'insurance', 'other'
);
CREATE TYPE vendor_document_status AS ENUM ('pending', 'approved', 'rejected', 'expired');
CREATE TYPE anomaly_severity AS ENUM ('low', 'medium', 'high', 'critical');
CREATE TYPE anomaly_status AS ENUM ('open', 'triaged', 'closed');

CREATE TABLE vendor_document (
  id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  vendor_id    UUID NOT NULL REFERENCES vendor(id) ON DELETE CASCADE,
  kind         vendor_document_kind NOT NULL,
  blob_uri     TEXT NOT NULL,
  filename     TEXT NOT NULL,
  uploaded_by  UUID REFERENCES "user"(id),
  expires_at   DATE,
  status       vendor_document_status NOT NULL DEFAULT 'pending',
  reviewed_by  UUID REFERENCES "user"(id),
  reviewed_at  TIMESTAMPTZ,
  notes        TEXT NOT NULL DEFAULT '',
  created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX vendor_document_vendor_idx ON vendor_document(vendor_id);
CREATE INDEX vendor_document_status_idx ON vendor_document(status);
CREATE INDEX vendor_document_expiring_idx ON vendor_document(expires_at) WHERE status = 'approved' AND expires_at IS NOT NULL;

CREATE TABLE anomaly_alert (
  id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  kind         TEXT NOT NULL,           -- 'on_time_rate_drop', 'document_expiring', etc.
  target_kind  TEXT NOT NULL,           -- 'vendor', 'order', etc.
  target_id    TEXT NOT NULL,
  severity     anomaly_severity NOT NULL DEFAULT 'medium',
  status       anomaly_status NOT NULL DEFAULT 'open',
  payload      JSONB NOT NULL DEFAULT '{}'::jsonb,
  evidence_uri TEXT[] NOT NULL DEFAULT '{}',
  triaged_at   TIMESTAMPTZ,
  triaged_by   UUID REFERENCES "user"(id),
  closed_at    TIMESTAMPTZ,
  closed_by    UUID REFERENCES "user"(id),
  notes        TEXT NOT NULL DEFAULT '',
  created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX anomaly_alert_target_idx ON anomaly_alert(target_kind, target_id);
CREATE INDEX anomaly_alert_status_idx ON anomaly_alert(status);
CREATE INDEX anomaly_alert_open_severity_idx ON anomaly_alert(severity, created_at DESC) WHERE status = 'open';

-- Dedup constraint: at most ONE open anomaly per (kind, target_kind, target_id) at any time
CREATE UNIQUE INDEX anomaly_alert_dedup_idx
  ON anomaly_alert(kind, target_kind, target_id) WHERE status = 'open';
```

Down: reverse drops + DROP TYPEs.

**Commit**: `feat(db): governance schema (vendor_document, anomaly_alert) + dedup constraint`

---

## Task 2：Compliance domain + repos

`services/api/internal/compliance/{types,errors,repository}.go` + `postgres/{document_repo,anomaly_repo}.go` + tests.

Domain:
- `Document{ID, VendorID, Kind (5 enum), BlobURI, Filename, UploadedBy, ExpiresAt *time.Time, Status (pending/approved/rejected/expired), ReviewedBy *string, ReviewedAt *time.Time, Notes}`
- `Anomaly{ID, Kind, TargetKind, TargetID, Severity, Status, Payload map[string]any, EvidenceURI []string, TriagedAt/By *, ClosedAt/By *, Notes}`

Repos:
- `DocumentRepo`: Create / GetByID / ListByVendor / UpdateStatus / ListExpiringBefore(date)
- `AnomalyRepo`: Open(kind, target_kind, target_id, severity, payload, evidenceURIs []string) — uses INSERT ON CONFLICT DO UPDATE to handle dedup; List(status filter); Triage(id, by, notes); Close(id, by, notes)

Errors: `ErrDocumentNotFound`, `ErrAnomalyNotFound`, `ErrInvalidStatus`

TDD: ~10 tests across both repos. Test the dedup constraint by Open()ing twice for same key.

**Commit**: `feat(compliance): domain + postgres repos with anomaly dedup`

---

## Task 3：Compliance service + huma handlers

`services/api/internal/compliance/service.go` + handlers:

- `UploadDocument(ctx, vendorID, kind, fileBytes, filename, expiresAt, uploadedBy)` — uploads to S3 (`vendor-docs/{vendor}/{uuid}-{filename}`); creates DB row in `pending`; writes audit
- `ReviewDocument(ctx, docID, reviewerID, status, notes)` — pending → approved | rejected; writes audit + outbox `vendor.document_reviewed.v1`
- `ListVendorDocuments(ctx, vendorID, includeAll bool)`
- `OpenAnomaly(ctx, anomaly Anomaly) error` — primary by workers; idempotent via dedup index
- `TriageAnomaly(ctx, id, by, notes)` / `CloseAnomaly(ctx, id, by, notes)` — admin actions

8 endpoints:

| Method | Path | Op | Role |
|---|---|---|---|
| POST | `/api/admin/vendors/{vendor_id}/documents` | uploadVendorDocument | admin (multipart) |
| GET  | `/api/admin/vendors/{vendor_id}/documents` | listVendorDocuments | admin |
| POST | `/api/admin/documents/{id}/review` | reviewDocument | admin |
| GET  | `/api/admin/anomalies` | listAnomalies (status filter) | admin |
| POST | `/api/admin/anomalies/{id}/triage` | triageAnomaly | admin |
| POST | `/api/admin/anomalies/{id}/close` | closeAnomaly | admin |
| GET  | `/api/admin/audit` | listAuditEvents (filter target_kind/target_id/since) | admin |
| POST | `/api/admin/dlq/replay` | replayDLQ (subject filter, batch) | admin |

For multipart upload: huma supports multipart via `huma.Multipart` — use it. If complex, fall back to plain chi handler for upload endpoint.

Wire into main.go + contract-export.

**Commit**: `feat(compliance): service + handlers (upload/review/anomaly triage/audit query/dlq)`

---

## Task 4：Document expiry scanner (scheduler addition)

Add a 3rd job to scheduler: `DocumentExpiryScanner`:

```go
type DocumentExpiryScanner struct {
    Pool     *pgxpool.Pool
    Docs     compliance.DocumentRepository
    Anomaly  compliance.AnomalyRepository
    Interval time.Duration  // default 1h
    DaysBefore int          // 14 days before expiry → low severity, 7 → medium, 1 → critical
    Logger   *slog.Logger
}
```

`RunOnce`: SELECT approved documents with `expires_at BETWEEN now() AND now() + interval '14 days'`. For each: compute severity by days_until_expiry; call `anomaly.Open(kind="document_expiring", target_kind="vendor_document", target_id=docID, severity, payload={vendor_id, kind, expires_at, days_until}, evidence_uri=[doc.BlobURI])`.

Also: documents past expiry get marked `expired` + open `critical` anomaly.

Scheduler entrypoint: add this job to errgroup alongside Cutoff + NoShowSweep.

Integration test: seed 3 docs (one due in 12 days, one in 5 days, one past expiry); run scanner; assert 3 anomalies created with correct severities.

**Commit**: `feat(scheduler): document expiry scanner generates anomalies + marks expired`

---

## Task 5：On-time-rate anomaly evaluator worker

New worker subscribed to `order.picked_up.v1` + `order.no_show.v1`. Maintains in-memory rolling window per vendor (last 7 days). When `picked_up rate / total < threshold` (e.g. < 95%), opens `anomaly.Open(kind="on_time_rate_drop", target_kind="vendor", target_id=vendorID, severity=high if <90% else medium, payload={total, picked_up, rate})`.

Implementation: maintain `map[vendorID][]event` where each event is `{timestamp, status}`. Trim entries older than 7d. Trigger evaluation per event (debounced if needed — for P6 simplicity, evaluate every event).

`services/api/internal/compliance/evaluator/onstime_rate.go` + integration test (use ephemeral testcontainers NATS, publish events, assert anomaly written).

Worker entrypoint: add to errgroup in --role=worker alongside outbox-relay + payroll-settler.

**Commit**: `feat(compliance): rolling-window on-time-rate anomaly evaluator worker`

---

## Task 6：DLQ list + replay API

DLQ in NATS JetStream: when a consumer fails after MaxDeliver, the original message goes to a `<stream>.dlq` subject (managed manually — JetStream doesn't natively route to DLQ; we need a `RepublishConfig` or `DiscardDLQ` setup).

Simpler P6 approach: use NATS's per-consumer "delivery exceeded" event subject + a dedicated `dlq.<stream>` stream that mirrors them. Actually for P6 simplicity, **build a synthetic DLQ tracking table** in Postgres: `dlq_message(id, original_subject, payload, original_event_id, attempts, last_error, first_seen_at, replayed_at)`. Workers that exceed their retry budget write the message to this table.

Add migration `000007_dlq.up.sql`:

```sql
CREATE TABLE dlq_message (
  id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  source_stream   TEXT NOT NULL,
  source_subject  TEXT NOT NULL,
  source_consumer TEXT NOT NULL,
  payload         JSONB NOT NULL,
  headers         JSONB NOT NULL DEFAULT '{}'::jsonb,
  last_error      TEXT NOT NULL DEFAULT '',
  first_seen_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
  replayed_at     TIMESTAMPTZ,
  resolved_at     TIMESTAMPTZ
);
CREATE INDEX dlq_message_pending_idx ON dlq_message(source_stream, first_seen_at DESC) WHERE replayed_at IS NULL AND resolved_at IS NULL;
```

Worker side: existing outbox-relay + settler + evaluator all catch failures internally. Add a helper `services/api/internal/platform/messaging/dlq.go` with `WriteDLQ(ctx, pool, stream, subject, consumer, payload, headers, lastError)` that's called by workers on irrecoverable failure.

API:
- `GET /api/admin/dlq` — list pending DLQ messages
- `POST /api/admin/dlq/{id}/replay` — re-publish to original subject + mark replayed_at
- `POST /api/admin/dlq/{id}/resolve` — mark resolved without replay

Note: actually wiring workers to write to DLQ on irrecoverable failure is complex and out of scope for P6. For P6 just ship the schema + API + (lightweight) test that confirms a manual DLQ row can be replayed.

**Commit**: `feat(dlq): synthetic dlq table + list/replay/resolve admin endpoints`

---

## Task 7：Admin governance UI

Routes in `apps/admin/src/routes/`:
- `/vendors/[id]/documents/+page.{svelte,server.ts}` — list + upload form + review actions
- `/anomalies/+page.{svelte,server.ts}` — list with severity/status filters + triage/close actions
- `/dlq/+page.{svelte,server.ts}` — list pending DLQ messages + replay/resolve actions  
- `/audit/+page.{svelte,server.ts}` — audit log with target_kind/target_id filters

Layout nav: add 「告警」 + 「死信」 + 「稽核」 links.

**Commit**: `feat(admin): governance UI (vendor documents + anomalies + DLQ + audit timeline)`

---

## Task 8：Employee dispute submission + OpenAPI + e2e + PR

Add `POST /api/employee/disputes` API改良 — instead of requiring `entry_id`, accept just `order_id` + backend look up the entry. Refactor `payroll.Service.OpenDispute` to lookup entry by `order_id` (search across all locked batches; for performance add an `order_id ANY(order_ids)` query).

Add employee UI route `/orders/[id]/dispute/+page.{svelte,server.ts}` — form with reason text + submit.

OpenAPI regen.

e2e spec.

Update README + design doc §15.

Push + PR.

**Commit**: `feat(employee): dispute submission UI + payroll API order_id-only signature`
**Commit**: `docs: mark P6 done; e2e governance spec`

---

## Exit Criteria

- [ ] Migrations up/down/up clean (2 new migrations: 000006 + 000007)
- [ ] Compliance repos + service tests pass
- [ ] Document expiry scanner integration test passes
- [ ] On-time-rate evaluator integration test passes
- [ ] DLQ replay endpoint smoke test passes
- [ ] Admin UI loads for documents / anomalies / dlq / audit
- [ ] Employee dispute submission UI submits successfully
- [ ] OpenAPI drift gate green
- [ ] PR opened
