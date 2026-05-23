# 領餐流程重新設計：餐點貼紙 QR 自助核銷

> 日期：2026-05-23
> 取代：`docs/plans/2026-05-13-p4-pickup-totp.md`（動態 TOTP 核銷）

## 背景與問題（已驗證）

現行核銷流程是「員工出示動態 6 位數碼，**商家手動輸入**」核銷（`apps/merchant/.../orders/+page.svelte` 的 verify modal → `POST /api/merchant/orders/{id}/verify-pickup`）。經驗證確認兩個問題：

1. **設計不合理**：商家不會在領餐區逐筆盯著員工手機打字核銷。核銷按鈕甚至用了 `qr` 圖示，卻只是一個數字輸入框，設計意圖與實作不符。
2. **實際上常核銷不了**：後端 TOTP 邏輯本身自洽（演算法、secret、狀態機、原子性測試都正確），但 TOTP 視窗只有 ±1 step（最多 90 秒，`totp.go:40`）。「員工出示 → 商家找到該筆 → 開 modal → 抄碼 → 送出」極易超過 90 秒，導致**系統性過期失敗**，而非偶發。

## 目標

把領餐改為**員工自助掃描餐點貼紙 QR** 的閉環流程：

- 商家可匯出「訂單編號 + QR」貼紙，貼在餐點上。
- 商家出餐時掃 QR 標記出餐。
- 員工到領餐區掃自己餐上的 QR 自助核銷，後端驗證本人。
- 員工找不到餐時可申訴。

## 非目標

- 不引入第三方標準 TOTP/authenticator（QR 不再含動態碼）。
- 不新增即時推播/email 通知（申訴沿用現有流程）。
- 不處理離線核銷。

## 核心設計決策（brainstorming 結論）

| 決策點 | 結論 |
|--------|------|
| 身分驗證 | QR 內含 order id；核銷時後端驗證 `order.user_id == 登入員工`，掃到別人的貼紙會被拒（順便防拿錯餐）。 |
| QR 內容 | `tbite://pickup?order={orderId}`，**移除動態 code**，安全靠登入身分比對。 |
| 出餐狀態 | **沿用現有 `ready`**，不新增 `served` enum。商家逐份掃描 = `placed → ready`，取代批次 `markReady`。 |
| 核銷觸發 | 從「商家手動輸入」改為「**員工掃描**」，`ready → picked_up`。 |
| 訂單編號 | **沿用 order.id 前 8 碼**（現有 UI 慣例 `o.id.slice(0,8)`），印在貼紙、供 web 手動輸入退路比對。無需 migration。 |
| 動態碼機制 | **完全移除**：商家手動輸入 modal、員工 TOTP 展示頁、totp 機制。 |
| employee web | 瀏覽器相機掃描 **+** 手動輸入訂單編號退路（桌機/相機被拒時）。 |
| employee app | Tauri 裝置相機掃描。 |
| 申訴 | 沿用現有 `/orders/[id]/dispute` + `POST /api/employee/disputes`，**放寬可申訴狀態**讓「未領取前」也能申訴。 |

## 狀態機

```
placed ──(商家掃描出餐)──▶ ready ──(員工掃描核銷, 驗證本人)──▶ picked_up
                                  └─(找不到 → 申訴)──▶ dispute(沿用現有 complaint)
```

沿用現有 `order_status` enum（`draft, placed, cutoff, cancelled, ready, picked_up, no_show, refunded`），不新增值。`MarkNoShow`、`CanTransition` 等依賴 `ready` 的既有邏輯不受影響。

## 後端 API 變更

**新增**
- `POST /api/employee/orders/{id}/pickup` — 員工自助核銷
  - 驗證登入員工 == `order.user_id`（否則 403 Forbidden）
  - 狀態須為 `ready`（否則 409 / ErrInvalidTransition）
  - 條件式 UPDATE 保證一次性冪等（沿用 `MarkPickedUpTx` 模式）
  - 寫 StateEvent（reason `qr_pickup`）、Outbox（`order.picked_up.v1`）、AuditEvent

**改造**
- 商家出餐掃描：沿用 `POST /api/merchant/orders/mark-ready`，前端改為「掃一筆送一筆」（body 傳單一 id）。不需新端點。

**移除**
- `GET /api/employee/orders/{id}/pickup-code`（TOTP 產碼）
- `POST /api/merchant/orders/{id}/verify-pickup`（商家手動核銷）
- `services/api/internal/pickup/totp/`（totp.go、totp_test.go）
- `Service.VerifyPickup`、handler `verifyPickup`、`getPickupCode`

## 資料模型變更

- `order.totp_secret` 欄位：**保留但停用**（避免 migration 風險；`CreateTx` 可繼續寫入或改寫 NULL）。後續可獨立 migration 清除。
  - 取捨：留欄位最小改動；若要乾淨可加 `000xxx_drop_totp_secret`。預設保留。

## 前端變更

**packages（共用）**
- 新增 pickup QR payload 的 build/parse 純函式（`buildPickupQR(orderId)` / `parsePickupQR(text)`），讓員工 app、employee web、（產生端）共用。**這是首要的可單元測試單元。**

**apps/merchant（web）**
1. 新增**貼紙匯出/列印頁**（`/orders/labels` 或 `/labels`）：列當日訂單，每筆渲染 `[訂單編號(前8碼) + QR(buildPickupQR)]`，列印用 CSS（A4/標籤紙網格）。
2. **出餐掃描**：在備餐看板加掃描器（`@zxing/library` 或 `html5-qrcode`），掃到 QR → parse order id → 呼叫 mark-ready（單筆）。保留批次「標記出餐」按鈕作為退路。
3. **移除** verify modal（手動輸入動態碼）。

**apps/employee-app（Tauri）**
1. 新增掃描核銷頁：裝置相機掃 QR → parse → `POST /pickup`。
2. **移除** `/totp` 路由與 TOTP 相關 UI。

**apps/employee（web）**
1. 新增掃描核銷：瀏覽器相機掃描 + 「手動輸入訂單編號」退路（比對本人訂單前 8 碼後核銷）。
2. **移除** `/orders/[id]/pickup`（TotpView）、`TotpModal`。
3. 掃描頁「找不到餐」入口 → 導向 `/orders/[id]/dispute`。

**申訴放寬**
- `apps/employee/src/routes/orders/[id]/dispute/+page.server.ts:6` 的 `DISPUTABLE` 由 `{picked_up, no_show}` 放寬，加入 `ready`（出餐後未領取也能申訴「找不到餐」）。

## 移除清單（彙整）

- 後端：totp package、`VerifyPickup`、`getPickupCode`、verify-pickup/pickup-code 路由
- merchant：verify modal
- employee：`/orders/[id]/pickup`、`TotpView.svelte`、`TotpModal.svelte`
- employee-app：`/totp` 路由
- 對應測試：`totp_test.go`、`VerifyPickup` 系列 test、`order-pickup.spec.ts`（改寫）

## 測試計畫

**後端（Go，testify + testcontainers）**
- `Pickup`（新）service test：
  - 本人 + ready → 成功 `picked_up`
  - 非本人 → `ErrForbidden`
  - 狀態非 ready → `ErrInvalidTransition`
  - 重複核銷 → 冪等（第二次拒絕，沿用 1000-racer 模式驗證原子性）
  - 訂單不存在 → `ErrOrderNotFound`
- 出餐掃描（mark-ready 單筆）：placed → ready
- 清理 totp 相關 test

**前端（共用純函式單元測試）**
- `parsePickupQR` / `buildPickupQR`：round-trip、合法/非法輸入、缺 order 參數、雜訊容錯

**E2E（Playwright）**
- 改寫 `order-pickup.spec.ts`：商家出餐掃描 → 員工掃描核銷 happy path；非本人/錯誤輸入；找不到 → 申訴入口

## 風險與取捨

- **手動輸入退路無法強證到場**：員工本人可在桌機輸入自己貼紙編號核銷而不實際到場。風險可接受 —— 僅限本人核銷自己的餐，且商家出餐掃描端已證明餐確實做出。
- **靜態 QR 可被拍照**：因核銷需登入本人 + 一次性，他人拍到也無法核銷該筆（非本人會被拒）。
- **掃描器相依新 library**：需評估 bundle 體積與 Tauri/瀏覽器相機權限。
