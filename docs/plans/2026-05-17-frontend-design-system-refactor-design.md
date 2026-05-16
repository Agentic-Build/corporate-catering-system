# T-Bite 前端設計系統重構 — 設計文件

- **日期**：2026-05-17
- **狀態**：待核准
- **作者**：Claude（與 @TakalaWang 協作）

---

## 1. 背景與目標

`~/Downloads/T-Bite Design System/` 是從 T-Bite 參考原型抽出的設計系統，包含品牌語彙、色彩、字體、元件，以及三個產品介面的完整 reference 實作（`reference_src/`、`ui_kits/tbite/`）。

三個 SvelteKit 前端（`apps/employee`、`apps/merchant`、`apps/admin`）目前已使用 `@tbite/ui` 與 `@tbite/tokens`，色彩 token 大致到位，但**版面骨架與互動元件與設計系統有明顯落差**。

**目標**：重構三個前端，使其與設計系統一致——各 app 主畫面精準重現 reference，其餘路由套用設計語言。

**「移除角色切換」的定義**：reference 是單一 SPA、頂部有一條 52px 的角色切換列。三個前端早已是各自獨立、各自登入的 app（程式碼中無任何 `role` 切換邏輯）。因此「移除角色切換」= 拆解 reference 時**不移植那條角色切換列**，把各角色的設計分別套到對應 app。連帶結果：sticky header 維持 `top-0`（不需 `top-[52px]` 偏移）。

---

## 2. 範圍與非範圍

### 範圍
- 三個 app 的 `+layout.svelte` 與主畫面（`/`）精準重現 reference。
- 三個 app 其餘所有路由套用設計語言（eyebrow＋大標題、卡片、StatCard、表格樣式、StateTag 等）。
- 四個新互動元件：購物車抽屜、領餐碼 QR modal、分類篩選列、商家餐點庫抽屜——全部串接真實後端，無 placeholder。
- 擴充 `@tbite/ui` 共用元件庫。
- 把設計系統的真實食物攝影 assets 納入 repo。

### 非範圍
- 不新增後端沒有對應功能的畫面（例：員工「通知中心」「設定」「薪資代扣」在後端無對應 API → 不做，側欄也不放這些連到空頁的項目）。
- 不重寫 Go 後端核心邏輯；僅補一個小型唯讀統計欄位（見 §9）。
- 不調整認證流程、不動 `@tbite/web-auth`。

---

## 3. 已確認決策

| # | 決策 | 選擇 |
|---|---|---|
| 1 | 改造範圍 | 主畫面精準重現＋其餘路由套設計語言 |
| 2 | 員工導覽版面 | 採用 reference 的 240px 左側側欄 |
| 3 | 分類篩選列資料來源 | 資料驅動：由當日菜單實際出現的 `tags` 動態產生 chip，client 端篩選 |
| 4 | 商家頁面結構 | 商家 `/` 合併成完整 MerchantView；`/supply` 併入並轉址 |
| 5 | 領餐碼 modal 範圍 | 全域「領餐碼」鈕 → modal；同時保留 `/orders/[id]/pickup` 路由 |

**員工側欄項目**（對映真實路由，移除無後端的項目）：
今日首頁 `/` · 我的訂單 `/orders` · 領餐碼（開 modal）· 我的常點 `/menu/favorites` · 申訴 `/disputes`。
reference 的「薪資代扣／通知中心／設定」**不放入側欄**（無對應後端，放了就是 placeholder）。Pro Tip 卡保留於側欄底部。

---

## 4. 架構總覽

### 4.1 後端現況查核（四個新元件）

兩次後端探索的結論——後端比預期完整，**這基本上是前端工程＋極少量後端微調**：

| 元件 | 後端現況 | 後端工作 |
|---|---|---|
| 購物車抽屜 | 購物車本就是純前端狀態；下單為一次性 `POST /api/employee/orders` | 無 |
| 領餐碼 QR modal | 完整支援：`GET /api/employee/orders/{id}/pickup-code`（6 位 TOTP、30s 過期、HMAC-SHA256 已實作） | 無 |
| 分類篩選列 | `menu_item.tags TEXT[]` 已存在、menu API 已回傳 `tags` | 無（client 端篩選）；需確認 `day_menu` 有帶 `tags`（見 §9） |
| 商家餐點庫抽屜 | 完整支援：`menu_item` 跨日持久（draft/active/archived）＋ `publish`/`archive`/`PUT supply` 端點 | 小：補 `last_used` / `total_sold` 唯讀統計（見 §9） |

### 4.2 技術轉換

reference 為 React/JSX；目標為 Svelte 5（runes：`$props`/`$state`/`$derived`/`$effect`）。所有 reference 元件需逐一轉成 Svelte。`@tbite/ui` 已有部分 Svelte 對應元件，將擴充補齊。

### 4.3 資料流（維持現有模式）

`Go API → +page.server.ts（createApiClient + locals.apiToken）→ Svelte page → form actions POST 回 +page.server.ts`。新元件沿用此模式，不引入 client 端直連 API。

---

## 5. 共用層：`@tbite/ui` 擴充

`@tbite/ui` 現有：`Button`、`Card`、`LocationBar`、`MealCard`、`ProviderButton`、`StateTag`、`StatCard`、`TBiteLogo`。

### 5.1 新增基礎元件（三 app 共用）

| 元件 | 來源 | 用途 |
|---|---|---|
| `Icon`（或 `icons.ts`） | `ui.jsx` `I.*` | 24px stroke-only SVG set：Cart, QR, Plus, Minus, Chevron, Filter, Search, Close, Download, Check, Alert, Doc, Heart, Home, Bell, Tag, Wallet, Card, Cog, Pin |
| `Modal` | `ui.jsx` `Modal` | scrim `bg-slate-900/40 backdrop-blur-sm`、`rounded-2xl`、`fade-up`、Esc/點外關閉、focus-trap |
| `Drawer` | `EmployeeView`/`MerchantView` 抽屜外殼 | 右側滑入面板，`translate-x-full`→`0`、scrim、`max-w-*` 可調 |
| `Toggle` | `ui.jsx` `Toggle` | 紅色開關，role="switch"、鍵盤可操作 |
| `PageHeader` | `EmployeePages.jsx` `PageHeader` | eyebrow（紅色 uppercase tracked）＋ `text-3xl font-black` 標題＋副標＋actions |
| `Tabs` | `EmployeePages.jsx` OrdersPage | 底線式分頁，含計數 pill |
| `SearchInput` | `EmployeeView` 搜尋框 | pill 圓角、左側 Search icon、focus ring |
| `RemainBar` | `ui.jsx` `RemainBar` | 剩餘份數進度條（emerald/amber/rose 分級） |
| `EmptyState` | 多處 | dashed border 空狀態卡（icon＋文字） |

### 5.2 動畫與全域樣式

`colors_and_type.css` 中的 `fadeUp`、`cartBump`、`.no-scrollbar`、`.placeholder-stripes*` 需移入 `@tbite/tokens/tokens.css`（或各 app `app.css`），目前 token 檔僅有色彩/字體變數。`animate-pulse` 用 Tailwind 內建。

### 5.3 既有元件校準

逐一比對現有 Svelte 元件與 `ui.jsx`／`components.jsx`，使其 class 與 reference 完全一致（特別是 `Button` 的 focus ring、`Card` 的 tone variant、`MealCard` 的 hover lift 與低庫存 badge）。

---

## 6. 員工 app（`apps/employee`）

### 6.1 版面（`+layout.svelte`）

reference `EmployeeView` 的兩層結構，**移除角色切換列**：

- **Sticky header**（`top-0`）：`TBiteLogo` ＋ `LocationBar`（廠區・取餐日）＋ `SearchInput` ＋「領餐碼」鈕（開 modal）＋ 購物車鈕（badge 計數、`cartBump` 動畫）＋ 使用者頭像（紅→玫瑰漸層、姓名首字）。手機版 LocationBar 移至 header 下方第二列。
- **主體**：`max-w-[1400px]` 容器内 `flex gap-6` — 左側 `Sidebar`（240px、`sticky top-[100px]`、`hidden lg:block`）＋ 右側 `main`。
- **Sidebar**：5 個導覽項（§3）＋分隔線＋Pro Tip 卡（`from-amber-50 to-rose-50` 漸層）。current route 高亮 `bg-red-50 text-red-700`。「我的訂單」可顯示進行中訂單數 badge。

### 6.2 首頁（`/`）— 精準重現

對映 reference `EmployeeView` 的 `home` 區塊，**用真實後端資料**：

| reference 區塊 | 重現方式 | 資料來源 |
|---|---|---|
| 日期 eyebrow ＋「哈囉，{name} 👋」＋截單倒數 | `PageHeader` 變體 | `data.user`、`home.target_day` |
| 分類篩選列 `TbCategoryStrip` | `CategoryStrip` 元件，chip 由當日菜單 distinct `tags` 動態產生（圓形漸層 swatch＋glyph） | `day_menu[].tags` |
| 精選橫向列 `TbFeaturedRow`（橫向 scroll 的 300px `MealCard`） | 三條 row：**再點一次／推薦你今天／我的最愛**——把現有 reorder/recommend/favorite 資料改用 `MealCard` 橫向列呈現（取代現有小尺寸 chip 元件） | `home.reorder_chips`、`recommend_chips`、`favorite_chips` |
| 全部餐點格 | `MealCard` 響應式 grid，套用分類篩選 | `home.day_menu` |
| 浮動購物車列 | 保留，重現 reference 的深色 pill（紅色圖示徽章＋金額＋「查看購物車 →」） | client cart state |
| 購物車抽屜 `TbCartDrawer` | **新增**：右側滑入，列出購物車品項（縮圖＋qty stepper）、小計、`送出預訂 · 由本月薪資代扣` | client cart →`?/placeOrder` action |
| 領餐碼 modal `TbTotpModal` | **新增**：見 §6.4 | `pickup-code` API |

現有 `ChipCarousel`/`ReorderChip`/`FavoriteChip`/`RecommendChip` 由 `FeaturedRow`＋`MealCard` 取代後移除。下單／加入最愛／reorder 的 form actions 與 toast 邏輯保留。

### 6.3 購物車抽屜

- 觸發：點 header 購物車鈕。
- 內容：reference `TbCartDrawer`——逐項縮圖、商家、單價、`+`/`−` stepper、移除；footer 小計＋合計＋送出鈕。
- 後端：**無新增**。抽屜操作 client cart state；「送出預訂」沿用首頁既有 `?/placeOrder` form action（隱藏欄位帶 `item_id`/`qty`）。
- 浮動購物車列與抽屜共用同一份 cart state。

### 6.4 領餐碼 QR modal

- 觸發：header／側欄「領餐碼」鈕（全域）。
- 行為：
  - 開啟時向 server 取「今日 `status=ready` 的訂單」清單。
  - 0 筆 → 空狀態（「目前沒有可領取的訂單」）。
  - 1 筆 → 直接顯示該訂單領餐碼。
  - 多筆 → 訂單選擇器，選後顯示。
- 內容：reference `TbTotpModal`——QR 圖（由真實 code 生成，非裝飾性亂數）、6 位動態碼、`{n}s 後更新` 倒數、amber 提示列。
- 資料：`GET /api/employee/orders/{id}/pickup-code` 回傳 `{code, expires_in_seconds}`；前端依 `expires_in_seconds` 倒數，歸零後重新 fetch。
- 路由並存：`/orders/[id]/pickup` 保留，改為套設計語言的全頁版本（內部複用同一 `TotpView` 元件）。

### 6.5 其餘路由（套設計語言）

`/orders`（`Tabs`＋reference `OrderCard` 樣式）、`/orders/[id]`、`/orders/[id]/dispute`、`/disputes`、`/menu/favorites`（reference `FavoritesPage` 樣式：No.1 hero 卡＋常點列）、`/menu/recommendations`、`/menu/reorders`、`/login`（`ProviderButton`）。全部換用 `PageHeader`、`Card`、`StateTag`、`MealCard` 與設計系統樣式。資料來源與 form actions 不變。

---

## 7. 商家 app（`apps/merchant`）

### 7.1 版面（`+layout.svelte`）

reference `MerchantView` header：`TBiteLogo` ＋「商家後台 · {vendor 名稱}」pill ＋ 右側通知/設定鈕＋頭像。背景 `bg-slate-50`。無側欄（單欄儀表板）。

### 7.2 首頁（`/`）— 精準重現＋合併 `/supply`

商家 `/` 變成完整 `MerchantView`，由上而下：

1. **今日 eyebrow ＋「今日備餐儀表板」大標題**。
2. **StatCard 列**（4 格）：今日份數、準時率、待備餐、營收等——由 `GET /api/merchant/supply`（今日）＋ `GET /api/merchant/orders`（今日）彙整。
3. **廠區彙總卡 `TbPlantAggCard`**：今日訂單依 plant 分組，每卡顯示總份數、品項 ×數量、「下載配送標籤」。資料：`GET /api/merchant/orders?date=today` 依 plant groupBy。
4. **7 天排程規劃器**（合併原 `/supply`）：
   - `ScheduleDayPicker`：Mon→Sun 日選，每日顯示道菜數、已訂/上限、狀態 pip（備餐中／今 17:00 截單／尚未排菜／接受預訂中）。
   - `ScheduleTable`：選定日的菜色表格——品項縮圖、售價、上限 stepper、`OrderProgress` 已訂/上限條、`Toggle` 上架、移除。今日列唯讀。
   - 編輯透過 `PUT /api/merchant/supply/{itemID}/{date}`；上架/下架透過 publish/archive 端點。

### 7.3 餐點庫抽屜

- 觸發：`ScheduleTable` 的「從餐點庫加入」鈕。
- 內容：reference `MealLibraryDrawer`——搜尋框、菜色卡列（縮圖、描述、價格、kcal、tags、上次上架、累計售出）、「加入此日」鈕（已排入者顯示已加入）。
- 資料：`GET /api/merchant/menu-items`（含 archived）為餐點庫；「加入此日」= `PUT /api/merchant/supply/{itemID}/{date}`（若 archived 先 `publish`）。
- `last_used` / `total_sold` 由 §9 後端新欄位提供。

### 7.4 其餘路由（套設計語言）

`/orders`（備餐看板，依 plant 分組、bulk markReady、verifyPickup TOTP 輸入）、`/menus`、`/menus/[id]`、`/menus/new`、`/onboard`、`/login`。`/supply` → 301 轉址至 `/`（功能已併入排程規劃器）。

---

## 8. 福委會 app（`apps/admin`）

### 8.1 版面（`+layout.svelte`）

reference `AdminView` header：`TBiteLogo` ＋「福委會後台 · 管理員」pill ＋ 右側「稽核紀錄」「新增邀請」鈕＋深色頭像。背景 `bg-slate-50`。單欄。

### 8.2 首頁（`/`）— 精準重現

商家 `/` 變成完整 `AdminView`，全部用真實資料（目前 `+page.server.ts` 僅載 vendors，需擴充）：

1. **eyebrow「合規 · 治理 · 對帳」＋「福委會後台」大標題**＋摘要副標。
2. **StatCard 列**（4 格）：待審商家、準時率、近 7 日告警、本月對帳金額。
3. **商家入駐審核**：`Card` 包 `TbApprovalCard` 列——商家名、`StateTag` 狀態、文件 chip、「通知補件」「核准」。資料：`GET /api/admin/vendors?status=pending`；動作接 `approve` 等端點。
4. **異常治理告警 `TbAlertList`**：`GET /api/admin/anomalies`，依 severity 著色的告警列。
5. **本月薪資代扣預覽**：`Card` 包表格，資料取最近一筆 `GET /api/admin/payroll/batches` ＋其 entries。

`+page.server.ts` 擴充為平行載入 vendors＋anomalies＋payroll batches（端點皆已存在，純前端 server-load 工作）。

### 8.3 其餘路由（套設計語言）

`/vendors`、`/vendors/[id]`、`/vendors/[id]/documents`、`/payroll`、`/payroll/[id]`、`/payroll/[id]/disputes`、`/payroll/new`、`/anomalies`、`/audit`、`/dlq`、`/login`。全部換用 `PageHeader`、`Card`、表格樣式、`StateTag`、`StatCard`。

---

## 9. 後端微調

唯一的後端工作，範圍刻意最小：

1. **餐點庫統計欄位**（商家餐點庫抽屜需要）：在 `GET /api/merchant/menu-items` 回應中為每個 item 增加：
   - `last_used`：該 item 最近一筆 `meal_supply.supply_date`。
   - `total_sold`：該 item 累計 `picked_up` 訂單的 `order_item.qty` 總和。
   - 兩者皆為唯讀彙整查詢，不需 migration、不改既有欄位。需同步更新 `contract/openapi/openapi.yaml` 與 `packages/api-client` 的 schema。
   - 若實作成本偏高，退路：UI 隱藏這兩個顯示欄位（仍非 placeholder，只是不顯示）。

2. **確認 `day_menu` 帶 `tags`**（分類篩選列需要）：檢查 `GET /api/employee/home` 的 `day_menu` 項目是否含 `tags`。
   - 有 → 無需後端工作。
   - 無 → 於 `home_handler.go` 的 DTO 補上 `tags`（資料已在 `menu_item.tags`，僅 SELECT/映射），同步更新 contract 與 api-client。

無新增 table、無新增 migration、無 cart table、TOTP 沿用既有實作。

---

## 10. 資源（assets）

設計系統 `assets/` 提供 34 張真實食物攝影（10 logos、10 store covers、10 item shots、4 category banners）。

- 把 `assets/` 複製進各 app 的 `static/brand/`（或建一個共用 `packages/assets`）。
- 商家 logo、菜色照在後端 `menu_item_image.blob_uri` 缺圖時，以這些 asset 作 fallback；種子資料可指向這些圖。
- 嚴守設計規範：**不使用 SVG 插畫或 AI 生成食物圖**；icon 用 §5.1 的 stroke SVG set。

---

## 11. 建置順序

分階段、每階段可獨立驗證：

| 階段 | 內容 | 驗證 |
|---|---|---|
| 0 | `@tbite/ui` 擴充（§5）：icons、Modal、Drawer、Toggle、PageHeader、Tabs、SearchInput、RemainBar、EmptyState；動畫進 tokens；assets 納入 | `pnpm --filter @tbite/ui check` 通過；Storybook/預覽頁目視 |
| 1 | 員工 app：layout（側欄＋header）→ 首頁重現（分類列、精選列、餐點格）→ 購物車抽屜 → 領餐碼 modal → 其餘路由 | `svelte-check` 通過；員工流程目視；下單/領餐碼端到端 |
| 2 | 商家 app：layout → `/` MerchantView（儀表板＋廠區彙總＋排程規劃器）→ 餐點庫抽屜 → `/supply` 轉址 → 其餘路由 | `svelte-check`；排菜/上限/上架端到端 |
| 3 | 福委會 app：layout → `/` AdminView → `+page.server.ts` 擴充 → 其餘路由 | `svelte-check`；審核/告警/對帳目視 |
| 4 | 後端微調（§9）：餐點庫統計、`day_menu` tags 確認；contract＋api-client 同步 | Go 測試；`openapi` 生成；型別檢查 |
| 5 | 全域驗證 | 三 app `build`、lint、型別檢查、`prettier`；對照 reference 目視 |

階段 0 為其餘階段的前置；1/2/3 彼此獨立，可平行。階段 4 在 §9 確認 `day_menu` 缺 `tags` 時須早於階段 1 對應部分。

---

## 12. 驗證

- 每個 app：`pnpm --filter <app> check`（svelte-check）＋ `build` 通過。
- `@tbite/ui`、`@tbite/api-client`：`check` 通過。
- 後端：`go build ./...` ＋既有測試通過；OpenAPI 重新生成無漂移。
- 目視：每個主畫面與 reference（`ui_kits/tbite/index.html`）並排比對。
- 端到端煙霧測試：員工下單→領餐碼；商家排菜→上限→上架；福委會審核商家。
- 全程 `prettier` 格式一致。

---

## 13. 風險與未決

- **React→Svelte 轉換量大**：reference 約 5,400 行 JSX。逐元件轉換，先共用層（階段 0）降低重複。
- **`day_menu` 是否帶 `tags`**：階段 0/1 之間須先確認（§9.2），影響分類篩選列。
- **餐點庫 `last_used`/`total_sold`**：唯讀彙整查詢，若 N+1 風險高則改 JOIN 聚合；退路為 UI 不顯示。
- **領餐碼多訂單**：reference 為單碼設計，真實情境員工當日可能多筆 ready 訂單 → 加訂單選擇器（§6.4），屬對 reference 的合理擴充。
- **`/supply` 轉址**：若有外部書籤或測試指向 `/supply`，需一併更新。

---

## 附錄：reference 檔案對照

| reference | 目標 |
|---|---|
| `ui_kits/tbite/EmployeeView.jsx` | `apps/employee` layout＋首頁 |
| `ui_kits/tbite/EmployeePages.jsx` | 員工其餘路由樣式（OrdersPage/FavoritesPage 等） |
| `ui_kits/tbite/MerchantView.jsx` | `apps/merchant` layout＋`/` |
| `ui_kits/tbite/AdminView.jsx` | `apps/admin` layout＋`/` |
| `reference_src/ui.jsx`、`ui_kits/tbite/components.jsx` | `@tbite/ui` 基礎元件 |
| `colors_and_type.css` | `@tbite/tokens` 動畫／全域樣式 |
| `assets/` | `apps/*/static/brand/` |
