# 設計：員工回饋、商家對帳、菜單搜尋、商家合規自查

> 狀態：已驗證，待實作
> 日期：2026-05-17
> 來源：對照 `INITIAL.md` 與既有實作的功能缺口盤點

## 1. 背景與範圍

對照 `INITIAL.md`（規格 SSOT）盤點出的功能缺口，本次實作四個 feature：

| # | Feature | 模組 | 類型 |
|---|---|---|---|
| F1 | 員工回饋（評分＋客訴） | 新 `feedback` 模組 | 新功能 |
| F2 | 商家對帳檢視 | 新 `settlement` 模組 | 新功能 |
| F3 | 菜單搜尋與篩選 | 擴充 `menu` 模組 | 擴充 |
| F4 | 商家合規自查 | 擴充 `compliance` 模組 | 擴充 |

**不在範圍**：缺貨/售完通知（Tier 1-3）、商家自助上傳補件文件、開賣提醒、進階分析儀表板。

**核心動機**：
- F1 補上治理引擎兩個沒有資料來源的訊號（滿意度、客訴）。
- F2 提供商家月度對帳能力（`INITIAL.md` 商家段落明列）。
- F3 補上 `INITIAL.md` 標明「優先於推薦引擎」的搜尋/篩選能力。
- F4 讓商家能自查合規狀態與文件到期。

## 2. 共同慣例

- Go modular monolith：每個 feature = 一個 package，內含 Service + Repository + `http/` handlers（huma）+ `postgres/` repo 實作。
- SQL 一律 `pgx` `$N` 參數化綁定，禁止 `fmt.Sprintf("SELECT…")`。
- 金額一律 `money_minor`（BIGINT），禁止浮點。
- 新增/修改 huma handler 後跑 `make contract-sync` 重新產生 OpenAPI + TS client。
- 所有「寫入」操作寫一筆 `audit_event`。
- 測試：repo 測試 + service 測試，遵循 TDD（先寫測試）。
- Migration 編號：**F1 = `000009_feedback`，F2 = `000010_vendor_settlement`**（事先分配避免衝突）。

## 3. F1 — 員工回饋（`feedback` 模組）

### 3.1 資料模型（migration `000009_feedback`）

```sql
CREATE TYPE meal_complaint_category AS ENUM (
  'wrong_item', 'missing_item', 'quality', 'portion', 'hygiene', 'other'
);
CREATE TYPE meal_complaint_status AS ENUM (
  'open', 'vendor_responded', 'escalated', 'resolved'
);

CREATE TABLE meal_rating (
  id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  order_id    UUID NOT NULL UNIQUE REFERENCES "order"(id) ON DELETE RESTRICT,
  user_id     UUID NOT NULL REFERENCES "user"(id) ON DELETE RESTRICT,
  vendor_id   UUID NOT NULL REFERENCES vendor(id) ON DELETE RESTRICT,
  score       SMALLINT NOT NULL CHECK (score BETWEEN 1 AND 5),
  comment     TEXT NOT NULL DEFAULT '',
  created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX meal_rating_vendor_idx ON meal_rating(vendor_id, created_at DESC);

CREATE TABLE meal_complaint (
  id                 UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  order_id           UUID NOT NULL REFERENCES "order"(id) ON DELETE RESTRICT,
  user_id            UUID NOT NULL REFERENCES "user"(id) ON DELETE RESTRICT,
  vendor_id          UUID NOT NULL REFERENCES vendor(id) ON DELETE RESTRICT,
  category           meal_complaint_category NOT NULL,
  description        TEXT NOT NULL,
  status             meal_complaint_status NOT NULL DEFAULT 'open',
  vendor_response    TEXT NOT NULL DEFAULT '',
  vendor_responded_at TIMESTAMPTZ,
  escalated_at       TIMESTAMPTZ,
  resolution         TEXT NOT NULL DEFAULT '',
  resolved_by        UUID REFERENCES "user"(id),
  resolved_at        TIMESTAMPTZ,
  created_at         TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at         TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX meal_complaint_vendor_idx ON meal_complaint(vendor_id, created_at DESC);
CREATE INDEX meal_complaint_user_idx   ON meal_complaint(user_id, created_at DESC);
CREATE INDEX meal_complaint_status_idx ON meal_complaint(status);
-- 每訂單至多一筆未結案客訴
CREATE UNIQUE INDEX meal_complaint_one_open_idx
  ON meal_complaint(order_id) WHERE status <> 'resolved';
```

- `vendor_id` 在兩表都反正規化：scanner 要 per-vendor 聚合，避免 JOIN。
- `meal_rating`：每訂單一筆（`UNIQUE order_id`）。

### 3.2 客訴狀態機

```
open ──(商家回覆)──▶ vendor_responded
 │                        │
 ├──(員工結案,滿意)─────────┼──(員工結案,滿意)──▶ resolved
 │                        │
 └──(員工升級*)──▶ escalated ◀──(員工升級*)────────┘
                    │
                    └──(福委會結案)──▶ resolved
```

- 升級條件（`*`）：`now >= created_at + 24h`，於 escalate 呼叫時即時檢查，**不需排程**。
- 員工可在 `open` / `vendor_responded` 直接結案（滿意）。
- `escalated` 後僅福委會能結案。終態僅 `resolved`。

### 3.3 規則

- 評分/客訴：訂單必須 `status = 'picked_up'`。
- 評分非強制；分數 1–5；留言 ≤ 500 字選填。
- 客訴 `description` 必填 5–1000 字。
- 一訂單同時至多一筆未結案客訴（`open`/`vendor_responded`/`escalated`），由 partial unique index 保證。
- 商家回覆 / 福委會結案文字 ≥ 5 字。

### 3.4 API

| Method | Path | 角色 | 說明 |
|---|---|---|---|
| POST | `/api/employee/orders/{id}/rating` | 員工 | 提交評分；限 picked_up；已評→409 |
| POST | `/api/employee/orders/{id}/complaint` | 員工 | 提交客訴；限 picked_up；已有未結案→409 |
| GET  | `/api/employee/complaints` | 員工 | 我的客訴列表 |
| POST | `/api/employee/complaints/{id}/escalate` | 員工 | 升級；24h gate + 狀態檢查 |
| POST | `/api/employee/complaints/{id}/resolve` | 員工 | 滿意結案 |
| GET  | `/api/merchant/complaints` | 商家 | 收件匣（可依 status 篩選） |
| POST | `/api/merchant/complaints/{id}/respond` | 商家 | 回覆；`open→vendor_responded` |
| GET  | `/api/admin/complaints` | 福委會 | 已升級客訴列表（status=escalated） |
| POST | `/api/admin/complaints/{id}/resolve` | 福委會 | 福委會結案；限 escalated |

- 角色由現有 auth middleware 提供；vendor handler 的 vendor_id、employee handler 的 user_id 由 session 解析，不走 path param。
- 授權：員工只能操作自己的客訴；商家只能看自己 vendor 的客訴。

### 3.5 Anomaly Scanner

新排程任務 `FeedbackScanner`，置於 `feedback` package，注入 `compliance.AnomalyRepository`，掛在 `--role=scheduler`（對齊 `DocumentExpiryScanner`）。

- 滾動視窗預設 14 天（env `FEEDBACK_SCAN_WINDOW`），掃描間隔預設 1h（env `FEEDBACK_SCAN_INTERVAL`）。
- 每 vendor 一次 DB 聚合：
  - **滿意度**：`avg(score)`，樣本數 ≥ 5；avg < 3.5 → 開 `satisfaction_drop`；avg < 2.5 → severity high，否則 medium。
  - **客訴**：視窗內 `count(*)`；≥ 5 → 開 `complaint_spike`；≥ 10 → high，否則 medium。
- 直接呼叫 `compliance.AnomalyRepository.Open()`；去重靠既有 `anomaly_alert_dedup_idx`（同 kind+target 僅一筆 open）。
- `payload` JSONB 帶診斷資料（avg、sample count、window_days 等）。
- **不需改 `anomaly_alert` schema**（`kind` 是自由 TEXT）。既有 `/admin/anomalies` 頁面自動顯示。

### 3.6 前端 UI

- **員工**：
  - `apps/employee/.../orders/[id]`：picked_up 訂單顯示評分表單（星等＋留言）與「回報問題」客訴表單；若已提交則顯示既有內容。
  - 新增 `apps/employee/.../complaints`：客訴列表頁，顯示狀態、商家回覆；升級鈕（24h 後啟用）、結案鈕。
- **商家**：新增 `apps/merchant/.../complaints`：收件匣＋回覆表單。
- **福委會**：新增 `apps/admin/.../complaints`：已升級客訴處理頁＋結案表單。Anomaly 不需新 UI。

## 4. F2 — 商家對帳（`settlement` 模組）

### 4.1 資料模型（migration `000010_vendor_settlement`）

```sql
CREATE TYPE vendor_settlement_status AS ENUM ('closed', 'void');

CREATE TABLE vendor_settlement (
  id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  vendor_id     UUID NOT NULL REFERENCES vendor(id) ON DELETE RESTRICT,
  period_start  DATE NOT NULL,
  period_end    DATE NOT NULL,
  order_count   INTEGER NOT NULL CHECK (order_count >= 0),
  portion_count INTEGER NOT NULL CHECK (portion_count >= 0),
  gross_minor   BIGINT NOT NULL CHECK (gross_minor >= 0),
  order_ids     UUID[] NOT NULL,
  status        vendor_settlement_status NOT NULL DEFAULT 'closed',
  closed_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
  closed_by     UUID REFERENCES "user"(id),
  created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
  CHECK (period_start <= period_end)
);
-- 同一商家同一期間至多一筆有效結算單
CREATE UNIQUE INDEX vendor_settlement_active_idx
  ON vendor_settlement(vendor_id, period_start, period_end)
  WHERE status = 'closed';
CREATE INDEX vendor_settlement_vendor_idx ON vendor_settlement(vendor_id, period_start DESC);
```

### 4.2 計入規則（關鍵：須與 payroll 扣款一致）

- `gross_minor = Σ order.total_price_minor`，計入 `status ∈ {picked_up, no_show}` 的訂單（已備餐者）。
- `portion_count = Σ order_item.qty`（同上訂單範圍）。
- 期間以 `order.supply_date` 切分。
- **實作時先核對 `payroll.Service` 的關帳計入邏輯並對齊**——對帳的前提是商家應收總額 = 員工被扣款總額。若 payroll 計入範圍不同，以 payroll 為準並在設計文件補註。

### 4.3 關帳與檢視

- **管理員關帳**：`POST /api/admin/vendor-settlements/close {period_start, period_end}` → 逐 vendor 聚合、寫 `vendor_settlement` 列、寫 `audit_event`。重複關同期間→409（需先 void）。獨立於 payroll 關帳。
- **商家即時推導 view**：`GET /api/merchant/reconciliation?period=YYYY-MM` → 從 `order` 即時算當月（未關帳）訂單數/份數/金額＋狀態分布（picked_up/no_show/cancelled/refunded 計數）。
- **商家對帳單**：`GET /api/merchant/settlements`（清單）、`GET /api/merchant/settlements/{id}`（明細，由 `order_ids` 展開訂單級資料）。

### 4.4 API

| Method | Path | 角色 | 說明 |
|---|---|---|---|
| GET  | `/api/merchant/reconciliation?period=YYYY-MM` | 商家 | 即時推導月度摘要 |
| GET  | `/api/merchant/settlements` | 商家 | 已關帳對帳單清單 |
| GET  | `/api/merchant/settlements/{id}` | 商家 | 對帳單明細（訂單級） |
| GET  | `/api/admin/vendor-settlements?period=YYYY-MM` | 福委會 | 全商家結算總覽 |
| POST | `/api/admin/vendor-settlements/close` | 福委會 | 關帳 |
| POST | `/api/admin/vendor-settlements/{id}/void` | 福委會 | 作廢（更正用） |

- 商家 endpoint 的 vendor_id 由 session 解析；商家只能看自己的結算單（`/settlements/{id}` 須校驗歸屬）。

### 4.5 前端 UI

- **商家**：新增 `apps/merchant/.../reconciliation`：當月即時摘要卡片＋歷史對帳單表格＋點入明細下鑽。
- **福委會**：新增 `apps/admin/.../vendor-settlements`：選期間、關帳鈕、全商家結算總覽表。

## 5. F3 — 菜單搜尋與篩選（擴充 `menu`）

### 5.1 API 擴充

`GET /api/employee/menu` 加上選用 query 參數，全部下推到 repo SQL：

| 參數 | 型別 | 行為 |
|---|---|---|
| `q` | string | name / description 關鍵字（`ILIKE '%q%'`） |
| `tags` | []string | 健康標籤，符合任一即列入（OR；對 `tags` 欄位 array overlap） |
| `price_min` / `price_max` | int64 (minor) | 價格區間（含端點） |
| `in_stock` | bool | true 時排除 `sold_out` 項目 |
| `sort` | enum `name｜price_asc｜price_desc｜remain` | 排序；預設維持現有順序 |

- `menu.Service.ListForEmployee` 改吃一個 `EmployeeMenuFilter` struct（含上述欄位 + 既有 plant/day）。
- 篩選/排序一律在 repo 層 SQL 完成，不在 Go 記憶體過濾。
- 所有參數皆為選用；不帶任何參數時行為與現狀完全一致（向後相容）。

### 5.2 前端 UI

`apps/employee` 菜單頁格線上方新增篩選列：搜尋輸入框、健康標籤 chips（多選）、價格區間、「僅顯示有貨」開關、排序下拉。篩選狀態反映在 URL query，重新整理可保留。

## 6. F4 — 商家合規自查（擴充 `compliance`）

### 6.1 API

新增 `GET /api/merchant/compliance`（vendor_id 由登入 session 解析，**不**走 path param）。回傳：

- `vendor`：id、display_name、status
- `documents[]`：kind、filename、status、expires_at、reviewed_at、notes — 重用既有 `compliance.Service.ListVendorDocuments`
- `warnings[]`：後端計算的提示，每筆含 `kind`、`message`、`severity`：
  - `document_rejected`：有文件被駁回
  - `document_expired`：已過期文件
  - `document_expiring`：30 天內到期
  - `document_missing`：必繳文件未上傳

必繳文件種類定義為常數（`business_license`、`food_safety_permit`、`tax_registration`、`insurance`）。

`vendor.status` 實際 enum 為 `pending / approved / suspended / terminated`（migration `000002`）；banner 文案依此四值撰寫。

### 6.2 前端 UI

`apps/merchant` 新增 `/compliance` 頁：頂部狀態 banner（大字顯示目前 vendor status＝`pending`待審／`approved`已核准／`suspended`停權中／`terminated`已終止，各附能力說明）＋文件表格＋警示清單。

### 6.3 範圍界線

本 feature 僅做唯讀自查。商家自助上傳補件文件目前是 admin-only，屬已知缺口，留作後續。

## 7. 跨feature整合與建置順序

### 7.1 共用檔案與衝突管理

路由註冊機制：每個 handler 模組各自匯出一個 `Register(huma.API)` 函式，由 `cmd/tbite/main.go` 以 `apiBuilders ...func(huma.API)` 變參組裝。`httpserver/server.go` 為通用機制、**不需修改**。

平行實作時，下列為共用檔案，**由 orchestrator 統一處理，subagent 不得修改**：

- `services/api/cmd/tbite/main.go`（唯一整合點）— 組裝各模組的 `Register`、建構各 Service、接線 F1 scanner。
- `contract/openapi/`、`packages/api-client/`（生成物）— 全部 handler 完成後由 orchestrator 跑一次 `make contract-sync`。

Migration 檔名已事先分配（F1=000009、F2=000010），不會衝突。

各 subagent 負責的範圍彼此 disjoint：
- F1：`services/api/internal/feedback/**`、`migrations/000009_*`、`apps/{employee,merchant,admin}` 的 complaints 路由與 orders/[id] 評分區塊。
- F2：`services/api/internal/settlement/**`、`migrations/000010_*`、`apps/{merchant,admin}` 的 settlement 路由。
- F3：`services/api/internal/menu/**`（擴充既有檔案）、`apps/employee` 菜單頁。
- F4：`services/api/internal/compliance/**`（新增 merchant handler）、`apps/merchant/compliance` 路由。

> 注意：F3 改 `menu` 既有檔案、F4 改 `compliance` 既有檔案，與 F1/F2 的新模組不重疊；但 F1 scanner 依賴 `compliance.AnomalyRepository`（既有、唯讀依賴，不衝突）。

### 7.2 建置順序

1. 兩個 migration（F1、F2）。
2. 四個 feature 的後端模組（Service + Repo + handlers）+ 測試 — **可平行**。
3. Orchestrator 串接 httpserver 路由 + main.go 接線（含 F1 scanner）。
4. `make contract-sync` 重新生成 OpenAPI + TS client。
5. 四個 feature 的前端 — **可平行**（依賴步驟 4 的 TS client）。
6. Orchestrator 整合驗證：`make test-go`、`make test-web`、build、必要時 e2e。

### 7.3 測試策略

- 每個後端模組：repo 測試（對真實 Postgres，沿用 `testhelper_test.go` 模式）+ service 測試。
- `FeedbackScanner`：對齊 `ontime_rate_test.go` 寫單元測試（門檻觸發 / 樣本不足不觸發 / 去重）。
- 客訴狀態機：service 測試覆蓋每個轉換與非法轉換（含 24h gate）。
- 結算計入規則：service 測試覆蓋 picked_up/no_show 計入、cancelled/refunded 排除。
- 遵循 TDD：先寫測試。

## 8. MCP（最後階段，可裁切）

為維持 `INITIAL.md` 要求的 MCP↔HTTP 契約對等，於全部 HTTP 完成後加 3 個薄包裝 tool，共用同一 service 層：
- `feedback.rate_order`、`feedback.file_complaint`（員工）
- `settlement.close_period`（福委會）

此階段若時間不足可裁切，不影響 F1–F4 的 HTTP 功能完整性。
