# 員工回饋／商家對帳／菜單搜尋／商家合規自查 — 實作計畫

> **For Claude:** 本計畫由 orchestrator 以平行 subagent 執行。設計細節見 `docs/plans/2026-05-17-feedback-settlement-discovery-design.md`（以下稱「設計文件」）。

**Goal:** 實作四個功能缺口 — 員工回饋（F1）、商家對帳（F2）、菜單搜尋篩選（F3）、商家合規自查（F4）。

**Architecture:** Go modular monolith，每個 feature 一個 package（Service + Repository + `http/` huma handlers + `postgres/` repo）。前端三個 SvelteKit app。後端模組彼此檔案不重疊，可平行；`cmd/tbite/main.go` 與 `make contract-sync` 由 orchestrator 統一處理。

**Tech Stack:** Go 1.23、chi、huma v2、pgx v5、testcontainers-go、golang-migrate、SvelteKit 2 / Svelte 5 / Tailwind 3。

---

## 執行階段

| 階段 | 內容 | 平行度 |
|---|---|---|
| A | 四個 feature 的後端模組 + 測試 | 4 agents 平行 |
| B | orchestrator：接線 `main.go`、`make contract-sync`、`go build ./...`、`go test ./...` | 序列 |
| C | 三個 app 的前端（依 app 切分，非依 feature） | 3 agents 平行 |
| D | orchestrator：`make test-go` / `make test-web` / `make build`、修正、commit | 序列 |

## 全域約束（所有 agent 必讀）

1. **不得修改** `services/api/cmd/tbite/main.go`、`services/api/internal/httpserver/`、`contract/openapi/`、`packages/api-client/`。
2. **不得執行任何 `git` 指令**（commit 由 orchestrator 統一處理，避免平行 index 衝突）。
3. Go 指令一律在 `services/api/` 目錄下執行；測試範圍限縮到自己的 package。
4. SQL 一律 `pgx` `$N` 參數化綁定，禁止 `fmt.Sprintf` 組 SQL（CI 有 grep 擋）。
5. 金額一律 `money_minor`（BIGINT），禁止浮點。
6. 遵循 TDD：先寫測試（repo 測試對齊 `services/api/internal/payroll/postgres/testhelper_test.go` 的 testcontainers 模式）。
7. 匯出的 huma handler 模組需提供 `Register(huma.API)` 函式（對齊既有模組，如 `services/api/internal/payroll/http/`）。
8. migration 檔需一次寫完整（Write 整檔），不留半成品 — testcontainers 會套用整個 `migrations/` 目錄。

---

## 階段 A — 後端（4 agents 平行）

### A1 — Feature F1：員工回饋（`feedback` 模組）

**設計**：設計文件 §3。

**Files:**
- Create: `migrations/000009_feedback.up.sql`、`migrations/000009_feedback.down.sql`
- Create: `services/api/internal/feedback/types.go`、`errors.go`、`repository.go`、`service.go`、`service_test.go`
- Create: `services/api/internal/feedback/postgres/rating_repo.go`、`complaint_repo.go`、`*_repo_test.go`、`testhelper_test.go`
- Create: `services/api/internal/feedback/http/handlers.go`（員工 + 商家 + 福委會 handler，皆掛在同一 `Register`）
- Create: `services/api/internal/feedback/scanner.go`、`scanner_test.go`（`FeedbackScanner`）

**內容：**
- migration：見設計文件 §3.1 的完整 DDL。down 檔 drop 兩表與兩個 enum。
- 客訴狀態機：見 §3.2；轉換 `Respond` / `Escalate` / `EmployeeResolve` / `AdminResolve`。`Escalate` 須檢查 `now >= created_at + 24h`，違反回 409。
- 規則：見 §3.3（限 `picked_up` 訂單、評分 1–5、客訴 description 5–1000 字、回覆/結案文字 ≥ 5 字、每訂單一筆未結案客訴）。
- `FeedbackScanner`：見 §3.5。注入 `compliance.AnomalyRepository`（介面見 `services/api/internal/compliance`）。對齊 `services/api/internal/compliance/scanner/document_expiry.go` 的排程結構與 `services/api/internal/compliance/evaluator/ontime_rate_test.go` 的測試結構。
- handler API endpoints：見 §3.4（9 個）。授權沿用既有 auth middleware，參考 `services/api/internal/payroll/http/handlers.go` 取得 session 中的 user_id / role / vendor_id。

**測試要求：** repo 測試（CRUD + partial unique index 行為）；service 測試（每個狀態轉換 + 非法轉換 + 24h gate + picked_up 限制 + 重複評分 409）；scanner 測試（門檻觸發、樣本不足不觸發、anomaly 去重）。

**Verification:** `cd services/api && go test ./internal/feedback/...` 全綠；`go vet ./internal/feedback/...` 無誤。

### A2 — Feature F2：商家對帳（`settlement` 模組）

**設計**：設計文件 §4。

**Files:**
- Create: `migrations/000010_vendor_settlement.up.sql`、`migrations/000010_vendor_settlement.down.sql`
- Create: `services/api/internal/settlement/types.go`、`errors.go`、`repository.go`、`service.go`、`service_test.go`
- Create: `services/api/internal/settlement/postgres/settlement_repo.go`、`settlement_repo_test.go`、`testhelper_test.go`
- Create: `services/api/internal/settlement/http/handlers.go`（商家 + 福委會 handler）

**內容：**
- migration：見設計文件 §4.1 的完整 DDL。down drop 表與 enum。
- 計入規則：見 §4.2 — `gross_minor = Σ total_price_minor`、計入 `status ∈ {picked_up, no_show}`、期間以 `supply_date` 切。**已核對 `services/api/internal/payroll/service.go` `BuildDraft`：payroll 同樣聚合 picked_up/no_show、refunded 排除 — 直接對齊。**
- 關帳 `CloseSettlement(periodStart, periodEnd)`：逐 vendor 聚合 → 寫 `vendor_settlement` 列。重複關同期間（已有 `status='closed'` 列）回 409。
- 即時推導 `Reconciliation(vendorID, period)`：從 `order` 即時算，含狀態分布。
- handler API endpoints：見 §4.4（6 個）。商家 endpoint vendor_id 由 session 解析；`/settlements/{id}` 須校驗歸屬（非本人 vendor → 404）。寫操作（close/void）寫 `audit_event`。

**測試要求：** repo 測試（active partial unique index、void 後可重關）；service 測試（picked_up/no_show 計入、cancelled/refunded 排除、跨 vendor 聚合、重複關帳 409、void、跨 vendor 歸屬校驗）。

**Verification:** `cd services/api && go test ./internal/settlement/...` 全綠；`go vet ./internal/settlement/...` 無誤。

### A3 — Feature F3：菜單搜尋與篩選（擴充 `menu`）

**設計**：設計文件 §5。

**Files:**
- Modify: `services/api/internal/menu/service.go`（`ListForEmployee` 改吃 `EmployeeMenuFilter`）
- Modify: `services/api/internal/menu/types.go`（新增 `EmployeeMenuFilter` struct）
- Modify: `services/api/internal/menu/postgres/`（員工菜單查詢 repo，加入 SQL 篩選/排序）
- Modify: `services/api/internal/menu/http/handlers.go`（員工菜單 endpoint 加 query 參數）
- Modify/Create: 對應 `*_test.go`

**內容：**
- `EmployeeMenuFilter`：欄位見設計文件 §5.1（`Q`、`Tags []string`、`PriceMin`、`PriceMax`、`InStock`、`Sort`）。所有欄位選用。
- 篩選/排序全部下推 repo 層 SQL：`q` 用 `ILIKE`；`tags` 用陣列 overlap `&&`（`menu_item.tags` 為 `TEXT[]`）；price 區間；`in_stock` 排除 sold_out；`sort` 對應 `name / price_asc / price_desc / remain`。
- handler：對既有員工菜單 endpoint 加選用 query 參數（huma `query:"..."` tag），不破壞無參數時的既有行為（向後相容）。
- **不得改動** merchant 菜單 endpoint 與其他 menu 既有行為。

**測試要求：** repo/service 測試覆蓋每個篩選維度、組合篩選、各排序、空參數＝既有行為。

**Verification:** `cd services/api && go test ./internal/menu/...` 全綠（含既有測試不回歸）；`go vet ./internal/menu/...` 無誤。

### A4 — Feature F4：商家合規自查（擴充 `compliance`）

**設計**：設計文件 §6。

**Files:**
- Create: `services/api/internal/compliance/http/merchant_handlers.go`（商家自查 handler）
- Modify: `services/api/internal/compliance/service.go` 或 Create 輔助檔（合規摘要 + warnings 計算）
- Create/Modify: 對應 `*_test.go`

**內容：**
- 新 endpoint `GET /api/merchant/compliance`：vendor_id 由 session 解析（**不**走 path param）。
- 回傳 vendor 狀態 + 文件清單（重用既有 `compliance.Service.ListVendorDocuments`）+ `warnings[]`。
- warnings 計算邏輯：見設計文件 §6.1（`document_rejected` / `document_expired` / `document_expiring`〔30 天內〕/ `document_missing`）。必繳文件常數：`business_license`、`food_safety_permit`、`tax_registration`、`insurance`。
- 新 handler 須掛進 compliance 的 `Register`（或既有 handler 註冊流程）—— 注意僅在 compliance package 內，不碰 main.go。
- **不得改動** 既有 admin 文件 endpoint 行為。

**測試要求：** service 測試覆蓋四種 warning 的觸發與不觸發、各 vendor 狀態。

**Verification:** `cd services/api && go test ./internal/compliance/...` 全綠（既有測試不回歸）；`go vet ./internal/compliance/...` 無誤。

---

## 階段 B — 整合（orchestrator）

1. 編輯 `cmd/tbite/main.go`：
   - 建構四個模組的 Service + Repo，把各 `Register` 加入 `apiBuilders`。
   - `--role=scheduler` 分支加入 `FeedbackScanner`（對齊既有 `docScanner` 接線），讀 env `FEEDBACK_SCAN_INTERVAL` / `FEEDBACK_SCAN_WINDOW`。
2. `cd services/api && go build ./...` — 修正接線錯誤。
3. `make migrate-up`（對 dev DB 套用 000009 / 000010）。
4. `make contract-sync` — 重新產生 OpenAPI + TS client；確認無非預期 drift。
5. `cd services/api && go test ./...` — 全綠。
6. commit 階段 A + B 成果。

## 階段 C — 前端（3 agents 平行，依 app 切分）

每個 agent 負責一個 app 的全部新 UI，app 之間零檔案重疊。共用 nav/layout 在各 app 內由該 agent 統一處理。

- **C1 — `apps/employee`**：F1 評分＋客訴（`orders/[id]` 評分區塊 + 新 `/complaints` 列表）、F3 菜單搜尋篩選列。
- **C2 — `apps/merchant`**：F1 客訴收件匣（`/complaints`）、F2 對帳頁（`/reconciliation`）、F4 合規自查頁（`/compliance`）。
- **C3 — `apps/admin`**：F1 已升級客訴處理（`/complaints`）、F2 商家結算總覽（`/vendor-settlements`）。

各 agent：沿用該 app 既有 SvelteKit form action + `+page.server.ts` 載入模式與既有元件樣式；用重新產生的 `@tbite/api-client` 型別；不得執行 `git`。

**Verification（每 agent）:** `pnpm --filter <app> check` 與 `lint` 通過。

## 階段 D — 驗證（orchestrator）

1. `make test-go` 全綠。
2. `make test-web` 全綠。
3. `make build` 成功。
4. 視情況跑 `make test-e2e`。
5. 修正所有問題，逐 feature commit，最後彙整。

## MCP（時間允許才做）

設計文件 §8：加 `feedback.rate_order`、`feedback.file_complaint`、`settlement.close_period` 三個薄包裝 tool。可裁切。
