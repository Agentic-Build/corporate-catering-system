# 設計：移除原生 App、三個前端改 RWD 對齊手機 mockup

日期：2026-05-24
分支：`feat/rwd-mobile-apps`（worktree：`.worktrees/rwd-mobile`，基於 `feat/pickup-qr-sticker` 含 merge 後 HEAD `e86a2d2`）

## 目標

1. 移除 Tauri 原生 App `apps/employee-app` 及其專屬的後端/設定殘留。
2. 三個 web 前端（`apps/employee`、`apps/merchant`、`apps/admin`）改為 RWD：
   **桌面維持現狀不動**，新增手機斷點，視覺對齊設計稿
   `~/Downloads/T-Bite_app/{T-Bite App,T-Bite Merchant,T-Bite Admin}.html`。

## 已確認的決策（來自與使用者的釐清）

- **範圍**：employee + merchant + admin 三個都做。
- **RWD 做法**：桌面維持現狀；只加手機斷點對齊 mockup。
- **員工手機 IA**：**不換資訊架構**——保留 web 既有 IA（featured rows + 餐點 grid），
  只做響應式適配（加底部導覽、把卡片/篩選/購物車重排成 mockup 視覺風格）。
  **不**移植 employee-app 的「店家列表→店家詳情」流程。
- **員工底部導覽**：5 格＝首頁 / 訂單 / 掃描領餐 / 薪資 / **我的（新增彙整頁）**。
- **表格手機化（merchant/admin）**：手機改成堆疊卡片對齊 mockup；
  桌面（`md:` 以上）維持原表格。含表單/勾選的複雜表格（orders 批次、dlq、對帳 7 欄）以
  `overflow-x-auto` 兜底，不強制卡片化。
- **merge**：已由使用者完成（commit `e86a2d2`），本工作在 worktree 進行，不碰主工作區。

## 關鍵前提

- 三個 app 的視覺 token（色彩、圓角、陰影、字級）**早已對齊 mockup**
  （`@tbite/tokens` 的 `tb-*` 鏡射 mockup 用的 Tailwind 預設色）。
  → 這不是「重做視覺風格」，**不需新增任何顏色/圓角 token**。
- 真正的兩大工作：**(1) 手機底部導覽**、**(2) 寬表格→手機卡片**。
- `apps/employee-app` 是 adapter-static + sample 假資料的 UI 原型；
  web app 是 SSR + 真實 server-load。故**不整套移植**，只借用其
  `BottomNav` 結構、bottom-sheet、`slide-up`/`fade-in`/`.no-scroll`/斜紋 placeholder CSS 作樣式參考。

## 斷點策略（各 app 維持自己的桌面斷點，確保桌面零變更）

- **employee**：手機 = `< lg`（既有 Sidebar 是 `hidden lg:block`）。
  底部導覽 `lg:hidden`，避免 768–1024 區間既無 Sidebar 又無導覽。
- **merchant / admin**：手機 = `< md`（既有頂部 nav 常駐）。
  底部導覽 `md:hidden`，頂部 nav 包 `hidden md:block`。

## 各 app 設計

### A. employee（`apps/employee`）

桌面（≥lg）：sticky header + 240px Sidebar + 主內容，**完全不動**。

手機（<lg）改動：
- **新增 `BottomNav.svelte`**（`lg:hidden`，`fixed bottom-0`）：
  首頁`/` · 訂單`/orders` · 掃描`/scan` · 薪資`/payroll` · 我的`/me`。
  白底 + `border-t` + safe-area，active 紅色，沿用既有 Icon。
- **新增 `/me`「我的」彙整頁**：常點 `/menu/favorites`、客訴 `/complaints`、
  申訴 `/disputes`、登出入口；對應 mockup 的 Profile tab。
- `+layout.svelte` 主體手機加底部留白；`FloatingCartBar` 手機上移避讓底部導覽
  （`bottom-[72px] lg:bottom-5`）。
- header 手機精簡：搜尋移到 header 下方整列（桌面維持 `ml-auto hidden md:block`）。
- 首頁元件視覺微調（卡片圓角/間距已一致，主要是手機單欄與 strip 樣式），IA 不變。
- 購物車手機沿用既有 `CartDrawer`（右側滑入、手機接近全螢幕，可接受），不強制改 bottom sheet。

### B. merchant（`apps/merchant`）

桌面（≥md）：頂部 nav + 表格版面，**不動**（頂部 `<nav>` 加 `hidden md:block`）。

手機（<md）改動：
- **新增底部導覽**（`md:hidden`，兩排對齊 mockup 4+3）：
  儀表板 · 看板 · 菜單 · 客訴 ／ 對帳 · 合規 · 設定。
  `/labels`（餐點貼紙）不入底部導覽，從相關頁進入。`<main>` 加 `pb-24 md:pb-6`。
- **表格→卡片**（手機 `md:hidden` 卡片版 + 桌面 `hidden md:block` 原表格，由易到難）：
  `/menus` → `/compliance` → `/reconciliation` →（最後）`/orders`（保留勾選批次/SSE）。
  `ScheduleTable.svelte` 手機卡片版（最複雜，獨立處理，保留 stepper/缺貨/移除的樂觀更新）。
- chip 列手機改 `overflow-x-auto`（`.no-scrollbar` 已有）。
- **資料流/server actions 一律不動**，只換 DOM 呈現；兩套 markup 注意 form/checkbox name 不重複造成重複提交。

### C. admin（`apps/admin`）

桌面（≥md）：頂部 nav + 表格，**不動**。手機**不鎖 430px**，用流式單欄（後台較實用）。

手機（<md）改動：
- **新增底部導覽**（`md:hidden`，兩排 4+3）：治理總覽 · 商家 · 薪資 · 結算 ／ 客訴 · 告警 · 稽核。`/dlq` 不入底部導覽。
- header 手機隱藏右側按鈕群（`hidden md:flex`），保留 logo + 頭像；**登出改放「我的/頭像」可達**（避免手機無登出入口）。
- **表格防溢出兜底**：全部 `<table>` 補 `overflow-x-auto`（先確保不破版）。
- **卡片化**（對齊 mockup）：`/vendors` 商家列表先做（最單純、mockup 有對應），作為其餘頁範本；
  其餘高價值表格（dashboard 預覽、payroll、vendor 操作員）視情況跟進。
  含 inline 表單的 `/dlq`、`/vendor-settlements` 以橫捲兜底。
- filter pill 手機 `overflow-x-auto`；表單主按鈕手機 `w-full`。

## 共用套件（`@tbite/ui` / `@tbite/tokens`）

- `BottomNav` 為各 app 私有元件（route 不同），不放共用。
- tokens 視需要補 mockup 用的 `slide-up` keyframe（若採 bottom-sheet）與 safe-area 工具，
  但**不新增顏色/圓角**。
- **避免改動共用元件**（`PageHeader`、`Drawer`、`Icon`、`Card`、`StatCard`）的桌面行為；
  若一定要改（如 `Drawer` 加 bottom 變體、`PageHeader` H1 響應式字級），需確認對其他 app 無回歸。

## 移除 employee-app 的範圍

1. `git rm -r apps/employee-app`（worktree 內仍存在，作樣式參考後刪除）。
   `pnpm-workspace.yaml` 用 `apps/*` glob，自動移除，免改。
2. 後端 Go 清理（employee-app 專屬的原生 OAuth deep-link）：
   - `services/api/internal/identity/service.go`（移除 `"employee-app"` 分支與判斷）
   - `services/api/internal/identity/http/handlers.go`（`app` enum 去掉 `employee-app`、移除 `out.App=="employee-app"` 的原生轉址）
   - `services/api/internal/identity/http/handlers_test.go`（對應測試）
   - 重新產生 `contract/openapi/*` 與 `packages/api-client/src/schema.d.ts`
   - 跑 Go 測試確認綠燈
3. 歷史設計文件（`docs/plans/2026-05-20-*`、`2026-05-23-*`）**保留**，不改寫歷史。

## 驗證策略

- 每個 app：`pnpm --filter @tbite/<app> check`（svelte-check）綠燈；`build` 成功。
- 手機/桌面雙斷點目視（Playwright 截圖 375px 與 ≥1280px），確認**桌面零變更**、手機對齊 mockup。
- merchant `/orders` 批次標記、SSE 即時更新在手機卡片版仍正常（無重複提交）。
- Go：`go test ./services/api/internal/identity/...` 綠燈、openapi/schema 重生成後 api-client 編譯通過。

## 實作順序（高層）

1. 移除 `apps/employee-app`（前端）+ 驗證三 app 仍 build。
2. employee RWD（底部導覽 + `/me` + header/cart 手機適配）。
3. merchant RWD（底部導覽 + 表格卡片化）。
4. admin RWD（底部導覽 + 表格兜底/卡片化 + header）。
5. 後端 Go 清理 employee-app OAuth + 重生成契約 + 測試。
6. 全面回歸：雙斷點截圖、各 app check/build、Go test。
