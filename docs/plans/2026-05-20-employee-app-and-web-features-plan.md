# 員工 App ＋ Web 補功能 — 實作計畫

> **For Claude:** 設計細節見 `docs/plans/2026-05-20-employee-app-and-web-features-design.md`(以下稱「設計文件」)。本計畫以 TDD 為原則,每項獨立 commit。

**Goal:** 實作 Web 既有頁面補功能(A1–A5)與員工手機 App(Tauri 2 / iOS+Android),並加一個 demo 用的 seed DB 指令。

**Architecture:** Go modular monolith(每模組 Service + Repository + `http/` huma handler + `postgres/` repo);三個 SvelteKit web app;新增 `apps/employee-app` 為 SvelteKit 靜態 SPA ＋ Tauri 2。

**Tech Stack:** Go 1.23、chi、huma v2、pgx v5、testcontainers-go、golang-migrate、NATS JetStream、AWS SDK v2(S3)、SvelteKit 2 / Svelte 5 / Tailwind 3、Tauri 2。

---

## 全域約束

1. 金額一律 `*_minor`(BIGINT),禁止浮點。
2. SQL 一律 `pgx` `$N` 參數化綁定。
3. 契約由 huma 註解產生 → 改完執行 `make contract-sync`。
4. 後端 TDD:repo 測試用 `postgres/` 的 testcontainers 模式(`testhelper_test.go`)。
5. 每個邏輯單元獨立 commit。

---

## M1 — 後端與契約基礎

### B1. 餐點圖片上傳

**Files:**
- Modify: `services/api/internal/menu/http/handlers.go`(新 upload 操作、`createItem`/`updateItem` input 加 `images`)
- Modify: `services/api/internal/menu/service.go`、`menu/types.go`(`CreateItemInput`/`UpdateItemInput` 加 `Images []string`)
- Modify: `services/api/internal/menu/postgres/item_repo.go` 或 `image_repo.go`(create/update 時同步 `menu_item_image`)
- Modify: `services/api/cmd/tbite/main.go`(注入 `*storage.S3Client` 給 menu API)

**Steps(TDD):**
1. 寫 `image_repo` 的 `ReplaceForItem(ctx, itemID, uris)` 測試 → 實作。
2. `menu.Service.CreateItem`/`UpdateItem` 接受 `Images`,寫測試覆蓋 → 實作 threading。
3. huma 新操作 `uploadMerchantImage`:`POST /api/merchant/uploads`,multipart;handler 驗證 content-type(jpeg/png/webp)與大小(≤2MB),呼叫 `S3Client.PutObject`,回 `{ url }`。
4. `createItem`/`updateItem` input DTO 加 `Images []string`。
5. `make contract-sync`;`go test ./internal/menu/...`。

### B2. 員工薪資逐筆明細端點

**Files:**
- Modify: `services/api/internal/payroll/service.go`、`payroll/types.go`(新 `ListCurrentLines`)
- Modify: `services/api/internal/payroll/postgres/`(新 query — 取未結批次期間內該員工訂單彙總)
- Modify: `services/api/internal/payroll/http/handlers.go`(新操作 `getMyCurrentPayroll`)

**Steps:**
1. 寫 repo 測試:給定員工 + 一批訂單,回逐筆 `{order_id, supply_date, vendor_name, items_summary, amount_minor, status, rated, complaint_id}`。
2. 實作 query(join `order`、`vendor`、`meal_rating`、`meal_complaint`)。
3. `Service.ListCurrentLines(ctx, userID)` + 測試。
4. huma 操作 `GET /api/employee/payroll/current`。
5. `make contract-sync`;`go test ./internal/payroll/...`。

### B3. 代扣自動沖銷 ⚠️

**先做調查**:細讀 `payroll/settler/settler.go`、`payroll/service.go` 的 `BuildDraft`、order 取消流程、complaint resolve 流程,確認沖銷掛點與冪等性。

**Files:**
- Modify: `services/api/internal/payroll/service.go`(新 `ReverseEntryForOrder`)
- Modify: order 取消 與 complaint resolve 的 service(成立時呼叫沖銷或發事件)
- 可能新增 migration:`payroll_entry` 沖銷記錄欄位 / `payroll_reversal` 表

**Steps:**
1. 寫測試:已收費訂單取消 → 產生等額沖銷,逐筆行 `status=reversed`,批次 `refunded_minor` 反映。
2. 寫測試:同事件重放不重複沖銷(冪等)。
3. 實作最安全路徑(訂單取消後沖銷);complaint 補償沖銷若語意需假設,保守實作並於 PR 標註。
4. `go test ./internal/payroll/...`。

### B4. 行動版 OIDC 回呼

**Files:**
- Modify: `services/api/internal/identity/http/handlers.go`(`completeLogin` 依 `client`/`app` 決定 redirect)
- Modify: `services/api/internal/identity/service.go`(`CompleteLogin` 回傳或接受 client 類型)
- Modify: `config/config.go`(新 `AppDeepLinkScheme`,預設 `tbite`)

**Steps:**
1. 寫測試:`app=employee-app`(allowlist)時 redirect 為 `tbite://auth?token=...`。
2. 寫測試:未知 app 值 fallback 至既有 web landing。
3. 實作;`go test ./internal/identity/...`。

### B5. 接線與契約

- `cmd/tbite/main.go` 注入新依賴;`make contract-sync`;`make build`;`go test ./...`。

---

## M2 — Web 補功能(A1–A5)

### A5. 商家圖片上傳 UI
- Modify: `apps/merchant/src/routes/menus/[id]/+page.svelte`、`menus/[id]/+page.server.ts`、`menus/new/+page.svelte`(+server)
- Create: `apps/merchant/src/lib/components/ImageUploader.svelte`
- 檔案輸入 → `POST /api/merchant/uploads` → URL 入 `images`;縮圖、刪除、排序;`?/update`/create action 帶 `images`。

### A1+A2+A3. 員工薪資頁
- Modify: `apps/employee/src/routes/payroll/+page.svelte`、`+page.server.ts`
- Create: `apps/employee/src/lib/components/PayrollEntrySheet.svelte`
- 加載 `/api/employee/payroll/current`;「本月進行中」累加 hero ＋ 逐筆列;每列開 sheet(評分/客訴,打既有 rate/complaint API);`reversed` 列負額呈現。

### A4. 員工首頁檢視切換
- Modify: `apps/employee/src/routes/+page.svelte`
- Create: `apps/employee/src/lib/components/MenuViewToggle.svelte`
- 依店家:`filteredMenu` 以 `vendor_id` client 端分組渲染;`?view=` ＋ localStorage。

**驗證:** `pnpm --filter @tbite/merchant check`、`pnpm --filter @tbite/employee check`、`make build`。

---

## M3–M5 — 員工 App(Tauri)

### M3. Scaffold ＋ Auth
- Create: `apps/employee-app/`(SvelteKit ＋ `adapter-static`、`ssr=false`)、`apps/employee-app/src-tauri/`(Tauri 2,iOS+Android)
- `pnpm-workspace.yaml` 納入;重用 `@tbite/ui`/`tokens`/`api-client`。
- Auth:`tauri-plugin-deep-link` 註冊 `tbite://`;系統瀏覽器登入 → 攔截 deep link → token 存 `tauri-plugin-stronghold`/keychain。
- 驗證:`pnpm --filter @tbite/employee-app build`(SPA);Tauri 原生建置需 Rust/Xcode/Android SDK,於本機/CI 後續驗。

### M4. 全畫面 1:1
- 依 `T-Bite App.html`:Bottom nav、HomeScreen、VendorDetail、CartSheet、OrdersScreen、TotpScreen、PayrollScreen ＋ EntryDetailSheet、ProfileScreen、FavoritesScreen、NotifModal。
- 各畫面 client fetch `/api/employee/*`;購物車沿用 `cart.svelte` 模式。

### M5. 打包與簽署
- iOS/Android 圖示、splash、`tauri.conf.json` bundle 設定、簽署。⚠️ 需原生 toolchain,列為後續。

---

## Seed DB 指令(demo)

- Create: `scripts/seed/`(seed 程式或 SQL)、`Makefile` 新目標 `seed`。
- 內容:demo 廠區、已核准商家、菜單品項(含圖片)、員工、近期訂單與供應量,讓三個 web 與 App 一啟動即有資料可展示。
- 驗證:`make seed` 後登入可見資料。

---

## 執行與里程碑驗證

| 里程碑 | 驗證指令 |
| --- | --- |
| M1 | `cd services/api && go test ./...`;`make contract-sync` 無 diff 漏失 |
| M2 | `pnpm -r check`;`make build` |
| M3 | `pnpm --filter @tbite/employee-app build` |
| seed | `make seed` 成功且資料可見 |

完成後:自行發 PR、自我 code review,PR 描述如實標註已驗證/待原生 toolchain 驗證的部分。
