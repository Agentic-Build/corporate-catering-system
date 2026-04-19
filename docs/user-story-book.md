# 企業訂餐系統 — User Story Book

> 本文件依據 `INITIAL.md` 的三方訪談結論，配合前端 (`apps/web`)、Rust 後端 (`src/`)、OpenAPI 契約 (`contract/openapi/openapi.yaml`) 與 TypeScript client (`contract/generated/ts-client/`) 的實際實作彙整而成。目的是把每個使用者在系統裡能做的**所有**操作、每一條流程、每種結果都列清楚，讓後續 UI 重整、測試設計、產品規劃能有一份單一權威來源 (Single Source of Truth)。
>
> **撰寫格式**：以 Agile 標準 User Story 句型「**身為 <角色>，我想要 <操作>，以便 <目標>**」為骨架，並附上：
> - **觸發位置**（路由 / 元件 / 按鈕）
> - **前置條件**（權限、資料狀態、業務規則）
> - **操作流程**（逐步步驟）
> - **API / 後端行為**（HTTP endpoint / MCP tool）
> - **成功結果** / **UI 反饋**
> - **失敗情境 & 錯誤碼**
>
> **命名慣例**：Story 以 `<ROLE>-<EPIC>-<序號>` 編碼，例如 `EMP-MENU-03` = 員工菜單 epic 第 3 個 story。

---

## 目錄

1. [角色總覽](#1-角色總覽)
2. [通用機制（跨角色）](#2-通用機制跨角色)
3. [員工 Employee User Stories](#3-員工-employee-user-stories)
4. [商家 Vendor Operator User Stories](#4-商家-vendor-operator-user-stories)
5. [福委會管理員 Committee Admin User Stories](#5-福委會管理員-committee-admin-user-stories)
6. [薪資操作員 Payroll Operator User Stories](#6-薪資操作員-payroll-operator-user-stories)
7. [AI Agent / MCP 用戶 Stories](#7-ai-agent--mcp-用戶-stories)
8. [狀態機與 Lifecycle 一覽](#8-狀態機與-lifecycle-一覽)
9. [錯誤碼與例外處理對照表](#9-錯誤碼與例外處理對照表)
10. [Epic × Story 索引](#10-epic--story-索引)

---

## 1. 角色總覽

系統定義 **4 個第一人稱角色** 以及 **1 個機器角色**：

| Code | 角色 | 認證方式 (`AuthenticationSource`) | 廠區範圍 (`PlantScope`) | 主要入口 |
|---|---|---|---|---|
| `Employee` | 員工 | `CORPORATE_SSO` | `Restricted(Vec<PlantId>)`（只能看自己廠區） | `/employee` |
| `VendorOperator` | 合作商家操作員 | `VENDOR_ACCOUNT_MFA` | `Restricted`（只能看自己簽約的廠區） | `/vendor` |
| `CommitteeAdmin` | 福委會管理員 | `CORPORATE_SSO` | `AllPlants`（全廠區） | `/admin` |
| `PayrollOperator` | 薪資操作員（HR 整合） | `CORPORATE_SSO` | `AllPlants` | `/api/v1/integrations/payroll/*`（無 UI，純 API） |
| `AI Agent` | AI Agent（MCP 用戶） | `OAUTH_SERVICE_ACCOUNT` | 依 token 而定 | `/mcp/v1/*` |

> 定義位置：`src/identity.rs` L1–223、`src/access.rs` L1–238；前端角色判斷：`apps/web/src/lib/server/auth/*`。

**角色相互獨立**：同一個自然人可同時具備多個 `Role`，但在介面上僅呈現單一 portal（切換由會話決定）。

---

## 2. 通用機制（跨角色）

以下為影響每個角色體驗的「系統級規則」，後續 story 會多次引用。

### 2.1 時間與區域
- 全系統使用 **Asia/Taipei (UTC+8)** 作為 business time。
- 截單時間以「**前一天的分鐘數**」表示（例如 `1020` = 配送日前一天 17:00）。
- Epoch day / minute of day 參數皆基於台北時區。

### 2.2 金額
- 一律使用 `money_minor` (BIGINT) 儲存，**禁止浮點**。
- 顯示時依 `currency` 分為 TWD/USD 等；預設 TWD，`amountMinor: 12000` 代表 NT$120。

### 2.3 認證 & 授權
- 前端以 Bearer Token 呼叫 `/api/v1/*`（`apps/web/src/lib/server/auth/api-bearer.ts`）。
- 後端在每個 handler 經由 `AccessController::authorize()` 檢查 `(Role, Action, TargetScope)`。
- MCP tool 以 `capability domain` 為邊界，分 `READ_ONLY / WRITE / HIGH_RISK_WRITE` 三級。

### 2.4 錯誤模型
- 標準化為 `{ code: ErrorCode, message, details[] }`；`ErrorCode` 列於 `openapi.yaml` L1340–1391。
- 前端 `apps/web/src/lib/platform/api/failure.ts` 將 HTTP 狀態碼本地化為繁中訊息。

### 2.5 稽核 (Audit)
- 所有「寫入」操作都會寫一筆 `ImmutableAuditEvidence`，含 `actorId / role / authenticationSource / action / entityType / entityId / correlationId / reason`。
- `audit_event` table 有 **trigger 禁止 UPDATE/DELETE/TRUNCATE**（append-only）。
- 可透過 `/api/v1/admin/audit/*` 查詢與責任歸屬。

### 2.6 通知系統（前端）
- 最多同時顯示 **5–6 筆**通知，自動 6 秒消失。
- 類型：`success`（綠）/ `error`（紅）/ `info`（藍）。
- 位置：各 `*-portal-mvp.svelte` 的 `notifications` store。

---

## 3. 員工 Employee User Stories

### 3.1 員工 Portal 全貌

入口：`/employee`（`apps/web/src/routes/employee/+page.svelte`）→ 元件 `employee-portal-mvp.svelte` (L1–1240)。

Portal 分三大 section：

| Section ID | 中文 | 路由 | 用途 |
|---|---|---|---|
| `overview` | 總覽 | `/employee` 或 `/employee/overview` | 今日餐點狀態、提醒，並把菜單瀏覽+下單+訂單管理一起呈現 |
| `orders` | 訂單 | `/employee/orders` | 管理預購、修改、取消、顯示領餐 QR |
| `payroll` | 薪資扣款 | `/employee/payroll` | 扣款流水查詢、申訴提交、追蹤狀態 |

---

### 3.2 EPIC EMP-MENU：菜單瀏覽與下單

#### `EMP-MENU-01` 以「週檢視」瀏覽未來 7 天菜單
**身為員工，我想要以週檢視快速看到本週每一天的菜單，以便決定未來數天的午餐安排。**

- **觸發位置**：`employee-portal-mvp.svelte` L791–942，「菜單瀏覽與下單」區塊。
- **前置條件**：已登入、`PlantScope` 有至少 1 個 `PlantId`，API 可連線。
- **流程**：
  1. 預設載入今日起 14 天菜單；view = `week`，menuDate = 今日。
  2. 員工在「檢視切換」選 `week`，輸入週起始日。
  3. 點擊「套用條件」重新載入。
- **API**：`GET /api/v1/employee/menus?plantId=&view=week&menuDate=&page=1&pageSize=200&sortBy=deliveryDate&sortOrder=asc`
- **結果**：回傳 `MenuPage { days, items, recommendationApplied, recommendationRequested }`；前端渲染菜單卡片網格，每張卡片顯示圖片、名稱、價格 (`Money`)、剩餘數量、截單倒數、`preorderOpen`、`healthTags`、`menuType`。
- **失敗**：
  - 400 → 「API 請求格式錯誤」
  - 401 → 「API 驗證失敗，請重新登入」
  - 403 → 「API 權限不足」
  - 500 → 「後端服務發生內部錯誤」
  - 無 `PlantScope` → `PlantScopeError`，顯示「目前登入帳號沒有可用的廠區範圍」

#### `EMP-MENU-02` 以「日曆檢視」指定日期範圍查菜單
**身為員工，我想要指定任意起訖日期，以便一次看完連假前後完整的菜單排程。**

- **流程**：切換 view = `calendar`，填 `fromDate` / `toDate`，點「套用條件」。
- **API**：`GET /api/v1/employee/menus?view=calendar&fromDate=&toDate=&plantId=...`
- **結果**：同 `EMP-MENU-01`，但依日期分群。

#### `EMP-MENU-03` 看到菜單商品的關鍵資訊
**身為員工，我想要在每張菜單卡片清楚看到剩餘數量、截單倒數、健康標籤、圖片，以便快速決定要不要下單。**

- **欄位**（來源 `MenuListItem`）：`menuItemId, name, description, price{amountMinor, currency}, remainingQuantity, preorderOpen, deliveryDate, cutoffDate, modifyCancelCutoffMinuteOfDay, healthTags[], menuType, imageUrl, vendorId, preorderOpenDaysAhead, earliestDeliveryDate, latestDeliveryDate`。
- **倒數邏輯**：`taipeiDateMinuteToEpochMs(cutoffDate, modifyCancelCutoffMinuteOfDay)` → 與 `Date.now()` 差值呈現 `mm:ss`；過時顯示「已截止」（`apps/web/src/lib/employee/portal.ts` L21–54）。

> 🚩 **API 已支援、前端尚未展示**：`search`（菜單搜尋）、`menuType` / `healthTag` / `priceMinMinor` / `priceMaxMinor` / `remainingQuantity` 篩選、排序 `name / priceMinor / remainingQuantity`。第二階段可補 UI。

#### `EMP-MENU-04` 下單（建立預購訂單）
**身為員工，我想要在看到喜歡的餐點時直接下單，以便鎖定份數避免被搶完。**

- **觸發位置**：每張菜單卡片的「立即下單」按鈕。
- **前置條件**：
  - `preorderOpen == true`
  - `remainingQuantity > 0`
  - 未超過截單 (`now < cutoff`)
  - 下單數量 `1 ≤ q ≤ min(20, remainingQuantity)`
- **流程**：
  1. 員工在數量輸入框輸入 `q`（預設 1）。
  2. 可在菜單上方「訂單備註」欄輸入全局備註（≤ 200 字，附加到 `employeeNote`）。
  3. 點擊「立即下單」。
  4. 前端驗證 → 呼叫 API。
  5. 成功後：清空數量 / 備註、重新載入菜單 & 訂單列表。
- **API**：`POST /api/v1/employee/orders`  
  Body：`{ plantId, deliveryDate, lineItems: [{ menuItemId, quantity, specialRequests: [] }], employeeNote }`
- **結果**：通知「已建立訂單：{menuName} x {q}」；訂單以 `status = PENDING` 出現在訂單區塊。
- **失敗**：
  - 數量超過庫存 → 「下單數量超過可用庫存，請調整後再試」
  - 409 `ORDER_VENDOR_DELIVERY_REJECTED` → 「此商家當前時段不可配送到你的廠區」
  - 409 `ORDER_POLICY_VIOLATION` → 「訂購規則不允許（截單已過 / 售罄）」
  - 422 `INVALID_ORDER_REQUEST` → 「API 驗證規則未通過」

#### `EMP-MENU-05` 在菜單上直接加備註
**身為員工，我想要在菜單上加通用備註（例如「少冰」），以便同一批下單都沿用。**

- **流程**：在「訂單備註」欄填文字 → 後續每筆下單都帶同一 `employeeNote`。
- **業務規則**：`specialRequests` 目前受限於後端固定選項（最多 3 個，依 `OrderLineItemRequest`）；自由文字走 `employeeNote`。

---

### 3.3 EPIC EMP-ORDER：訂單管理

#### `EMP-ORDER-01` 查看我的訂單列表
**身為員工，我想要看到所有預購中、已完成、已取消的訂單，以便掌握近期用餐安排。**

- **觸發位置**：`/employee/orders`，「訂單管理與領餐核銷」區塊。
- **API**：`GET /api/v1/employee/orders?page=1&pageSize=200&sortBy=deliveryDate&sortOrder=desc`
- **欄位**（`EmployeeOrder`）：`orderId, deliveryDate, status, total, createdAt, lineItems[{menuItemId, quantity, pricePerUnit}], timeline[]`
- **狀態**：`PENDING / MODIFIED / CANCELLED / SOLD_OUT / REFUND_PENDING / REFUNDED / FULFILLED`

> 🚩 **API 已支援、前端尚未展示**：`fromDate / toDate / status` 篩選，`sortBy status / createdAt`。

#### `EMP-ORDER-02` 修改訂單數量（REPLACE_LINE_ITEMS）
**身為員工，我想要在截單前修改訂單品項數量，以便因應臨時變更。**

- **前置條件**：
  - `status ∈ {PENDING, MODIFIED}`（`isEmployeeOrderEditable()`）
  - 未超過該菜單的截單時間
  - `isOrderMutationInFlight == false`
- **流程**：
  1. 在訂單卡片內每個 line item 的輸入框調整數量（1–20）。
  2. 暫存於 `orderEditQuantities[orderId]`。
  3. 點「送出修改」。
  4. 驗證至少 1 個 line item 數量 ≥ 1。
- **API**：`PATCH /api/v1/employee/orders/{orderId}`  
  Body：`{ operation: "REPLACE_LINE_ITEMS", lineItems: [...] }`
- **結果**：`status` 變為 `MODIFIED`；重新載入菜單 / 訂單 / 薪資明細。
- **失敗**：
  - 409 `ORDER_MUTATION_NOT_ALLOWED` → 「此訂單狀態不可修改」
  - 404 `ORDER_NOT_FOUND` → 「API 路徑不存在」

#### `EMP-ORDER-03` 取消訂單（CANCEL）
**身為員工，我想要在截單前取消訂單並寫下原因，以便避免被扣款。**

- **前置條件**：同 `EMP-ORDER-02`，外加「取消原因 ≥ 5 字」。
- **流程**：輸入取消原因（預設「行程調整取消」）→ 點「取消訂單」。
- **API**：`PATCH /api/v1/employee/orders/{orderId}`  
  Body：`{ operation: "CANCEL", cancelReason }`
- **結果**：通知「訂單 {orderId} 已取消」；`status` → `CANCELLED`；若該訂單正在顯示 QR，自動關閉。

#### `EMP-ORDER-04` 顯示領餐 QR（TOTP）
**身為員工，我想要在訂單可取餐時顯示一個 30 秒輪轉的 QR，以便到領餐點快速核銷。**

- **前置條件**：`isPickupEligible(status) == true`（`PENDING` / `MODIFIED`）。
- **流程**：
  1. 點「顯示領餐 QR」。
  2. 前端設 `activePickupOrderId`；呼叫 API。
  3. 回傳 `PickupVerificationQr { verificationCode, generatedAtEpochSecond, expiresAtEpochSecond, refreshIntervalSeconds (30), secondsUntilRefresh }`。
  4. 用 `qrcode` lib 產生 360×360 圖像（等級 M）。
  5. 每 30 秒自動呼叫 API 更新；倒數顯示在圖下方。
- **API**：`GET /api/v1/employee/orders/{orderId}/pickup-verification-qr`
- **相關後端**：`src/pickup_totp.rs`；`TOTP_STEP_SECONDS=30`、`TAIPEI_FIXED_OFFSET_SECONDS=28800`、允許 ±1 步 drift。

#### `EMP-ORDER-05` 手動刷新領餐 QR
**身為員工，我想要在讀取失敗時立即刷新 QR，以便不用等下一輪。**

- **觸發**：「立即刷新 QR」按鈕。
- **行為**：取消 pending timer → 重新呼叫 API → 重置 timer。

#### `EMP-ORDER-06` 完成領餐核銷
**身為員工，我想要在拿到餐後標記已領，以便系統關單並避免重複領取。**

- **流程**：點「完成領餐核銷」→ 把當前 QR 的 `verificationCode` 送出。
- **API**：`POST /api/v1/employee/orders/{orderId}/pickup-verifications`  
  Body：`{ verificationCode }`
- **結果**：`status` → `FULFILLED`；QR 卡片隱藏；重新載入薪資明細。
- **失敗**：
  - 409 `PICKUP_VERIFICATION_REPLAYED` → 「此驗證碼已使用過」
  - 409 `PICKUP_VERIFICATION_EXPIRED` → 「驗證碼已過期，請刷新後重試」
  - 409 `PICKUP_VERIFICATION_INVALID_WINDOW` → 「時間超出核銷視窗」
  - 409 `PICKUP_VERIFICATION_INVALID_CODE` → 「驗證碼錯誤」
  - 409 `PICKUP_VERIFICATION_STATE_CONFLICT` → 「訂單狀態不可核銷」

#### `EMP-ORDER-07` 查看訂單 timeline
**身為員工，我想要看到訂單從下單到領餐每個事件，以便追蹤狀態變化。**

- **資料**：`EmployeeOrder.timeline: OrderTimelineEvent[]`，包含狀態轉換時間、來源事件。
- **展示**：訂單卡片內的時間軸。

---

### 3.4 EPIC EMP-PAYROLL：薪資扣款與申訴

#### `EMP-PAY-01` 檢視特定訂單的薪資扣款明細
**身為員工，我想要看到每筆訂單對應的扣款流水，以便確認扣款無誤。**

- **觸發位置**：`/employee/payroll`；左側訂單列表 → 點任一筆。
- **API**：`GET /api/v1/employee/orders/{orderId}/payroll-ledger`
- **返回**（`EmployeeOrderPayrollLedger`）：
  - 訂單概況（`orderId, deliveryDate, netAmountMinor, currency`）
  - `ledgerEntries[]`：`ledgerEntryId, kind (DEDUCTION / ADJUSTMENT_DEBIT / ADJUSTMENT_CREDIT / REFUND), amount, occurredAt, sourceEventKind (ORDER_MUTATION / DISPUTE_WORKFLOW / SFTP_BATCH_EXPORT / HR_API_SYNC_ADJUNCT), sourceEventReference`
  - `disputes[]`（見 `EMP-PAY-03`）

#### `EMP-PAY-02` 檢視帳務摘要
**身為員工，我想要看到本月已載入訂單的總扣款、申訴進度，以便心中有譜。**

- **計算**（前端）：
  - `loadedOrderCount`、`netAmountMinor` 總和
  - `openDisputeCount`（狀態不是 `RESOLVED_*`）
  - `resolvedDisputeCount`

#### `EMP-PAY-03` 提交薪資申訴
**身為員工，我想要針對扣款錯誤提出申訴，以便追回款項或取得說明。**

- **前置條件**：已載入薪資明細、申訴理由非空。
- **流程**：在 textarea 填理由 → 點「送出申訴」。
- **API**：`POST /api/v1/employee/orders/{orderId}/disputes`  
  Body：`{ reason }`
- **結果**：新申訴加入 `disputes[]`，狀態 `OPEN`。
- **失敗**：
  - 空白 → 「申訴原因不可為空白」
  - 409 → 「該訂單已有未結案申訴 / 狀態不允許申訴」

#### `EMP-PAY-04` 追蹤申訴狀態與負責人
**身為員工，我想要看到申訴被指派給誰、現在到哪一步，以便估計何時能拿到退款。**

- **欄位**（`PayrollDispute`）：`disputeId, status (OPEN / IN_REVIEW / RESOLVED_REFUND_APPROVED / RESOLVED_REJECTED), openedAt, updatedAt, ownerActorId, trace[]`
- **Trace 事件**：`OPENED / OWNER_ASSIGNED / RESOLVED_REFUND_APPROVED / RESOLVED_REJECTED`

---

### 3.5 EPIC EMP-NOTIFY：提醒與偏好（選配）

#### `EMP-NOTIFY-01` 設定尖峰提醒偏好
**身為員工，我想要設定開賣提醒，以便搶到熱門餐點。**

- **API**：`PUT /api/v1/employee/rush-reminder-preferences`（前端尚未實裝 UI）
- **後端**：`src/rush_reminder.rs`
- **狀態**：MVP 範圍外，列於 `INITIAL.md` 第二階段；故此 story 目前為**佔位**，待 UI 補齊。

---

### 3.6 員工端業務規則速查
- **下單數量**：1 ≤ q ≤ min(20, remainingQuantity)。
- **修改**：line item 數量 1–20，整張訂單至少 1 筆 line item。
- **取消原因**：≥ 5 字。
- **截單時間** = `cutoffDate - 1 day` 00:00 + `modifyCancelCutoffMinuteOfDay` 分鐘（台北時區）。
- **QR 輪轉**：30 秒，允許 ±1 步 drift。
- **狀態流**：`PENDING → MODIFIED`（修改）/`CANCELLED`（取消）/`FULFILLED`（領餐）；`SOLD_OUT`、`REFUND_PENDING`、`REFUNDED` 為非互動終態。

---

## 4. 商家 Vendor Operator User Stories

入口：`/vendor`（`apps/web/src/routes/vendor/+page.svelte`）→ 元件 `vendor-portal-mvp.svelte` (L1–1684)。

| Section ID | 中文 | 路由 |
|---|---|---|
| `overview` | 總覽 | `/vendor` 或 `/vendor/overview` |
| `menu` | 菜單與訂購視窗 | `/vendor/menu` |
| `fulfillment` | 履約配送 | `/vendor/fulfillment` |
| `docs` | 文件與物件儲存 | `/vendor/docs` |

---

### 4.1 EPIC VEN-MENU：菜單與供應量管理

#### `VEN-MENU-01` 查看菜單清單並依狀態 / 日期篩選
**身為商家，我想要列出自家菜單並依日期範圍 / 狀態篩選，以便快速定位要修改的項目。**

- **API**：`GET /api/v1/vendor/menu-items?fromDate=&toDate=&status=(ALL|LISTED|PAUSED|DELISTED)&sortOrder=asc&page=1&pageSize=200`
- **返回欄位**：`menuItemId, name, description, deliveryDate, price, maxDailyQuantity, remainingQuantity, status, healthTags, menuType`

#### `VEN-MENU-02` 新增或更新菜單項目
**身為商家，我想要上架新菜單或修改既有菜單，以便反映每日供應。**

- **流程**（`vendor-portal-mvp.svelte` L49–1202，「菜單建立 / 更新」表單）：
  1. 填 `menuItemId`（可 auto-gen）、配送日、名稱（1–80 字）、描述（1–280 字）。
  2. 選 `menuType` (BENTO / BOWL / NOODLE / SALAD / SNACK / DRINK)。
  3. 選 `healthTags`（逗號：LOW_CALORIE / HIGH_PROTEIN / VEGETARIAN / VEGAN / GLUTEN_FREE）。
  4. 選填 `imageUrl`（≤ 512 字；支援 jpg/jpeg/png/webp，≤ 10MB）。
  5. 設 `currency`（預設 TWD）、`price` (minor)、`maxDailyQuantity` (1–2000)。
  6. 可覆寫 `preorderOpenDaysAheadOverride`（1–7）與 `modifyCancelCutoffMinuteOfDayOverride`（900–1200）。
  7. 送「送出菜單更新」。
- **API**：`PUT /api/v1/vendor/menu-items/{menuItemId}`
- **業務規則**（`src/menu_supply_window.rs`）：名稱 ≤ 80、描述 ≤ 280、圖片 URL ≤ 512、`maxDailyQuantity` ≤ 2000、開放天數 1–7 (預設 7)、截單分鐘 900–1200 (預設 1020)。
- **失敗**：422 欄位驗證失敗；409 業務規則衝突。

#### `VEN-MENU-03` 載入既有菜單到表單（複製）
**身為商家，我想要從菜單列表載入一筆資料到表單修改，以便快速複製昨天的菜單。**

- **流程**：列表「載入到表單」按鈕 → 所有欄位預填 → 修改 `menuItemId` + 配送日 → 送出變成新菜單。

#### `VEN-MENU-04` 切換菜單上下架狀態
**身為商家，我想要把賣完或暫停的菜單轉成 `PAUSED` 或 `DELISTED`，以便不讓員工下單。**

- **觸發位置**：菜單列表「狀態」按鈕。
- **API**：`PATCH /api/v1/vendor/menu-items/{menuItemId}/status`  
  Body：`{ status: "LISTED" | "PAUSED" | "DELISTED" }`
- **狀態語意**：`LISTED` 可預購、`PAUSED` 暫停接單但可恢復、`DELISTED` 永久下架。
- **失敗**：409 當前狀態已相同或違反狀態機；404 菜單不存在。

#### `VEN-MENU-05` 查看本店的訂購視窗政策
**身為商家，我想要知道目前全店的預購開放天數與截單時間，以便掌握節奏。**

- **API**：`GET /api/v1/vendor/ordering-policy`
- **返回**：`{ preorderOpenDaysAhead, modifyCancelCutoffMinuteOfDay }`

#### `VEN-MENU-06` 調整全店訂購視窗政策
**身為商家，我想要改掉預設 7 天 / 17:00 截單，以便配合門市備料節奏。**

- **表單驗證**：天數 1–7、分鐘 900–1200。
- **API**：`PUT /api/v1/vendor/ordering-policy`  
  Body：`{ preorderOpenDaysAhead, modifyCancelCutoffMinuteOfDay }`

#### `VEN-MENU-07` 清空菜單表單
**身為商家，我想要一鍵清空輸入中的欄位，以便重新建立下一個菜單。**

---

### 4.2 EPIC VEN-FULFILL：備餐與配送看板

#### `VEN-FULFILL-01` 查看當日履約看板
**身為商家，我想要看到指定日期 / 廠區的訂單彙總，以便備餐與分裝。**

- **表單**：配送日、plantId（唯讀，帶入本人 scope）、`includeAuditTransitions` checkbox。
- **API**：`GET /api/v1/vendor/fulfillment-board?deliveryDate=&plantId=&includeAuditTransitions=true|false`
- **返回**（`VendorFulfillmentBoardSnapshot`）：
  - **廠區彙總表**：`订单数、份数、各配送狀態分布、特殊需求分布`
  - **訂單詳情表**：`orderId, plant, lifecycleStatus, deliveryStatus, lineItems[], specialRequests`
  - **稽核軌跡表**（可選）：時間 / 操作者 / actionId / 前後狀態

#### `VEN-FULFILL-02` 推進訂單配送狀態
**身為商家，我想要在完成備餐 / 打包 / 上車 / 送達時打卡，以便同步給福委會與員工。**

- **觸發位置**：訂單行的「配送更新」下拉 + 「送出狀態更新」。
- **API**：`POST /api/v1/vendor/orders/{orderId}/delivery-status`  
  Body：`{ toStatus, occurredAt }`（系統自動填當前台北時間）
- **狀態機**（`src/vendor_fulfillment.rs` L1451–1486）：
  ```
  PENDING_PREP → PREPARING → PACKED → OUT_FOR_DELIVERY → DELIVERED
  任何狀態 → CANCELLED（終態）
  ```
- **失敗**：409 狀態轉換違規；422 時間格式錯。

#### `VEN-FULFILL-03` 建立履約匯出批次（含四層工件）
**身為商家，我想要建立一個不可變的每日批次，以便列印分區總表、標籤、配送籃清單。**

- **觸發**：「建立可列印批次」按鈕。
- **API**：`POST /api/v1/vendor/fulfillment-batches`  
  Body：`{ deliveryDate }`
- **後端產物**（四層工件，皆為 JSON + SHA256 + object storage ref）：
  1. `DAILY_SUMMARY`：日總表（訂單數 / 份數）
  2. `PLANT_PARTITION_SHEET`：廠區分區表
  3. `LABELS`：每餐標籤
  4. `BASKET_LIST`：配送籃清單（每籃 12 份）
- **結果**：通知「履約批次已建立：{batchId}」；batchId 填入查詢欄 + 加入「最近批次」。

#### `VEN-FULFILL-04` 查詢批次詳情
**身為商家，我想要查看舊批次內容以便重印或追蹤。**

- **API**：`GET /api/v1/vendor/fulfillment-batches/{batchId}`
- **返回**：`vendorId, deliveryDate, capturedAt, createdBy, artifacts[]`

#### `VEN-FULFILL-05` 列印批次
**身為商家，我想要把批次轉成紙本或 PDF，以便交給備餐現場。**

- **行為**：`window.print()`；頁面以列印樣式排版。

#### `VEN-FULFILL-06` 查詢營運訂單
**身為商家，我想要用狀態 / 日期範圍查詢訂單，以便追蹤異常。**

- **API**：`GET /api/v1/vendor/orders?plantId=&fromDate=&toDate=&page=&pageSize=&sortBy=&sortOrder=&status=(ALL|PENDING|MODIFIED|CANCELLED|SOLD_OUT|REFUND_PENDING|REFUNDED|FULFILLED)`
- **返回**：訂單列表 + 總數。

> 🚩 **API 已支援、前端尚未呈現**：營運分析儀表板 `GET /api/v1/vendor/analytics/operations-dashboard`（`fromEpochDay / toEpochDay`）可回傳商家範圍的指標破表與定義目錄。

---

### 4.3 EPIC VEN-DOCS：文件與物件儲存

#### `VEN-DOCS-01` 建立上傳計畫
**身為商家，我想要拿到一個預簽名 URL，以便直接把合規文件或菜單圖片上傳到物件儲存。**

- **API**：`POST /api/v1/vendor/object-storage/upload-plans`  
  Body：`{ artifactClass: COMPLIANCE_DOCUMENT | MENU_IMAGE | MENU_IMAGE_THUMBNAIL, filename, contentType, sizeBytes, thumbnailSizeBytes?, locale? }`
- **返回**：`{ objectRef, uploadUrl, uploadExpiresAt, thumbnailObjectRef? }`
- **大小限制**：合規文件 ≤ 20MB（PDF/JPG/PNG）、菜單圖片 ≤ 10MB（JPG/PNG/WebP）。
- **失敗**：`OBJECT_STORAGE_INVALID_ARTIFACT_CLASS` / `OBJECT_STORAGE_SIZE_EXCEEDED`。

#### `VEN-DOCS-02` 建立下載連結
**身為商家，我想要產生臨時預簽名下載連結，以便複核已上傳文件。**

- **API**：`POST /api/v1/vendor/object-storage/access-links`  
  Body：`{ objectRef, locale? }`
- **返回**：`{ objectRef, downloadUrl, downloadExpiresAt }`

---

### 4.4 EPIC VEN-COMPLIANCE：合規狀態（商家視角）

商家在後端有一個 `VendorComplianceStatus`：`PendingReview / FixRequested / Active / Rejected / Suspended`（`src/vendor_compliance.rs` L380–386）。

#### `VEN-COMPLIANCE-01` 理解目前合規狀態對我的影響
**身為商家，我想要知道自己現在處於哪個狀態、可以做什麼、不能做什麼。**

| 狀態 | 可上架菜單 | 可接訂單 | 可上傳文件 | 可執行配送 |
|---|---|---|---|---|
| `PendingReview` | ❌ | ❌ | ✅ | ❌ |
| `FixRequested` | ❌ | ❌ | ✅ | ❌ |
| `Active` | ✅ | ✅ | ✅ | ✅ |
| `Rejected` | ❌ | ❌ | 有限 | ❌ |
| `Suspended` | 有限 | ❌ | 有限 | 有限 |

> 🚩 **目前前端未顯式呈現合規狀態 banner**，建議加 UI（未來 user story）。後端 API 已回傳此狀態於 `AdminVendorRecord.status`，但 vendor 端需另開端點。

#### `VEN-COMPLIANCE-02` 上傳補件文件
**身為商家，我想要在「要求補件」時重新上傳遺漏的文件，以便恢復營運資格。**

- **流程**：透過 `VEN-DOCS-01` 上傳 → 由福委會重審（見 `ADM-VEN-02`）。

---

### 4.5 商家端業務規則速查
- **菜單**：名稱 ≤ 80、描述 ≤ 280、`maxDailyQuantity` ≤ 2000、圖片 URL ≤ 512。
- **訂購視窗**：預購開放 1–7 天 (預設 7)；截單 900–1200 分鐘 (預設 1020 = 17:00)。
- **配送狀態機**：`PENDING_PREP → PREPARING → PACKED → OUT_FOR_DELIVERY → DELIVERED` / `CANCELLED`。
- **配送籃容量**：12 份 / 籃（`BASKET_CAPACITY_PORTIONS`）。
- **TOTP 驗證碼**：由領餐現場掃描，商家可在看板看到驗證歷程；具體 verify 動作走員工端。

---

## 5. 福委會管理員 Committee Admin User Stories

入口：`/admin`（`apps/web/src/routes/admin/+page.svelte`）→ 元件 `admin-portal-mvp.svelte` (L1–1365)。

| Section ID | 中文 | 路由 |
|---|---|---|
| `overview` | 總覽 | `/admin` 或 `/admin/overview` |
| `vendors` | 商家審核 | `/admin/vendors` |
| `settlement` | 月結作業 | `/admin/settlement` |
| `anomalies` | 異常治理 | `/admin/anomalies` |
| `audit` | 稽核查詢 | `/admin/audit` |
| `analytics` | 營運分析 | `/admin/analytics` |

---

### 5.1 EPIC ADM-OVERVIEW：總覽儀表板

#### `ADM-OV-01` 檢視平台關鍵指標摘要
**身為管理員，我想要一眼看到待審核數、停權中商家、開放告警、SLA breached、月結例外筆數，以便決定今天先處理什麼。**

- **觸發位置**：`/admin` overview。
- **資料來源**：彙整 `listAdminVendors`（狀態分布）、`listAnomalyAlerts`（SLA、open count）、最近 `closePayrollMonthlySettlement` 結果。
- **前端**：`admin-portal-mvp.svelte` overview 區塊。

---

### 5.2 EPIC ADM-VEN：商家審核與生命週期

#### `ADM-VEN-01` 列出並篩選商家
- **API**：`GET /api/v1/admin/vendors?page=&pageSize=&sortBy=(createdAt|status|displayName|vendorCategory)&sortOrder=&status=(ALL|PENDING_REVIEW|FIX_REQUESTED|APPROVED|REJECTED|SUSPENDED)`

#### `ADM-VEN-02` 對商家提交審核決策（通過 / 要求補件 / 拒絕）
**身為管理員，我想要對待審商家下決策並附審核意見，以便推進或阻擋入駐。**

- **前置條件**：`status ∈ {PENDING_REVIEW, FIX_REQUESTED}`；意見 ≥ 5 字。
- **API**：`POST /api/v1/admin/vendors/{vendorId}/reviews`  
  Body：`{ decision: "APPROVED" | "REQUEST_FIX" | "REJECTED", comment }`
- **後端（`src/vendor_compliance.rs` L995–1059）**：
  - `APPROVED` 前會跑 `approval_compliance_gaps()` 驗證所有必填文件齊全且未過期。
  - 決策寫入 `reviewHistory`（append-only）。
- **失敗**：必填文件缺失 → 422；狀態衝突 → 409。

#### `ADM-VEN-03` 維護文件模板
**身為管理員，我想要定義每類商家必交哪些文件、幾天到期、提前幾天提醒、寬限幾天，以便自動化合規。**

- **API**：`PUT /api/v1/admin/compliance/document-templates/{vendorCategory}/{templateId}`  
  Body：`{ displayName, required, maxValidityDays, reminderDaysBeforeExpiry[], suspensionGraceDays }`
- **驗證**：提醒天數 ≤ maxValidityDays；去重降序；templateId / displayName 非空。

#### `ADM-VEN-04` 執行自動化合規生命週期
**身為管理員，我想要每天跑一次 lifecycle，以便自動發提醒、停權逾期商家、復權補件完成者。**

- **API**：`POST /api/v1/admin/compliance/lifecycle/executions`  
  Body：`{ runDate, dryRun? }`
- **後端**：遍歷所有商家必交文件 → 依提醒天數發事件 → 逾期超過 `suspensionGraceDays` 自動 `Suspended` → 補件後自動 `Active`。
- **結果**：`{ remindersSent, suspended, reinstated }`。

#### `ADM-VEN-05` 管理商家 × 廠區配送映射
**身為管理員，我想要精細控制哪家商家在哪個廠區、哪個時段可送達，以便避免員工看到不可送達的店家。**

- **查詢**：`GET /api/v1/admin/vendor-plant-delivery-mappings?vendorId=&plantId=&activeAt=&page=&pageSize=`
- **新增/更新**：`PUT /api/v1/admin/vendors/{vendorId}/plant-delivery-mappings/{mappingId}`  
  Body：`{ plantId, effect: "ALLOW" | "DENY", precedence: 0..65535, serviceWindow: { startsAt, endsAt } }`
- **刪除**：`DELETE /api/v1/admin/vendors/{vendorId}/plant-delivery-mappings/{mappingId}`
- **驗證**：`endsAt > startsAt`；優先級 0–65535；時段以台北時間 ISO datetime 儲存。
- **稽核**：每筆變更寫入映射的 `auditTrail`（who / when / create|update|delete）。

#### `ADM-VEN-06` 為稽核文件建立下載連結
- **API**：`POST /api/v1/admin/object-storage/access-links`  
  Body：`{ objectRef, locale }`
- **用途**：審核商家補件時下載原始 PDF。

---

### 5.3 EPIC ADM-SETTLE：月結作業

#### `ADM-SETTLE-01` 執行月結關帳（含 ISS-003 簽核）
**身為管理員，我想要關閉上一個月結週期並產出 HR 批次，以便提交薪資扣款。**

- **前置條件**：Issue checklist 必須包含 `ISS-003`（settlement release sign-off）。
- **API**：`POST /api/v1/admin/payroll/monthly-settlements/close`  
  Body：`{ cycleKey?, issueChecklist: string[], page, pageSize (1..200), sortBy, sortOrder }`
- **後端行為**：計算扣款 → 產出月結摘要（`totalRecords / disputedRecords / deductionFailedRecords`）→ 建 SFTP 匯出批次（含 checksum）→ 寫 `payroll_exchange_batch`。
- **返回**：batch metadata + 第 1 頁扣款記錄。

#### `ADM-SETTLE-02` 篩選並檢視月結例外
**身為管理員，我想要依 `DISPUTED / DEDUCTION_FAILED / EMPLOYEE_TERMINATED / REFUNDED` 篩選例外筆數，以便逐筆處理。**

- **資料**：已載入的關帳結果；前端本地篩選。
- **欄位**：`employeeActorId, orderId, amountMinor, status, exceptionClass`。

#### `ADM-SETTLE-03` 鎖定月結週期
**身為管理員，我想要在提交 HR 後鎖住週期，以便避免任何人再動。**

- **API**：`POST /api/v1/admin/payroll/monthly-settlements/{cycleKey}/lock`  
  Body：`{ reason }`
- **必要欄位**：cycleKey + reason。

#### `ADM-SETTLE-04` 解鎖月結週期（用於重新計算）
- **API**：`POST /api/v1/admin/payroll/monthly-settlements/{cycleKey}/unlock`  
  Body：`{ reason }`

#### `ADM-SETTLE-05` 處理月結爭議
**身為管理員，我想要對員工的薪資申訴指派負責人、核准退款或駁回，以便閉環。**

- **API**：`PATCH /api/v1/admin/payroll/disputes/{disputeId}`
- **三種操作**：
  - `ASSIGN_OWNER`：`{ operation, ownerActorId, note? }`
  - `RESOLVE_REFUND`：`{ operation, note (≥ 5 字), refundAmountMinor? ≥ 1 }`
  - `RESOLVE_REJECTED`：`{ operation, note (≥ 5 字) }`
- **結果**：`PayrollDispute.status` → `ASSIGNED / RESOLVED_REFUND_APPROVED / RESOLVED_REJECTED`。
- **Trace**：全部寫入 `PayrollDisputeTraceEvent`，append-only。

#### `ADM-SETTLE-06` 觸發薪資保留期清除
- **API**：`POST /api/v1/admin/payroll/retention-purge`（刪除指定日期前記錄）。
- **注意**：`payroll_ledger_entry` 有 append-only trigger；僅允許以 retention policy 進行批次清除。

---

### 5.4 EPIC ADM-ANOMALY：異常治理

#### `ADM-ANO-01` 查詢異常告警
- **API**：`GET /api/v1/admin/anomaly/alerts?vendorId=&ownerActorId=&status=(ALL|OPEN|ACKNOWLEDGED|REMEDIATION_IN_PROGRESS|ESCALATED|CLOSED)&escalatedOnly=(ALL|true|false)&slaStatus=(ALL|ON_TRACK|BREACHED)&asOfEpochDay=&asOfMinuteOfDay=`

#### `ADM-ANO-02` 推進告警狀態
**身為管理員，我想要把告警從 OPEN 走到 CLOSED，以便完成治理。**

- **API**：`PATCH /api/v1/admin/anomaly/alerts/{alertId}`
- **操作**：
  - `ASSIGN_OWNER`：`{ ownerActorId, note? }`
  - `ACKNOWLEDGE`：`{ note? }`
  - `START_REMEDIATION`：`{ note? }`
  - `ESCALATE`：`{ note? }`
  - `CLOSE`：**需 `ISS-007` 簽核**；`{ issueChecklist, closureNote (必填), closureEvidenceRefs[] (≥ 1), ticketReference?, note? }`

#### `ADM-ANO-03` 手動評估異常規則觸發告警
- **API**：`POST /api/v1/admin/anomaly/alerts/evaluations`  
  Body：`{ vendorId, defaultOwnerActorId?, daysUntilExpiry?, onTimeRate (0..1)?, satisfactionScore (0..1)?, complaintCount?, observedAtEpochDay?, observedAtMinuteOfDay? }`
- **後端**：逐一評估 `AnomalyRule` → 依 `thresholdValue / thresholdComparator` 決定是否觸發。

#### `ADM-ANO-04` 管理異常規則
- **查詢**：`GET /api/v1/admin/anomaly/rules`
- **upsert**：`PUT /api/v1/admin/anomaly/rules/{ruleId}`  
  Body：`{ kind: EXPIRY_RISK | ON_TIME_DEGRADATION | SATISFACTION_DROP | COMPLAINT_SPIKE, displayName, description, governanceIssueId, enabled, thresholdValue, thresholdComparator: LT | LTE | GT | GTE, evaluationWindowDays, slaMinutes, severity: WARNING | CRITICAL }`

---

### 5.5 EPIC ADM-AUDIT：稽核查詢

#### `ADM-AUDIT-01` 查詢操作稽核紀錄
- **API**：`GET /api/v1/admin/audit/investigations?actorId=&action=&entityType=&entityId=&occurredFromEpochDay=&occurredToEpochDay=&correlationId=`
- **返回**：`ImmutableAuditEvidence[]`，欄位 `{ evidenceId, occurredAt, actorId, role, authenticationSource, action, entityType, entityId, reason, correlationId }`。
- **可選 action 列舉**（`AuditAction`，34 項）：
  `CREATE_EMPLOYEE_ORDER, UPDATE_EMPLOYEE_ORDER, VERIFY_PICKUP_ORDER, UPSERT_VENDOR_MENU_ITEM, ADVANCE_VENDOR_FULFILLMENT_DELIVERY_STATUS, REGISTER_VENDOR_APPLICATION, SUBMIT_VENDOR_COMPLIANCE_DOCUMENT, REVIEW_VENDOR_APPLICATION, OPEN_PAYROLL_DISPUTE, UPSERT_ANOMALY_DETECTION_RULE, ...`

#### `ADM-AUDIT-02` 查責任歸屬（依執行者彙總）
- **API**：`GET /api/v1/admin/audit/responsibilities?...`（參數同 investigations）
- **返回**：每個 actor 的事件計數、他參與過的 action / entity 集合。

#### `ADM-AUDIT-03` 執行保留期清除
- **API**：`POST /api/v1/admin/audit/retention-purge`  
  Body：`{ asOfEpochDay }`
- **行為**：刪除指定日期前稽核證據（append-only table 例外路徑）。

---

### 5.6 EPIC ADM-ANALYTICS：營運分析

#### `ADM-ANA-01` 檢視跨商家 / 廠區 / 時間儀表板
- **API**：`GET /api/v1/admin/analytics/operations-dashboard?fromEpochDay=&toEpochDay=`
- **返回**：
  - `metricDefinitions[]`：key, displayName, unit, formula, source, version
  - `vendorBreakdown[]` / `plantBreakdown[]` / `timeBreakdown[]`
- **支援指標**：
  - `anomaly_triggered_total` / `anomaly_closed_total`
  - `payroll_settlement_records_total` / `payroll_disputed_records_total` / `payroll_deduction_failed_records_total` / `payroll_hr_sync_failed_total`

---

### 5.7 管理員端業務規則速查
- **雙重簽核（Issue Checklist）**：
  - 月結關帳必須有 `ISS-003`
  - 告警 CLOSE 必須有 `ISS-007`
- **必填意見**：審核 ≥ 5 字；爭議 RESOLVE_* ≥ 5 字。
- **Append-only**：`audit_event`、`payroll_ledger_entry`、`payroll_dispute_trace`、`anomaly_alert_trace`、`vendor_review_history`、`vendor_lifecycle_history`。
- **保留期**（`vendor_compliance.rs`）：
  - 商家審核歷程 2555 天
  - 商家生命週期歷程 2555 天
  - 被拒商家完整記錄 365 天後刪
  - 月結 / 爭議 7 年
  - 異常告警 2 年
- **優先級**：配送映射 `precedence` 0–65535，數字越小越優先。

---

## 6. 薪資操作員 Payroll Operator User Stories

此角色**沒有 UI**，僅透過 HTTP API 供 HR 系統整合。

#### `POPR-01` 匯出薪資扣款批次
- **API**：`POST /api/v1/integrations/payroll/deductions`
- **用途**：HR 系統輪詢已關帳的批次，抓出 SFTP-compatible 扣款明細。
- **後端**：`src/payroll.rs`。

#### `POPR-02` 觸發選擇性 HR API 伴隨同步
- **API**：`POST /api/v1/integrations/payroll/sftp-batches/{batchId}/hr-api-sync`
- **用途**：部分欄位（例如離職員工旗標）走 HR API 而不是 SFTP 時，用這端點重新派送。
- **失敗會觸發指標**：`payroll_hr_sync_failed_total`（ADM-ANA-01 可見）。

---

## 7. AI Agent / MCP 用戶 Stories

MCP server 位於 `/mcp/v1/*`；工具與 capability domain 定義於 `src/transport/mcp.rs`（或 `src/contract.rs`）+ OpenAPI L1429–1522。

#### `MCP-01` 列出可用資源
- **Endpoint**：`GET /mcp/v1/resources`

#### `MCP-02` 列出授權給此 OAuth service account 的工具
- **Endpoint**：`GET /mcp/v1/tools`

#### `MCP-03` 執行 MCP 工具
- **Endpoint**：`POST /mcp/v1/tools/{toolName}/invoke`
- **工具依 capability domain 分組**：

| Domain | 示例 tool | 風險等級 | 對應角色 |
|---|---|---|---|
| `ordering` | `ordering.create_employee_order` | HIGH_RISK_WRITE | Employee |
| `verification` | `verification.verify_pickup_qr` | WRITE | Employee |
| `compliance-review` | `compliance-review.review_vendor` | HIGH_RISK_WRITE | CommitteeAdmin |
| `settlement` | `settlement.export_deductions` | WRITE | PayrollOperator |
| `anomaly` | `anomaly.evaluate_rules` | WRITE | CommitteeAdmin |

- **契約保證**：MCP tool 與 HTTP API 共用同一 domain/service/authorization；錯誤模型、稽核路徑一致。

---

## 8. 狀態機與 Lifecycle 一覽

### 8.1 員工訂單 `OrderLifecycleState` / `EmployeeOrderStatus`
```
PENDING ─(修改)──▶ MODIFIED ─┬─▶ FULFILLED（領餐完成，終態）
   │                        │
   ├─(取消)──▶ CANCELLED ────┤（終態）
   ├─(商家售罄)──▶ SOLD_OUT ─▶ REFUND_PENDING ─▶ REFUNDED（終態）
```

### 8.2 商家配送 `FulfillmentDeliveryStatus`
```
PENDING_PREP → PREPARING → PACKED → OUT_FOR_DELIVERY → DELIVERED（終態）
        │          │         │            │
        └──────────┴─────────┴────────────┴──▶ CANCELLED（終態）
```

### 8.3 商家合規 `VendorComplianceStatus`
```
PendingReview ─▶ FixRequested ⇄ PendingReview
     │              │
     └────▶ Active ⇄ Suspended
     │
     └────▶ Rejected（365 天後刪除）
```

### 8.4 異常告警 `AnomalyAlertStatus`
```
OPEN → ACKNOWLEDGED → REMEDIATION_IN_PROGRESS → CLOSED（需 ISS-007 簽核）
   \_________________________________/
                    └─▶ ESCALATED → REMEDIATION_IN_PROGRESS / CLOSED
```

### 8.5 月結爭議 `PayrollDisputeStatus`
```
OPEN ─(ASSIGN_OWNER)─▶ IN_REVIEW / ASSIGNED ─┬─▶ RESOLVED_REFUND_APPROVED
                                             └─▶ RESOLVED_REJECTED
```

### 8.6 月結週期鎖定
```
UNLOCKED ⇄ LOCKED（lock/unlock 皆需 reason）
```

### 8.7 菜單項目 `VendorMenuItemStatus`
```
LISTED ⇄ PAUSED
   │
   └──▶ DELISTED（永久，可被新 menuItemId 取代）
```

---

## 9. 錯誤碼與例外處理對照表

| ErrorCode | HTTP | 典型訊息 | 常見觸發 |
|---|---|---|---|
| `INVALID_ORDER_REQUEST` | 422 | 「API 驗證規則未通過」 | 欄位缺失或格式錯 |
| `ORDER_POLICY_VIOLATION` | 409 | 「訂購規則不允許」 | 截單已過 / 售罄 |
| `ORDER_VENDOR_DELIVERY_REJECTED` | 409 | 「此商家當前時段不可配送到你的廠區」 | 無 mapping 或 `DENY` |
| `ORDER_MUTATION_NOT_ALLOWED` | 409 | 「此訂單狀態不可修改」 | 非 PENDING/MODIFIED |
| `ORDER_NOT_FOUND` | 404 | 「API 路徑不存在」 | 訂單 ID 無效 |
| `INVALID_MENU_DISCOVERY_QUERY` | 400 | 「API 請求格式錯誤」 | 日期格式或排序錯 |
| `MENU_DISCOVERY_INTERNAL_ERROR` | 500 | 「後端服務發生內部錯誤」 | 菜單搜尋內部錯 |
| `PICKUP_VERIFICATION_REPLAYED` | 409 | 「此驗證碼已使用過」 | TOTP 重放 |
| `PICKUP_VERIFICATION_STATE_CONFLICT` | 409 | 「訂單狀態不可核銷」 | 非 PENDING/MODIFIED |
| `PICKUP_VERIFICATION_EXPIRED` | 409 | 「驗證碼已過期」 | 超過 30s + drift |
| `PICKUP_VERIFICATION_INVALID_WINDOW` | 409 | 「時間超出核銷視窗」 | 非配送日 |
| `PICKUP_VERIFICATION_INVALID_CODE` | 409 | 「驗證碼錯誤」 | TOTP 不匹配 |
| `VENDOR_COMPLIANCE_INTERNAL_ERROR` | 500 | 「後端服務發生內部錯誤」 | lifecycle 執行異常 |
| `VENDOR_COMPLIANCE_PERSISTENCE_ERROR` | 500 | 同上 | DB 寫入失敗 |
| `PAYROLL_LEDGER_INTERNAL_ERROR` | 500 | 同上 | Ledger 寫入失敗 |
| `ANOMALY_ALERT_INTERNAL_ERROR` | 500 | 同上 | Alert 評估失敗 |
| `ANALYTICS_WAREHOUSE_INTERNAL_ERROR` | 500 | 同上 | Warehouse 查詢失敗 |
| `OBJECT_STORAGE_INVALID_ARTIFACT_CLASS` | 400 | 「類別不支援」 | 非允許 artifactClass |
| `OBJECT_STORAGE_INVALID_OBJECT_REF` | 400 | 「objectRef 格式無效」 | ref 不符規範 |
| `OBJECT_STORAGE_SIZE_EXCEEDED` | 422 | 「檔案大小超過限制」 | > 20MB / > 10MB |

> 前端本地化對照：`apps/web/src/lib/platform/api/failure.ts`、`apps/web/src/lib/i18n/zh-tw.ts` L116–137。

---

## 10. Epic × Story 索引

| 角色 | Epic | Story IDs | 重點 |
|---|---|---|---|
| **Employee** | `EMP-MENU` | 01–05 | 週/日曆檢視、看欄位、下單、備註 |
| Employee | `EMP-ORDER` | 01–07 | 列表、修改、取消、QR、刷新、核銷、timeline |
| Employee | `EMP-PAYROLL` | 01–04 | 明細、摘要、申訴、追蹤 |
| Employee | `EMP-NOTIFY` | 01 | 尖峰提醒（第二階段） |
| **Vendor** | `VEN-MENU` | 01–07 | 菜單 CRUD、複製、上下架、訂購視窗 |
| Vendor | `VEN-FULFILL` | 01–06 | 看板、推進狀態、批次、列印、營運訂單、分析 |
| Vendor | `VEN-DOCS` | 01–02 | 上傳計畫、下載連結 |
| Vendor | `VEN-COMPLIANCE` | 01–02 | 狀態與能力、補件 |
| **Admin** | `ADM-OV` | 01 | 總覽儀表板 |
| Admin | `ADM-VEN` | 01–06 | 列商家、審核、模板、lifecycle、映射、下載 |
| Admin | `ADM-SETTLE` | 01–06 | 關帳、例外、鎖/解鎖、爭議、保留期 |
| Admin | `ADM-ANOMALY` | 01–04 | 告警查詢、推進、評估、規則 |
| Admin | `ADM-AUDIT` | 01–03 | 稽核查詢、責任歸屬、保留期 |
| Admin | `ADM-ANALYTICS` | 01 | 營運指標 |
| **Payroll** | `POPR` | 01–02 | 匯出、HR 同步 |
| **MCP** | `MCP` | 01–03 | 資源、工具列表、調用 |

---

## 11. 後端 / 前端覆蓋差異（產品需補齊的缺口）

| 項目 | API 狀態 | 前端狀態 | 建議 |
|---|---|---|---|
| 菜單 `search / healthTag / menuType / price range / remainingQuantity` 篩選 | ✅ | ❌ | 在員工菜單區塊加篩選列 |
| 菜單排序 (`name / priceMinor / remainingQuantity`) | ✅ | ❌ | 加排序選單 |
| 訂單狀態篩選 (employee list) | ✅ | ❌ | 員工訂單頁加 status dropdown |
| 尖峰提醒偏好 | ✅ | ❌ | 加個人偏好頁 |
| 商家營運分析儀表板 | ✅ | ❌ | vendor 端加「營運分析」section |
| 商家合規狀態自我檢視 | ❌ admin-only | ❌ | 開 vendor-facing endpoint + 狀態 banner |
| 菜單搜尋推薦（`recommendationApplied` 旗標） | ✅ | ❌ | 加「為我推薦」UI（第二階段） |
| 備餐批次四層工件分別下載 | ✅ | 部分 | 針對 labels/basket_list 加下載按鈕 |
| 爭議完整 trace 時間線 | ✅ | 部分 | 管理端加展開式 timeline |
| 映射稽核歷程 | ✅ | ❌ 無獨立 UI | 加 mapping 變更歷史抽屜 |

---

> **文件維護**：本文件應與 `contract/openapi/openapi.yaml` 保持同步。當契約變更時，須：
> 1. 更新對應 Story 的 API 區塊；
> 2. 若狀態機有變，更新第 8 節；
> 3. 若新增 ErrorCode，更新第 9 節；
> 4. 回到第 10 節更新索引。
