# 企業訂餐系統 — UX 重新設計藍圖 (Blueprint v2)

> 本文件是整個前端重新設計的**單一權威來源**。改 UI、新增路由、改 navigation 時，必須先來這裡對齊。
>
> 對應 user story 定義在 `docs/user-story-book.md`。
>
> 註 (2026-05-28)：本藍圖所驅動的「舊版 `*-portal-mvp.svelte` 巨型元件重構」已完成（現為 task-oriented `+page.svelte` 路由）；以下路由/導覽藍圖仍為設計參考。

---

## 1. 為什麼要重新設計

舊版前端以單一大 `*-portal-mvp.svelte` 元件（1,200–2,700 行）塞滿一個 `[section]` 動態路由。每個 section 內又混合 5–15 張表單 / 表格。結果：

- **認知負荷爆炸**：員工打開「訂單」頁就看到 5 張表單、3 張表格，完全不知道「現在要幹嘛」。
- **看不到目前任務焦點**：沒有 breadcrumb，沒有 task header，使用者在哪個流程中完全靠直覺。
- **深連結失效**：取消訂單、顯示領餐 QR、查某批次，都沒有獨立 URL，無法分享 / 書籤。
- **無法響應性展開**：所有 section 的 loading state 都擠在同一個元件裡，互相干擾。

因此重新設計必須滿足 4 條設計原則。

---

## 2. 設計原則

### P1. 任務導向路由 (Task-oriented Routing)
每個 URL **對應一件事**。`/employee/orders/[orderId]/pickup` 就是「領取這張訂單」，不是「順便看看訂單」。

### P2. 漸進揭露 (Progressive Disclosure)
首頁（角色 home）只顯示「今天要我做什麼」。進階工具與歷史資料放在子路由。

### P3. 清楚的任務框架 (Task Framing)
每頁頂部固定三件事：
- **Breadcrumb**：我在整個導覽樹的哪裡
- **Task Title**：我正在做什麼
- **Task Description + Primary Action**：我下一步能做什麼

### P4. 一致的跨角色語彙 (Consistent Cross-role Vocabulary)
所有角色共用同一批 UI primitive（`Card`、`DataTable`、`StateTag`、`FormField`、`ConfirmDialog`、`Toast`）。視覺基調仍依角色微調：
- 員工：手機優先、圓角大、配色溫暖
- 商家：桌機優先、資訊密度高、配色中性
- 管理員：桌機優先、表格導向、配色專業

---

## 3. 新路由結構

### 3.1 員工 Employee — `/employee/*`

```
/employee                                 # 今日 Home（我的當日餐點、待領取）
/employee/discover                        # 菜單瀏覽（週 / 日曆檢視 + 篩選）
/employee/discover/[deliveryDate]         # 特定日期菜單（deep link）
/employee/discover/vendors/[vendorId]     # 特定商家菜單
/employee/orders                          # 我的訂單列表
/employee/orders/[orderId]                # 訂單詳情
/employee/orders/[orderId]/edit           # 修改訂單（獨立頁）
/employee/orders/[orderId]/cancel         # 取消訂單（獨立頁，有確認）
/employee/orders/[orderId]/pickup         # 領餐 QR（全螢幕）
/employee/orders/[orderId]/dispute        # 提交申訴
/employee/wallet                          # 薪資扣款（原 payroll，改名更直覺）
/employee/wallet/[orderId]                # 特定訂單的扣款明細 + 申訴追蹤
/employee/settings                        # 個人偏好（尖峰提醒等；第二階段）
```

導覽階層（主頁底部 bottom-tab bar，手機優先）：
1. 🍱 今日 (`/employee`)
2. 🔍 菜單 (`/employee/discover`)
3. 📦 訂單 (`/employee/orders`)
4. 💳 扣款 (`/employee/wallet`)

### 3.2 商家 Vendor — `/vendor/*`

```
/vendor                                   # 今日儀表板（今天要備餐多少、緊急事件）
/vendor/today                             # 今日作業看板（fulfillment board）
/vendor/today/[plantId]                   # 特定廠區的今日作業
/vendor/menu                              # 菜單總覽
/vendor/menu/new                          # 新增菜單
/vendor/menu/[menuItemId]                 # 編輯菜單（含狀態切換）
/vendor/schedule                          # 訂購視窗政策（預購區間 / 截單時間）
/vendor/batches                           # 備餐批次列表
/vendor/batches/new                       # 建立批次
/vendor/batches/[batchId]                 # 批次詳情（可列印）
/vendor/orders                            # 營運訂單查詢（日期 / 狀態 / 廠區）
/vendor/orders/[orderId]                  # 訂單詳情（狀態推進）
/vendor/compliance                        # 合規狀態（狀態 banner + 文件清單）
/vendor/compliance/upload                 # 建立上傳計畫
/vendor/compliance/access-links           # 建立下載連結
/vendor/insights                          # 營運分析（原隱藏能力，曝光）
```

導覽階層（桌機 side-nav）：
- 今日 | 菜單 | 訂購政策 | 批次 | 訂單 | 合規 | 分析

### 3.3 福委會管理員 Admin — `/admin/*`

```
/admin                                    # 總覽 + 統一 Inbox（待辦）
/admin/vendors                            # 商家清單
/admin/vendors/[vendorId]                 # 商家詳情
/admin/vendors/[vendorId]/review          # 提交審核決策
/admin/vendors/[vendorId]/mappings        # 該商家的廠區映射
/admin/compliance/templates               # 合規文件模板
/admin/compliance/templates/new           # 新增模板
/admin/compliance/templates/[id]          # 編輯模板
/admin/compliance/lifecycle               # 執行 lifecycle（含 dry run）
/admin/settlement                         # 月結作業 hub
/admin/settlement/close                   # 執行關帳（ISS-003 簽核）
/admin/settlement/cycles                  # 週期列表
/admin/settlement/cycles/[cycleKey]       # 週期詳情 + 鎖/解鎖
/admin/settlement/disputes                # 爭議列表
/admin/settlement/disputes/[disputeId]    # 爭議處理
/admin/anomalies                          # 告警列表
/admin/anomalies/[alertId]                # 告警詳情（含 ISS-007 簽核 CLOSE）
/admin/anomalies/evaluate                 # 手動評估規則
/admin/anomalies/rules                    # 規則列表
/admin/anomalies/rules/new                # 新增規則
/admin/anomalies/rules/[ruleId]           # 編輯規則
/admin/audit                              # 稽核查詢
/admin/audit/responsibilities             # 責任歸屬
/admin/analytics                          # 營運分析儀表板
```

導覽階層（桌機 side-nav + 子 tab）：
- 總覽 | 商家 | 合規 | 月結 | 異常 | 稽核 | 分析

---

## 4. 導覽模型 (Navigation Model)

### 4.1 三層導覽階層

```
Role Portal     →  Section        →  Task
/employee       →  /discover      →  /discover/vendors/[vendorId]
```

每層都有自己的 UI 元素：
- **Role Portal** = 最外層 header（角色切換 + 登出）
- **Section** = 左側（桌機）或底部（手機）主導覽
- **Task** = 主 content 區域頂部的 breadcrumb + task header

### 4.2 Navigation tree 定義

`lib/platform/navigation.ts` 重新設計為**樹狀資料結構**：

```ts
type NavNode = {
  id: string;                    // 穩定 ID 供 active 比對
  labelKey: string;              // 指向 i18n 的 key
  href: string;                  // 相對完整路徑
  icon?: string;                 // heroicons name
  children?: NavNode[];
};

type RoleNavigationTree = {
  role: PortalRole;
  primary: NavNode[];            // section 級（side-nav / bottom-tab）
};
```

### 4.3 Breadcrumb 自動化

每個 `+page.ts` 回傳 `breadcrumbs: BreadcrumbItem[]` 給 layout 使用：

```ts
export interface BreadcrumbItem {
  label: string;
  href: string | null;   // null = 當前頁，不可點
}
```

Layout 讀 `data.breadcrumbs` 自動渲染。

### 4.4 Task header

每個 page 提供 `pageHeader: { title, description, primaryAction? }`。Layout 依此渲染統一 header。

---

## 5. UI Primitive Inventory

全部放在 `apps/web/src/lib/components/ui/`，使用 Svelte 5 runes。

| Primitive | 檔名 | 用途 |
|---|---|---|
| `Card` | `card.svelte` | 內容容器（邊框 + 標題 + body） |
| `Button` | `button.svelte` | 支援 `primary / secondary / danger / ghost` variant |
| `StateTag` | `state-tag.svelte` | 狀態徽章（PENDING/FULFILLED/LOCKED…） |
| `Toast` | `toast.svelte` + `toast-store.ts` | 全域 toast store |
| `DataTable` | `data-table.svelte` | 具 `columns[]` props 的表格 |
| `FormField` | `form-field.svelte` | label + input + help + error |
| `Breadcrumb` | `breadcrumb.svelte` | 渲染階層導覽 |
| `PageHeader` | `page-header.svelte` | 統一任務 header |
| `EmptyState` | `empty-state.svelte` | 無資料顯示 |
| `LoadingState` | `loading-state.svelte` | 骨架屏 / spinner |
| `ConfirmDialog` | `confirm-dialog.svelte` | 破壞性操作二次確認 |
| `MoneyAmount` | `money-amount.svelte` | 顯示 minor→major + currency |
| `CountdownBadge` | `countdown-badge.svelte` | 截單倒數 / QR 倒數 |

---

## 6. 各角色 Landing 頁設計

### 6.1 員工 Home (`/employee`)

**目標**：告訴員工「今天要你做什麼」。

```
┌─────────────────────────────────────┐
│ 下午好，王小明                        │
│ ──────────────────────────────────── │
│                                      │
│ 📦 你有 1 筆待領取                   │
│    [ 今天 12:00 雞腿便當 (顯示 QR) ]  │
│                                      │
│ ⏰ 明日 12:00 截單剩 2 小時           │
│    [ 看明日菜單 → ]                  │
│                                      │
│ 💬 1 筆申訴進行中                    │
│    [ 查看 → ]                        │
└─────────────────────────────────────┘
```

四個快速入口（底部 tab bar）。

### 6.2 商家 Today (`/vendor`)

**目標**：告訴商家「今天的備餐節奏」。

```
┌────────────────────────────────────────────┐
│ 2026-04-19 · 第 17 週                      │
│ ──────────────────────────────────────────│
│ [今日備餐 120 份] [待上車 45] [已送達 30]  │
│                                            │
│ 🚨 即將截單 (17:00)                        │
│    A 廠：38 份 | B 廠：20 份 | C 廠：12 份 │
│                                            │
│ 📋 合規狀態：通過 (下次到期 2026-07-01)    │
│                                            │
│ [ 進入今日作業看板 → ]                     │
└────────────────────────────────────────────┘
```

### 6.3 管理員 Overview (`/admin`)

**目標**：告訴管理員「今天該處理什麼治理事件」。

```
┌───────────────────────────────────────┐
│ 統一 Inbox                            │
│ ─────────────────────────────────────│
│ 🏢 待審商家       (3)                 │
│ 🚨 開放告警       (5, 2 SLA breached) │
│ 💰 月結例外待處理 (12)                │
│ 📋 爭議待解決     (4)                 │
│                                       │
│ 快速動作：                            │
│ [執行月結關帳] [執行 lifecycle]        │
└───────────────────────────────────────┘
```

---

## 7. 執行順序（本次 PR）

1. ✅ 寫本藍圖（這份文件）
2. ⏭ 重新設計 `navigation.ts` 以支援 `NavNode` 樹 + breadcrumbs + pageHeader
3. ⏭ 重寫 `+layout.svelte` 使用新 nav model
4. ⏭ 建立 UI primitives（`lib/components/ui/*`）
5. ⏭ 刪除舊 `[section]/+page.svelte`，改為每個 task 的具名 route
6. ⏭ 依路由拆 mega-component 為 per-route page component
7. ⏭ 更新 i18n 補齊新字串
8. ⏭ 首頁 `/` 改為「角色切換」卡片入口
9. ⏭ `pnpm check` + `pnpm build` 驗證

---

## 8. 未變 / 保留

- **API 客戶端層** (`lib/platform/api/*`) 與 **生成的 TypeScript client** (`contract/generated/ts-client/*`)：完全保留。
- **認證 / 授權** (`lib/server/auth/*`、`hooks.server.ts`)：完全保留。
- **Shell bootstrap** (`lib/platform/shell.ts`)：微調回傳 `NavigationTree`，核心邏輯不動。
- **後端 Rust 服務**：完全不動。
- **OpenAPI contract**：完全不動。

---

## 9. 與 user-story-book.md 對照

本藍圖的**每個** URL 都對應 user-story-book 裡一個或多個 Story。具體對應於 blueprint 維護期間以表格形式補足（見 `docs/ux-redesign-url-matrix.md`，未來再補）。
