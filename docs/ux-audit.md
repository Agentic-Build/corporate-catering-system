# 企業訂餐系統 — UX Audit 報告

> **目的**：對 49 個路由頁面逐一檢驗「component 存在合理性」「系統設計是否符合第一直覺」，並對照主流外送平台（UberEats / foodpanda / DoorDash / 美團 / 餓了麼 / Deliveroo / LINE MAN）與後台系統（Stripe Dashboard / Salesforce Lightning / PagerDuty / Opsgenie / Toast POS）的設計慣例，指出具體落差與可行動修法。
>
> **方法**：三位 agent 各自深度 audit 員工 / 商家 / 管理員三個 role 的**所有原始碼**（`.svelte` + `+page.ts` + helpers），輸出每頁的 component 盤點、✅/⚠️/❌ 合理性評級、第一直覺檢驗、外送平台 benchmark、3–5 條具體優化建議。本文件為彙整摘要，完整逐頁細節請見 appendix A/B/C 的 agent raw output。
>
> **Backend 說明**：Docker compose 拉取 redis / nats / minio 的 immutable digest 超過 20 分鐘無進展（本地網路問題），最終放棄重開後端。本 audit 以**源碼閱讀為主、既有截圖為輔**。源碼 audit 反而比截圖更能揭露工程 ID 穿透、狀態機 enum、API 參數裸露等問題 — 這些在截圖上不一定明顯。
>
> 相關文件：`docs/ux-redesign.md`（路由藍圖）、`docs/user-story-book.md`（行為契約）、`docs/user-journeys.md`（靜態截圖走訪）。

---

## 目錄

1. [執行摘要（Executive Summary）](#1-執行摘要executive-summary)
2. [9 個系統性問題（Systemic Findings）](#2-9-個系統性問題systemic-findings)
3. [逐頁 audit 總表（49 routes）](#3-逐頁-audit-總表49-routes)
4. [Top 10 優先修補清單](#4-top-10-優先修補清單)
5. [外送平台 & 後台 benchmark 對照](#5-外送平台--後台-benchmark-對照)
6. [Appendix A — 員工 audit raw](#6-appendix-a--員工-audit-raw)
7. [Appendix B — 商家 audit raw](#7-appendix-b--商家-audit-raw)
8. [Appendix C — 管理員 audit raw](#8-appendix-c--管理員-audit-raw)

---

## 1. 執行摘要（Executive Summary）

系統**骨架乾淨**（task-oriented 路由、完整 auth guard、軟退場、i18n 結構）— 前一輪重新設計成功達成「使用者不會在他角功能中迷路」。但**到了頁面內部**，大量問題集中在：

| 嚴重度 | 問題類別 | 命中角色 |
|---|---|---|
| 🔴 Critical | 工程 ID / enum / epoch 裸露（menuItemId、objectRef、epochDay、minuteOfDay、kind、status） | 全 3 個角色 |
| 🔴 Critical | 「需要按套用」反即時 UX（多處 filter 都要 click submit） | 員工 + 商家 |
| 🔴 Critical | 月結關帳 UI = API body editor（把 pageSize/sortBy/issueChecklist CSV 丟給業務人） | 管理員 |
| 🔴 Critical | 商家「合規文件上傳」實際上是填 mimeType/sizeBytes 表單（非開發者無法完成） | 商家 |
| 🟠 High | 看板用 table 不用 Kanban（商家每日主要工作流） | 商家 |
| 🟠 High | 缺購物車模型（員工只能單品單筆下單） | 員工 |
| 🟠 High | 「Inbox」名稱但實作是 KPI dashboard | 管理員 |
| 🟠 High | localStorage 當成資料源（settlement cycles / disputes list） | 管理員 |
| 🟡 Medium | Master-detail 結構空洞（`/employee/wallet` 右欄永遠是 placeholder） | 員工 |
| 🟡 Medium | 審核 / 爭議詳情 context-less（進入頁看不到在審什麼） | 管理員 |
| 🟡 Medium | 沒有 bulk action / saved view / CSV export | 管理員 |

**整體評分（依角色）**：

| 角色 | 評分 | 一句話 |
|---|---|---|
| 員工 (mobile-first) | **⚠️ 6.5 / 10** | 骨架對，但 ID 洩漏 + 篩選摩擦 + 缺購物車讓下單流程每步都疊一點摩擦 |
| 商家 (desktop-first) | **⚠️ 5.5 / 10** | 最該用桌機 canvas 的角色，實際卻多在堆兩欄卡片；看板 + 上傳流程的可用性受損最大 |
| 管理員 (desktop-first) | **⚠️ 6.0 / 10** | 後端語意完整，但 UI 層像是 API schema 直接翻表單；簽核、月結、爭議三大關鍵流程與 Stripe 有兩代差距 |

**最該先動的 3 件事**（見 §4 詳細）：

1. **月結關帳改成 4-step wizard**（preview → 例外 → 簽核 → 執行）— 這是會實際處理大額金錢的 UI，目前長得像 POST body editor。
2. **商家文件 / 圖片上傳改成 file dropzone**（目前要人工填 mimeType、拿 URL 後自己 curl）— 影響每一次新菜單、每一次補件。
3. **所有 ID / enum / epoch day / minute-of-day 統一 mapping layer** — 影響幾乎每一頁的 scannability。

---

## 2. 9 個系統性問題（Systemic Findings）

跨 3 個角色共同出現、需要系統性修補的問題：

### 2.1 🔴 工程識別符穿透到 UI

**症狀**：`menuItemId`、`orderId`、`vendorId`、`disputeId`、`objectRef`、`alertId`、`ruleId` 等 UUID / 内部字串大量以 `font-semibold text-slate-900` 作為主欄位。員工打開訂單詳情，首欄是 `menu-mo5i5z0mdnnmxdjz`，不是菜名。

**對照外送平台**：UberEats / DoorDash 訂單詳情永遠是「餐點縮圖 + 菜名 + 數量」，絕不顯示 orderId。Stripe Dashboard 把 ID 放在小一級灰字 + hover 可複製。

**修法**：
- 後端 response 加 `displayName` / `humanLabel` 等欄位；若需 join 則 BFF 做。
- 前端建 `friendlyXxx()` mapping layer（已有 `friendlyOrderStatus` pattern），擴展到：
  - `friendlyEventType()` for timeline
  - `friendlyLedgerKind()` for wallet 流水
  - `friendlyMenuType()` for `BENTO`/`BOWL` → 便當/丼飯
  - `friendlyHealthTag()` for `VEGAN` → 純素
- ID 統一降級為 `text-xs text-slate-500`，或做 `<IdTag>` component hover 可複製。

**影響範圍**：21 個頁面（`/employee/**`、`/vendor/menu/**`、`/admin/vendors/**`、`/admin/anomalies/**`）。

---

### 2.2 🔴 「需要按套用」反即時 UX

**症狀**：`/employee/discover`、`/employee/orders`、`/vendor/menu`、`/vendor/today`、`/admin/vendors`、`/admin/anomalies` 的篩選區都是「改欄位 → 按套用 → 才 refetch」。

**對照外送平台**：Deliveroo / UberEats / DoorDash 幾乎所有篩選都是 `onchange` 即刷，頂部會出現 loading bar。Stripe Dashboard 同。

**修法**：把 `套用` 按鈕砍掉，filter 改 `$effect` auto-refresh；對 expensive query 加 `debounce(300ms)`。保留 `CSV export` 類的按鈕。

**影響範圍**：至少 8 個頁面。

---

### 2.3 🔴 金額 / 時間 / 日期裸露內部表示

**症狀**：
- `12000`（TWD minor unit）裸露在菜單建立表單、月結結果 card；已有 `<MoneyAmount>` primitive 卻沒統一用。
- `minuteOfDay = 1080` 裸露在商家訂購政策、管理員 anomaly filter（商家要心算 1080÷60=18:00）。
- `epochDay = 19831` 裸露在 admin audit / anomaly 查詢、analytics 篩選（管理員要查「上週某 vendor 改動」要自己算 epoch day）。

**對照**：Stripe / Chargebee / Toast 全用 `$12.00` / `18:00` / `2025-04-19` 顯示，內部才是 epoch + minor。

**修法**：
- 金額一律走 `<MoneyAmount>`，表單輸入用「元」+ auto-format，送出時乘 100。
- 時間改 `<input type="time" step="900">` 15 分鐘刻度，內部轉 minute-of-day。
- 日期改 `<input type="date">`，UI 轉 epoch day。
- Helper 位置：`apps/web/src/lib/platform/time.ts`（新建）。

**影響範圍**：約 12 個頁面。

---

### 2.4 🔴 關鍵破壞性動作 = 表單 dump

**症狀**：
- `/admin/settlement/close` 把 `cycleKey / issueChecklist CSV / page / pageSize / sortBy / sortOrder` 全部扁平塞在一張 Card 裡，關鍵的 ISS-003 簽核變成 text input。
- `/admin/settlement/disputes/[disputeId]` 的 refund 操作直接給 `refundAmountMinor` 輸入框，沒有「你正在退款 NT$200 給員工 E-12345」的再次確認。
- `/admin/anomalies/[alertId]` CLOSE 要填 `closureEvidenceRefs` CSV。

**對照 Stripe dispute**：submit evidence 是 side-drawer with evidence checklist + 大 submit button + ConfirmDialog re-echo 金額。**簽核一律是 checkbox 或二段式認證**，絕非 CSV text input。

**修法**：
- 破壞性動作全改 **4-step wizard**（preview → validate → sign → execute）。
- ISS-xxx 簽核改 `<label><input type="checkbox"/> 我已取得 ISS-007 簽核</label>` + 顯示簽核人姓名 / 時間。
- `closureEvidenceRefs` 改 chip input，每個 ref 一個 chip。
- 刪除「pageSize / sortBy / sortOrder」欄位 — 這些是結果顯示參數，應該在結果頁才出現。

**影響範圍**：3 個關鍵頁面但影響整體可信度。

---

### 2.5 🔴 缺完整 file upload 閉環

**症狀**：商家新增菜單要附圖 → 必須跳到 `/vendor/compliance/upload` → 手填 mimeType / sizeBytes → 拿到 `uploadUrl` → **自己 curl PUT 檔案** → 回菜單表單貼上 objectRef。跨 4 頁、中間有一步是 CLI。

**對照**：UberEats Menu Manager / DoorDash Merchant 全部是 **dropzone + auto PUT + 進度條**；0 個平台要求使用者手填 mimeType。

**修法**：
- 把 `createVendorObjectStorageUploadPlan` + `fetch(uploadUrl, PUT)` 包成 `<FileDropzone artifactClass="MENU_IMAGE" bind:objectRef={...}>` 元件。
- 在菜單表單、合規文件上傳、批次 artifact 下載三處直接使用。
- `/vendor/compliance/access-links` 直接從 navigation 移除 — 下載連結由「批次詳情」「合規文件」等入口自動產生。

**影響範圍**：3 個頁面 + 新增菜單流程。

---

### 2.6 🟠 Kanban 缺席、table 當看板

**症狀**：商家 `/vendor/today` 備餐看板是**訂單 DataTable + inline `<select>` 推進狀態 + submit 按鈕**（每單兩步）。本應是 kanban。

**對照 Toast KDS / Deliveroo Hub / UberEats Merchant Order tab**：kanban 6 欄（PENDING_PREP / PREPARING / PACKED / OUT_FOR_DELIVERY / DELIVERED / CANCELLED），訂單以卡片呈現，單點或拖拉推進。

**修法**：
- Primary 視圖 → 改 kanban（用 `FulfillmentDeliveryStatus` 6 個狀態當欄位）。
- Table 保留作為「列表模式」tab，給需要 export 的 ops。
- `nextDeliveryStatus(status)` helper 已經能算下一狀態，改成「卡片右下一顆 `→` 按鈕直接推進」。

**影響範圍**：1 個頁面但是商家每日主要工作流。

---

### 2.7 🟠 缺購物車模型（員工）

**症狀**：`/employee/discover` 每張菜單卡片有「立即下單」，**一鍵直接建單**（會扣薪）。`/employee/orders/[orderId]/edit` 只能改數量不能增加新品項。

**對照 UberEats / foodpanda / DoorDash**：全部是「加入購物車」→ 底部浮動 CTA 顯示「下 N 筆訂單（合計 $X）」→ Checkout → Confirm。

**修法**：
- 菜單頁「立即下單」改「加入當日訂單」。
- 底部浮動 cart summary；點擊進 confirm dialog。
- `orders/[orderId]/edit` 加「新增品項」（跳回 discover 以 append 模式）+「刪除品項」（quantity=0 時真的從 lineItems 移除）。

**影響範圍**：`/employee/discover`、`/employee/orders/[orderId]/edit`。

---

### 2.8 🟠 localStorage 冒充資料源

**症狀**：`/admin/settlement/cycles`、`/admin/settlement/disputes`、`/admin/overview` 「最近關帳摘要」都從 `readRecentSettlements()` 讀 browser 儲存。換瀏覽器即失憶，合規後台不該如此。

**修法**：
- 後端補 `listPayrollSettlementCycles` / `listPayrollDisputes` endpoint。
- 前端 localStorage 保留為 secondary 預覽或 draft，不作權威來源。
- 頁面 description 刪掉「此清單為本地紀錄」自我安慰文案。

**影響範圍**：3 個管理員頁面 + backend 新增 2 個 endpoint。

---

### 2.9 🟡 缺 bulk action / saved view / export

**症狀**：24 個管理員頁面**無一頁**支援多選 bulk action、`我的 / 待處理 / 全部` saved view、CSV export。

**對照 Stripe / Salesforce / Retool**：這三件事是 back-office 後台的**最基本三件套**。例如 Stripe dispute list 可 bulk "Accept" 多筆、Salesforce Lightning 所有 list 都有 saved filter、Retool 全部有 CSV 匯出。

**修法**：
- 建 `<DataTable>` 支援 `selectable` prop + bulk action toolbar。
- 建 `<SavedViewSelector>`：URL query string 儲存 filter 組合 + localStorage 記住常用 view。
- 所有列表頁右上加 `Export CSV` 按鈕（client-side CSV from current filtered data）。

**影響範圍**：管理員 24 頁大部分 + UI primitives 擴充。

---

## 3. 逐頁 audit 總表（49 routes）

**評級定義**：
- ✅ 合理、第一直覺 OK
- ⚠️ 結構對但細節有明顯改善空間
- ❌ 核心功能失守或違反業界慣例

### 3.1 員工（10 routes）

| 路由 | 目的 | 評級 | 頭號問題 |
|---|---|---|---|
| `/employee` | 今日 Home | ⚠️ | `orderId` 當主標、`帳務摘要` Card 名實不符 |
| `/employee/discover` | 菜單 + 下單 | ❌ | 一鍵直接下單（無購物車）、filter 需按套用、數量是 number input |
| `/employee/orders` | 訂單列表 | ⚠️ | `orderId` 第一欄、狀態 dropdown 用 enum（C 端不該暴露） |
| `/employee/orders/[orderId]` | 訂單詳情 | ⚠️ | 品項欄顯示 menuItemId 不是菜名（最嚴重）、timeline event 用英文 enum |
| `/employee/orders/[orderId]/edit` | 修改訂單 | ⚠️ | 只能改數量、不能加 / 刪品項、無 diff preview |
| `/employee/orders/[orderId]/cancel` | 取消訂單 | ✅ | 缺「預退金額」預告 + reason radio 代替自由文字 |
| `/employee/orders/[orderId]/pickup` | 領餐 QR | ⚠️ | verificationCode 字體太小、缺 wake lock / 亮度最大化、refresh 按鈕權重錯 |
| `/employee/orders/[orderId]/dispute` | 提交申訴 | ⚠️ | 缺「申訴的是哪筆」上下文卡、缺附件上傳、缺 SLA 預期 |
| `/employee/wallet` | 扣款總覽 | ❌ | 「已載入訂單」假指標、右欄永遠是 placeholder、缺月份 picker |
| `/employee/wallet/[orderId]` | 扣款明細 | ⚠️ | kind / sourceEventKind 英文 enum、正負金額同色、缺推導流程（原金額 − 退款 = 淨扣） |

### 3.2 商家（15 routes）

| 路由 | 目的 | 評級 | 頭號問題 |
|---|---|---|---|
| `/vendor` | 今日儀表板 | ⚠️ | 3 張 KPI 只有純數字、截單顯示 `1080` 分鐘不轉時間、缺 task queue |
| `/vendor/today` | 今日看板 | ❌ | table 當看板，應 kanban、狀態推進兩段式、audit trail 預設開啟佔版面 |
| `/vendor/today/[plantId]` | 指定廠區 | ⚠️ | 和 `/vendor/today` 差別看不出、廠區彙總仍顯示（冗餘） |
| `/vendor/menu` | 菜單列表 | ⚠️ | 無 thumbnail 圖、menuItemId 第一欄、date 當主 filter 反直覺 |
| `/vendor/menu/new` | 新增菜單 | ❌ | menuItemId 預填要改、金額填 minor、圖片要跨頁、欄位 12 個扁平 |
| `/vendor/menu/[menuItemId]` | 編輯菜單 | ⚠️ | list + find hack 撈不到 90 天外菜單、缺「今日訂購 N 份」實況 |
| `/vendor/schedule` | 訂購政策 | ❌ | 900–1200 分鐘輸入、無 live preview、無時區提示 |
| `/vendor/batches` | 批次列表 | ❌ | 假列表（只有 localStorage）、empty state 誤導 |
| `/vendor/batches/new` | 建立批次 | ⚠️ | 無 preview 說「將產生 N 個標籤 / N 張分區表」 |
| `/vendor/batches/[batchId]` | 批次詳情 | ⚠️ | artifacts 無 download button、print 印 metadata 表非 artifact |
| `/vendor/orders` | 營運訂單 | ⚠️ | 4 欄太少（缺金額 / 品項 / 特殊需求）、plantId 需手輸 |
| `/vendor/compliance` | 合規狀態 | ✅ | 誠實 UX 做得好，缺 mailto 連結、缺最後上傳時間 |
| `/vendor/compliance/upload` | 建立上傳計畫 | ❌ | 手填 mimeType / sizeBytes、拿 URL 後要 curl、epoch 時間 |
| `/vendor/compliance/access-links` | 建立下載連結 | ❌ | objectRef 要手輸、孤立、應整合進其他入口 |
| `/vendor/insights` | 營運分析 | ❌ | epochDay 輸入、無任何圖表、metric schema 當主內容 |

### 3.3 管理員（24 routes）

| 路由 | 目的 | 評級 | 頭號問題 |
|---|---|---|---|
| `/admin` | Inbox | ⚠️ | 名為 Inbox 實為 KPI dashboard、無 task queue、KPI 無 trend |
| `/admin/vendors` | 商家列表 | ⚠️ | status 用 dropdown 不是 tab、vendorId 第一欄、無 bulk action |
| `/admin/vendors/[vendorId]` | 商家詳情 | ⚠️ | meta 8 欄扁平、review action 在獨立頁（失 context）|
| `/admin/vendors/[vendorId]/review` | 審核決策 | ⚠️ | 獨立頁喪失上下文、decision 是 raw enum、comment ≥5 字太寬鬆 |
| `/admin/vendors/[vendorId]/mappings` | 廠區映射 | ⚠️ | `precedence 65535`、`ALLOW/DENY` 概念、單次 datetime 非 recurring |
| `/admin/compliance/templates` | 模板列表 | ✅ | 缺「影響 N 家」callout、reminder 欄位單位不明 |
| `/admin/compliance/templates/new` | 新增模板 | ⚠️ | 11 欄扁平無分組、reminderCsv 是 CSV |
| `/admin/compliance/templates/[id]` | 編輯模板 | ⚠️ | composite key 脆弱、缺 delete / version history |
| `/admin/compliance/lifecycle` | 執行 lifecycle | ✅ | dry run 設計好，缺影響預估、缺排程、缺歷程 |
| `/admin/settlement` | 月結 Hub | ⚠️ | 3 張 action card 無流程指引、摘要來自 localStorage |
| `/admin/settlement/close` | 執行關帳 | ❌ | 關鍵破壞性動作是 API body editor（整個系統最差頁） |
| `/admin/settlement/cycles` | 週期列表 | ❌ | 只讀 localStorage、缺 HR 對帳狀態欄 |
| `/admin/settlement/cycles/[cycleKey]` | 週期詳情 | ⚠️ | lock / unlock 同視覺權重危險、reason 無 template |
| `/admin/settlement/disputes` | 爭議列表 | ❌ | 只讀 localStorage、無 tab / 金額 / SLA 排序 |
| `/admin/settlement/disputes/[disputeId]` | 爭議處理 | ❌ | context-less 做決策、refund 輸入 minor、Operation 用 tab bar |
| `/admin/anomalies` | 告警列表 | ❌ | 7 欄 filter epochDay / minuteOfDay、無 SLA countdown、無 saved view |
| `/admin/anomalies/[alertId]` | 告警詳情 | ⚠️ | 12 欄無主次、transition trace 用 table、CLOSE evidence 是 CSV |
| `/admin/anomalies/evaluate` | 手動評估 | ⚠️ | 本是 QA 工具不該在主 nav、epochDay 欄位 |
| `/admin/anomalies/rules` | 規則列表 | ✅ | 缺「最近觸發次數」「目前 open alerts」欄位 |
| `/admin/anomalies/rules/new` | 新增規則 | ⚠️ | 11 欄無分組、governanceIssueId 手輸、缺 live sentence preview |
| `/admin/anomalies/rules/[ruleId]` | 編輯規則 | ⚠️ | 缺 delete / duplicate、無 threshold 歷史 |
| `/admin/audit` | 稽核查詢 | ⚠️ | epochDay 輸入、無 facet sidebar / saved query / CSV export |
| `/admin/audit/responsibilities` | 責任歸屬 | ⚠️ | actions 集合用 comma-join、無 drilldown |
| `/admin/analytics` | 營運分析 | ❌ | epochDay 輸入、零圖表、全 raw table |

**統計**：49 頁中 **12 頁 ❌（24%）、27 頁 ⚠️（55%）、10 頁 ✅（21%）**。結構無大問題，細節待打磨。

---

## 4. Top 10 優先修補清單

按「影響頻次 × 修補難度 × 戰略價值」排序：

| # | 修補 | 影響頁面 | 類別 | 預估工作量 |
|---|---|---|---|---|
| 1 | **月結關帳改 4-step wizard**（preview → 例外 → 簽核 → 執行） | 1 | 🔴 Critical | 2–3 days |
| 2 | **FileDropzone 元件 + 整合**到菜單 / 合規 / 批次下載 | 4 | 🔴 Critical | 2 days |
| 3 | **統一 `friendlyXxx()` mapping layer**（ID、enum、timeline event） | ~20 | 🔴 Critical | 2 days |
| 4 | **時間 / 金額 / 日期輸入全改人類可讀**（`<input type="time">` / 元 / date picker） | ~12 | 🔴 Critical | 1 day |
| 5 | **商家看板 table → kanban** | 1 | 🟠 High | 3 days |
| 6 | **員工加購物車模型** | 2 | 🟠 High | 2–3 days |
| 7 | **Filter 去除「套用」按鈕改 live refetch** | ~8 | 🟠 High | 0.5 day |
| 8 | **Admin Inbox 改真 task queue** | 1 | 🟠 High | 2 days |
| 9 | **後端補 list API**（settlement cycles / disputes）+ 前端切換 | 3 | 🟠 High | 2 days (backend + frontend) |
| 10 | **DataTable 加 selectable + bulk action**、CSV export、saved view | ~15 | 🟡 Medium | 3 days |

**合計：預估約 20–25 人天**可顯著提升整體 UX 水準。

---

## 5. 外送平台 & 後台 benchmark 對照

這張表整理每個情境對應的標竿設計。

### 5.1 員工（消費者 app 慣例）

| 情境 | 系統現狀 | 對照 | 建議對齊 |
|---|---|---|---|
| Home 首屏 | KPI + Cards | UberEats: Reorder carousel + 推薦 | Hero 橫幅顯示「今日待領取」大字 + reorder tiles |
| 菜單瀏覽 | 週 / 日曆切換 + 篩選 | foodpanda / DoorDash: live filter + chip 快捷 | 移除套用、加「今天 / 明天 / 本週」chip |
| 下單 | 一鍵下單 | UberEats: `+` → bottom sheet → 購物車 | 加購物車模型 + 底部 CTA |
| 數量輸入 | `<input type="number">` | 美團 / 餓了麼 / foodpanda: stepper | `- [1] +` stepper |
| 領餐碼 | QR + 小字 code | 美團 / 7-11: 大字 4-位碼 / 全螢幕 | verificationCode 放大、wake lock、亮度最大 |
| 訂單 timeline | `ORDER_CREATED` 英文 | foodpanda: 已建立 → 製作中 → 外送中 + icon | friendlyEventType 中文化 + icon |
| Wallet | 4 stat card + master-detail 空右欄 | UberEats wallet / 銀行 app：月份 picker + 趨勢圖 | 移除假指標、加月份切換、單欄列表 |

### 5.2 商家（B2B 後台慣例）

| 情境 | 系統現狀 | 對照 | 建議對齊 |
|---|---|---|---|
| 今日儀表板 | KPI + 截單卡 | UberEats Manager: trend + top issues queue | 加 task queue、sparkline、可點擊進入 |
| 備餐看板 | Table + `<select>` 推進 | Toast KDS / Deliveroo Hub: kanban column + 拖拉 | Kanban 6 欄 + 單鍵下一狀態 |
| 菜單列表 | 無 thumbnail、按日期 | UberEats Menu Manager / 美團: thumbnail + 分類 tab | 加縮圖、status toggle inline |
| 菜單新增 | 12 欄扁平 + minor 金額 | Square / UberEats item editor: section + `$12.00` | 分 section、元單位、圖片 dropzone |
| 訂購政策 | 分鐘數字輸入 | Toast / foodpanda: `<input type="time">` + preview | 時間 picker + live preview |
| 合規文件 | 手填 mimeType / 拿 URL curl | UberEats / DoorDash: dropzone + auto PUT | FileDropzone 元件 |
| 營運分析 | 零圖表 + epochDay | UberEats Analytics: trend chart + heat map | 加 chart + preset date range |

### 5.3 管理員（後台 / 治理系統慣例）

| 情境 | 系統現狀 | 對照 | 建議對齊 |
|---|---|---|---|
| Inbox | KPI + 摘要 | Stripe: Today + Needs review 可處理 queue | 真 inbox：可 inline Acknowledge / Assign / Snooze |
| 商家審核 | 獨立頁 context-less | Stripe dispute: side-drawer with evidence | inline drawer + 必填文件清單 |
| 月結關帳 | API body editor | Stripe Billing / Chargebee: 4-step wizard + preview | wizard + ISS 簽核 checkbox |
| 月結爭議處理 | tab + form，無 context | Stripe dispute detail: 左 evidence 右 submit | context-first + MoneyAmount |
| 異常告警 | Filter 7 欄 epochDay | PagerDuty / Opsgenie: SLA countdown + saved view | date picker + 倒數 chip |
| 稽核查詢 | Filter + table | Salesforce / Honeycomb / Loki: facet sidebar + saved search | facet UI + CSV export + correlation drill |
| 規則定義 | 11 欄 flat | Grafana / Datadog: wizard + live sentence | 分 section + preview |
| 分析 | Raw table | Stripe Sigma / UberEats Analytics: KPI + trend chart | 加 chart + preset |

---

## 6. Appendix A — 員工 audit raw

**完整員工端 audit**（10 個路由的 component 盤點、評級表格、benchmark、3–5 條具體優化建議）：
→ **[`docs/ux-audit/employee.md`](./ux-audit/employee.md)**

核心結論摘要（不可不讀）：

- **技術 ID 穿透**是員工端最嚴重問題：`orderId / menuItemId / disputeId / ownerActorId / eventType / kind / sourceEventKind` 全部以主欄位顯示。
- **EMP-02 discover 頁**：「立即下單」不經 confirm 直接扣薪 + 數量用 number input + 「套用」按鈕是三個疊加摩擦。
- **EMP-07 pickup 頁** 是整個員工端最接近成熟外送平台的頁面，但 verificationCode 字體太小、缺 wake lock、兩顆按鈕主次錯。
- **EMP-09 wallet 頁**：右欄永遠是 placeholder（master-detail 結構失效），「已載入訂單」是假指標。
- **EMP-08 dispute 頁** 缺「申訴的是哪筆」上下文卡片，員工填時看不到金額 / 品項。

---

## 7. Appendix B — 商家 audit raw

**完整商家端 audit**（15 路由 detail）：
→ **[`docs/ux-audit/vendor.md`](./ux-audit/vendor.md)**

核心結論摘要：

- **視覺密度**：全商家端**沒有真正善用桌機 canvas**。side-nav 256px + content 1000px，多數頁用 2-col grid 擺兩張 card 就結束。**「桌機上塞手機版」**的批評成立。
- **Kanban 缺席**：今日看板核心備餐動作完全 table，沒有任何 Toast KDS / Deliveroo Hub 風格的 column board。
- **工程 ID 洩漏**：menuItemId、objectRef、epochDay、minuteOfDay、sizeBytes、sha256 全裸露。多半是把 API contract 直接 wrap 成表單。
- **Action 斷裂**：菜單建圖片需跨到 compliance upload；批次詳情下載需跨到 access-links；合規上傳拿到 URL 還要自己 curl。每個跨頁都是未收尾 flow。
- **最該先修**：`/vendor/today` 改 Kanban、`/vendor/compliance/upload` 改 dropzone、`/vendor/menu/new` 移除 menuItemId 輸入並整合圖片上傳。

---

## 8. Appendix C — 管理員 audit raw

**完整管理員端 audit**（24 路由 detail）：
→ **[`docs/ux-audit/admin.md`](./ux-audit/admin.md)**

核心結論摘要：

- **「Inbox」是名義不是實作**：`/admin` 是 KPI hub 而非 task queue。真正符合「今天該處理什麼」的 inbox 應該是一條條可 inline Acknowledge / Assign / Snooze 的 queue（Stripe / Opsgenie 都長這樣）。
- **簽核（ISS-003、ISS-007）UX 比 Stripe dispute 差兩代**：目前是 text input 打 `ISS-003` 字串；應該是 checkbox + 顯示簽核單位 / 人 / 時間。
- **月結流程 vs Stripe / Chargebee**：本系統 close page 把關帳和 pageSize / sortBy 混在一起，複雜度 3 倍於需要。
- **Audit 查詢無 pivot / facet / saved view**：Salesforce / Honeycomb / Grafana Loki 都有 facet sidebar + saved search + export + correlation drilldown。本系統只有 filter form + table。
- **缺 bulk actions / quick filter / saved views 系統性問題**：24 頁中無一頁有 multi-select、saved view、CSV export。後台最基本三件套全缺。
- **localStorage 當資料源**：settlement/cycles、settlement/disputes、analytics「最近關帳摘要」都從 `readRecentSettlements()` 讀 browser。合規後台不應如此。
- **正面值得保留**：ConfirmDialog（destructive 標配）、primitives（MoneyAmount / StateTag / PageHeader）、append-only trace 語意、dry run 模式（compliance/lifecycle）。

---

## 結語

**本系統的骨架 — 路由設計、角色分離、軟退場、authz guard — 都是對的**。前一輪重新設計已經成功解決「使用者不會被他角干擾」。

**但走進頁面內部，距離「第一直覺可用」還有一段明顯距離**：
- 消費者端不夠像外送 app（少購物車、少 stepper、少 thumbnail、多 ID）
- 商家後台不夠像 UberEats Manager / Toast POS（少 kanban、少 dropzone、多 epoch 輸入）
- 管理員治理不夠像 Stripe Dashboard（少 task queue、少 facet search、少 wizard、多 CSV）

按照 §4 的 Top 10 清單修完，整體 UX 分數可以從目前平均 **6.0 / 10** 提升到 **8.0+ / 10**。預估 20–25 人天工作量。

建議優先從 **#1 月結 wizard** 與 **#2 FileDropzone** 開始 — 前者是**錢流信任**、後者是**功能可用性**，是最該立即降低風險的兩件事。
