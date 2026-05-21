# 設計:員工 App(Tauri)＋ Web 既有頁面補功能

日期:2026-05-20
分支:`feat/employee-app-and-web-features`
設計參考:`~/Downloads/T-Bite_app.zip`(`T-Bite App.html` React mockup)

本文件涵蓋兩塊一起規劃的工作:

- **A — Web 既有頁面補功能**:員工 web 與商家 web 的增量功能。
- **B — 員工 App**:用 Tauri 2 製作 iOS/Android 手機 App,1:1 參考 `T-Bite App.html`。

兩者共用同一個 Go 後端與同一組 `@tbite/*` 套件;部分後端/契約變更為雙邊共用,優先實作。

---

## 架構與 repo 佈局

新增一個 app,**不動現有三個 web app 的架構**:

```
apps/
  employee/        現有 SSR web(SvelteKit + adapter-node)— 本次僅「加功能」
  merchant/        現有 SSR web — 本次僅「加功能」
  admin/           不動
  employee-app/    【新】Tauri SPA — 員工手機 App
    src/           SvelteKit + adapter-static(ssr=false、SPA 模式)
    src-tauri/     Tauri 2 設定,iOS + Android 目標
packages/
  ui / api-client / tokens   App 與 web 共用;契約改動後重新產生 api-client
services/api/      Go 後端 — 本次為「加端點 / 加欄位」的增量變更
contract/openapi/  加欄位與端點後 `make contract-sync` 重新產生 api-client
```

**決策依據**

- 後端 middleware(`identity/http/middleware.go`)已同時接受 `Authorization: Bearer` 與 session cookie → 原生 client 可用 token 認證,不需 SSR、不需 Node server。
- `@tbite/api-client`(openapi-fetch)本就能在前端執行;現有 `+page.server.ts` 全為單純 API GET、form actions 全為 API POST。
- `employee-app` 為純前端 SPA,與 `apps/employee`(SSR web)並存,共用同一後端。
- 沿用現有 SSR web 包 Tauri 不划算:mockup 的資訊架構(底部 tab、店家優先瀏覽)與現有 web 差異大,且 Tauri 包 SSR 只能 bundle Node sidecar 或 webview 指向遠端,皆有缺陷。

---

# Part A — Web 既有頁面補功能

## A1. 評分與客訴整合進員工薪資頁

**現況**:評分已存在於 `apps/employee/src/routes/orders/[id]`(對 `picked_up` 訂單的 5 星評分＋留言);客訴已有 `/complaints` 頁與訂單頁回報表單。本項把兩者比照 App 整合進薪資頁,**重用既有後端端點**。

- **前端**:`apps/employee/src/routes/payroll/+page.svelte` 的「本月進行中」逐筆訂單列(見 A2)每列可點 → 開新元件 `PayrollEntrySheet.svelte`(比照 App `EntryDetailSheet`),雙模式 ⭐評分 / 📣客訴。
- 評分與客訴表單邏輯由 `orders/[id]/+page.svelte` 抽出為共用片段,sheet 直接打既有 rate / complaint API。
- **後端 / 契約**:無新增,重用既有 rate 與 complaint 端點。
- **測試**:sheet 送出評分/客訴後狀態更新;已評分訂單不可重複評分。

## A2. 薪資累加(逐筆明細＋即時累加)

**現況**:`/api/employee/payroll` 僅回月結批次;web 薪資頁僅顯示批次表格＋「累計淨扣款」。

- **前端**:薪資頁批次表格上方新增「本月進行中」區塊 — 累加 hero(本月即時累計扣款)＋逐筆訂單列(店家、餐點摘要、日期、金額、狀態徽章:已扣/已沖銷/未領)。`+page.server.ts` 加載 B2 新端點。
- **後端 / 契約**:見 B2。
- **測試**:本月累計金額正確;沖銷列不計入正額累加。

## A3. 代扣自動沖銷

**目標**:訂單於收費後取消、或客訴 resolved 並判定補償時,對應代扣金額自動沖銷(退款)。

- **後端**:見 B3(掛 `payroll/settler`)。
- **前端**:逐筆訂單列 `reversed` 狀態以負額/刪節線呈現;批次表「退款」欄已存在,無需改動。
- **測試**:見 B3。

## A4. 員工 web 首頁「依店家 / 依餐點」切換

**現況**:首頁為扁平 `MealCard` grid(「全部餐點 · N 項」)。

- **前端**:`apps/employee/src/routes/+page.svelte` 新增元件 `MenuViewToggle.svelte`(依餐點 / 依店家)。「依店家」將 `filteredMenu` 以 `vendor_id` 於 client 端分組,渲染店家分區(店家標頭＋該店 MealCard)。**不需新路由**。選擇存 `?view=` 與 localStorage。
- **後端 / 契約**:無。
- **測試**:切換後分組正確;搜尋/分類篩選在兩種檢視下一致。

## A5. 商家編輯餐點 — 圖片欄位(檔案上傳)

**現況**:`MerchantItemDTO` / `EmployeeMenuItemDTO` 讀取面已有 `images`,但寫入面 `CreateItemInputBody` / `UpdateItemInputBody` 無此欄位;無圖片上傳端點。

- **後端 / 契約**:見 B1。
- **前端**:`apps/merchant/src/routes/menus/[id]/+page.svelte` 與 `menus/new/+page.svelte` 表單新增「餐點圖片」區 — 檔案輸入 → 上傳 `POST /api/merchant/uploads` → 取回 URL 加入 `images`;縮圖預覽、可刪除與排序。新元件 `ImageUploader.svelte`。送出時 `images` 隨 `?/update` / create action 帶入。
- **測試**:上傳後縮圖顯示;儲存後員工端 `MealCard` 顯示圖片;非圖片型別/超大檔被拒。

---

# Part B — 後端與契約基礎(雙邊共用,優先實作)

## B1. 餐點圖片上傳

- **契約**:`CreateItemInputBody` / `UpdateItemInputBody` 加 `images: string[]`(nullable array)。新增 `POST /api/merchant/uploads`(multipart),回 `{ url }`。
- **後端**:handler 置於 `menu/http`,經 `platform/storage/s3.go` 寫入 MinIO/GCS(已有基礎設施:`ops/kubernetes/overlays/single-node/minio.yaml`、`gcs-binding.yaml`)。驗證 content-type(jpeg/png/webp)與大小上限(建議 2MB);物件 key 含 vendor 範圍。
- `menu.Service` 的 create/update 接受並持久化 `images`。
- **測試**:上傳回合法 URL;型別/大小驗證;`images` 寫入與讀回一致。

## B2. 員工薪資逐筆明細端點

- **契約**:`GET /api/employee/payroll/current`,回本期逐筆訂單行:
  `{ order_id, supply_date, vendor_name, items_summary, amount_minor, status, rated, complaint_id }`。
  `status` ∈ `charged | reversed | no_show`。
- **後端**:`payroll` 模組新增查詢 — 取目前未結批次期間內、該員工的訂單彙總。
- 此端點同時供 Web 薪資頁(A1/A2)與 App PayrollScreen 使用。
- **測試**:僅回本期;沖銷與未領狀態正確;`rated` / `complaint_id` 對應正確。

## B3. 代扣自動沖銷 ⚠️ 最高風險

- **後端**:沖銷邏輯掛 `payroll/settler`。觸發事件:
  1. 訂單於收費後取消 / 退款。
  2. 客訴 `resolved` 且判定補償。
  settler 寫一筆沖銷(負額)歸入該期,逐筆行 `status` 轉 `reversed`,批次 `refunded_minor` 反映。
- **planning 階段須先深挖**:`payroll/settler` 目前如何消費 order / complaint 事件、是否有事件總線或定時結算。沖銷的冪等性與重放安全需確認。
- **測試**:收費後取消產生等額沖銷;客訴補償產生沖銷;同事件重放不重複沖銷;沖銷後淨額正確。

## B4. 行動版 OIDC 回呼

- **現況**:OIDC 回呼導向 SvelteKit landing 並帶 `?token=`,landing 寫入 HttpOnly cookie。
- **後端**:`identity` 回呼流程加一個 allowlist 過的 redirect 目標 — 當 `client=app` 時導向自訂 scheme deep link `tbite://auth?token=…`(或經一個偵測行動裝置後 302 至 deep link 的極簡 landing 頁)。
- **測試**:`client=app` 回呼導向 deep link 並帶 token;未在 allowlist 的 redirect 目標被拒。

## B5. 契約重新產生

- B1/B2/B4 契約改動後執行 `make contract-sync`,重新產生 `@tbite/api-client`;App 與 web 皆取用新型別。

---

# Part B' — 員工 App(Tauri)

## 技術堆疊

- SvelteKit ＋ `@sveltejs/adapter-static`(`ssr=false`、SPA fallback)。
- `src-tauri/`:Tauri 2,目標 iOS + Android。
- 重用 `@tbite/tokens` 的 Tailwind preset 與 Noto Sans TC,色票與 web 一致。
- 重用 `@tbite/ui` 基礎元件(Button / Icon / StateTag);行動專用元件(底部導覽、底部抽屜、店家卡、餐點列)為新建。

## 畫面(1:1 對 `T-Bite App.html`)

全為新 Svelte 元件(mockup 原為 React):

- 底部 5 tab:首頁 / 訂單 / 領餐碼 / 薪資 / 個人。
- **HomeScreen**:地點列、搜尋、7 天日期條、篩選區(關鍵字/價格區間/排序/供應狀態/標籤)、分類 pill、店家卡列表。
- **VendorDetail**:封面、店家資訊卡、篩選列、`FilterSheet`(底部抽屜)、餐點列＋數量加減、購物車吸底列。
- **CartSheet**:底部抽屜、品項、備註、送出預訂 → 成功頁。
- **OrdersScreen**:預訂中 / 歷史 tab、訂單卡、出示領餐碼。
- **TotpScreen**:QR ＋ 動態碼 ＋ 30s 倒數 ＋ 步驟說明。
- **PayrollScreen**:本月已扣款 hero ＋ 逐筆明細 → `EntryDetailSheet`(評分/客訴)。
- **ProfileScreen**、**FavoritesScreen**、**NotifModal**。

## 資料層

- `@tbite/api-client` 於 client 端執行,帶 Bearer token。
- 各畫面 `onMount` fetch 既有 `/api/employee/*` 端點;薪資打 B2 新端點。
- 購物車:client 端 Svelte store,沿用 `apps/employee/src/lib/cart.svelte.ts` 模式。
- 即時菜單:`EventSource`(`/menu/events`)在 webview 內可用。

## Auth(Tauri mobile)

- `tauri-plugin-deep-link` 註冊 `tbite://` scheme。
- 登入:以系統瀏覽器開 `/auth/{provider}/start?client=app` → OIDC → 後端(B4)導回 `tbite://auth?token=…`。
- App 攔截 deep link 取 token,存入 `tauri-plugin-stronghold` / 平台 keychain(**非** localStorage)。
- 啟動時讀 token → 驗 `/me` → 進首頁或登入頁。登出清除 token 並打 `/auth/logout`。

## 範圍外

- 原生推播(FCM/APNs)不做;`NotifModal` 僅為 App 內通知清單。

---

# 建置順序與里程碑

| 里程碑 | 內容 | 驗證 |
| --- | --- | --- |
| **M1** | 後端與契約基礎(B1–B5) | contract 測試、端點測試、`make contract-sync` |
| **M2** | Web 變更(A1–A5 前端) | `svelte-check`、既有 e2e |
| **M3** | App 骨架＋auth:scaffold `employee-app` SPA＋Tauri 2、deep-link 登入、secure storage | iOS 模擬器/Android 模擬器可登入並開首頁 |
| **M4** | App 全畫面 1:1:其餘所有畫面、購物車、抽屜、TOTP、薪資、個人 | 各畫面對齊 mockup |
| **M5** | 打包與簽署:iOS/Android 建置設定、icon/splash、簽署、CI | 產出可安裝套件 |

**相依**:M1 解鎖 M2 與 M3/M4(App 需要 `payroll/current` 與行動 auth)。M2 與 M3 在 M1 之後可部分並行。

**環境需求**:Rust toolchain、Xcode(iOS)、Android SDK/NDK＋JDK。

# 風險

- **B3 代扣沖銷**:最高風險,需先理解 `payroll/settler` 的事件消費與結算時序,確保沖銷冪等。
- **Tauri mobile CI**:iOS/Android 建置在 CI 上較複雜(需模擬器/簽署),M5 須評估是否納入 CI 或僅本機/手動建置。
- **行動 OIDC**:deep link 回呼在 iOS/Android 的註冊與測試需實機驗證。
