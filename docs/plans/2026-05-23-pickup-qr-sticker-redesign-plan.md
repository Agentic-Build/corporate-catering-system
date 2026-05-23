# 領餐流程重新設計 實作計畫（餐點貼紙 QR 自助核銷）

> **For Claude:** REQUIRED SUB-SKILL: 用 superpowers:subagent-driven-development 平行執行。
> 設計依據：`docs/plans/2026-05-23-pickup-qr-sticker-redesign-design.md`

**Goal:** 把核銷從「商家手動輸入動態碼」改為「員工掃描餐點貼紙 QR 自助核銷（驗證本人）」，並提供商家貼紙匯出與出餐掃描。

**Architecture:** 後端新增員工核銷 endpoint（沿用 ready→picked_up 狀態機 + 本人驗證 + 一次性冪等），移除 TOTP 機制；前端共用一個 QR payload 純函式 package；merchant 加貼紙匯出頁與出餐掃描；employee app/web 加掃描核銷（web 含手動輸入退路）；申訴沿用現有流程並放寬可申訴狀態。

**Tech Stack:** Go + Huma（後端）、SvelteKit + Svelte 5（前端）、Tauri（employee-app）、openapi-typescript（contract）、vitest（新增前端單元測試）、`html5-qrcode` 或 `@zxing/browser`（掃描器）。

---

## 波次與依賴

```
波次 1（平行：A 與 B 無相互依賴，檔案不重疊）
  ├─ Task A：共用 QR payload package + vitest 單元測試（packages/，純 TS）
  └─ Task B：後端員工核銷 API + 移除 TOTP + Go 測試 + contract-sync（services/, contract/, schema.d.ts）
        │ B 產出新 contract types（make contract-sync）
        ▼
波次 2（平行：C/D/E 各改自己的 app 目錄，皆依賴 A 的函式 + B 的 contract）
  ├─ Task C：merchant — 貼紙匯出頁 + 出餐掃描 + 移除 verify modal
  ├─ Task D：employee-app（Tauri）— 掃描核銷頁 + 移除 /totp
  └─ Task E：employee web — 掃描+手動輸入核銷 + 移除 TOTP 頁 + 申訴放寬
        ▼
波次 3（序列）
  └─ Task F：E2E 改寫 + 全量驗證（make test-go / test-web / test-e2e）
```

**平行執行守則（給每個 subagent）：**
- 只改自己 task 範圍內的檔案，不碰其他 task 的檔案。
- **不要自行 git commit**；完成後回報變更檔案清單與測試結果，由協調者統一 commit。
- 不要跑 `pnpm install` 以外的破壞性指令；波次 2 開始前 contract 已穩定。

---

## Task A：共用 QR payload package（packages/pickup）

**依賴：** 無。

**Files:**
- Create: `packages/pickup/package.json`
- Create: `packages/pickup/tsconfig.json`
- Create: `packages/pickup/vitest.config.ts`
- Create: `packages/pickup/src/index.ts`
- Create: `packages/pickup/src/pickup-qr.test.ts`

**契約：**
```ts
// QR 內容格式：tbite://pickup?order=<orderId>
export function buildPickupQR(orderId: string): string;
// 解析；非法/缺 order/錯 scheme/雜訊一律回 null
export function parsePickupQR(text: string): { orderId: string } | null;
```

**Step 1 — 寫失敗測試** `src/pickup-qr.test.ts`：
```ts
import { describe, it, expect } from "vitest";
import { buildPickupQR, parsePickupQR } from "./index";

describe("pickup QR payload", () => {
  it("builds the tbite pickup URL", () => {
    expect(buildPickupQR("abc-123")).toBe("tbite://pickup?order=abc-123");
  });
  it("round-trips", () => {
    expect(parsePickupQR(buildPickupQR("o1"))).toEqual({ orderId: "o1" });
  });
  it("parses a valid payload", () => {
    expect(parsePickupQR("tbite://pickup?order=xyz")).toEqual({ orderId: "xyz" });
  });
  it("returns null when order param missing", () => {
    expect(parsePickupQR("tbite://pickup")).toBeNull();
  });
  it("returns null for wrong scheme/host", () => {
    expect(parsePickupQR("https://evil?order=x")).toBeNull();
    expect(parsePickupQR("tbite://other?order=x")).toBeNull();
  });
  it("returns null for garbage / empty", () => {
    expect(parsePickupQR("garbage")).toBeNull();
    expect(parsePickupQR("")).toBeNull();
  });
});
```

**Step 2 — 跑測試確認失敗：** `pnpm --filter @tbite/pickup test` → FAIL（模組不存在）。

**Step 3 — 最小實作** `src/index.ts`：用字串解析（避免 `new URL` 對自訂 scheme 的環境差異）：
```ts
export function buildPickupQR(orderId: string): string {
  return `tbite://pickup?order=${orderId}`;
}

const PREFIX = "tbite://pickup?";
export function parsePickupQR(text: string): { orderId: string } | null {
  if (!text?.startsWith(PREFIX)) return null;
  const params = new URLSearchParams(text.slice(PREFIX.length));
  const orderId = params.get("order");
  return orderId ? { orderId } : null;
}
```

**Step 4 — 跑測試確認通過：** `pnpm --filter @tbite/pickup test`（含 `pnpm install` 安裝 vitest）→ PASS。

**Step 5 — 型別檢查：** `pnpm --filter @tbite/pickup check` → 通過。

**package.json 要點：** `"name": "@tbite/pickup"`、`"type": "module"`、devDeps `vitest`、`typescript`；scripts `test: "vitest run"`、`check: "tsc --noEmit"`；`main`/`exports` 指向 `src/index.ts`。對齊 `packages/api-client/package.json` 既有風格。

**回報：** 變更檔案清單 + vitest 輸出。

---

## Task B：後端員工核銷 API + 移除 TOTP + contract-sync

**依賴：** 無（與 A 平行）。

**Files:**
- Modify: `services/api/internal/order/service.go`（新增 `Pickup`；移除 `VerifyPickup`）
- Modify: `services/api/internal/order/http/handlers.go`（新增 `pickup` handler+路由；移除 `getPickupCode`/`verifyPickup` 與其 input/output struct、路由）
- Modify: `services/api/internal/order/service.go` 的 `Place()`（移除 `totp.NewSecret`）
- Modify: `services/api/internal/order/postgres/order_repo.go`（`CreateTx` 的 `totp_secret` 改寫 `nil`）
- Delete: `services/api/internal/pickup/totp/`（totp.go、totp_test.go）
- Modify: `services/api/internal/order/errors.go`（移除 `ErrInvalidPickupCode`）
- Modify: `services/api/internal/order/service_test.go`（移除 VerifyPickup 系列；新增 Pickup 系列）
- Check/Modify: `services/api/internal/order/perf/pickup_perf_test.go`（移除或改寫，因依賴 totp）
- Run: `make contract-sync`

**新增 `Service.Pickup`（複製 VerifyPickup 骨架，去 totp、加 owner 檢查）：**
```go
// Pickup atomically transitions READY → PICKED_UP for the order's OWNER.
// Employee self-service: the scanned QR carries only the order id; ownership
// is enforced here. The conditional UPDATE guarantees one-time idempotency.
func (s *Service) Pickup(ctx context.Context, orderID, employeeID string) error {
	o, err := s.Orders.GetByID(ctx, orderID)
	if err != nil {
		return err
	}
	if o.UserID != employeeID {
		return ErrForbidden
	}
	if o.Status != StatusReady {
		return ErrInvalidTransition
	}
	return pgx.BeginFunc(ctx, s.Pool, func(tx pgx.Tx) error {
		if err := s.OrdersTx.MarkPickedUpTx(ctx, tx, orderID); err != nil {
			return err
		}
		from := StatusReady
		evRole := "employee"
		if err := s.StateTx.AppendTx(ctx, tx, &StateEvent{
			OrderID: orderID, FromState: &from, ToState: StatusPickedUp,
			ActorID: &employeeID, ActorRole: &evRole,
			Reason: "qr_pickup", Payload: map[string]any{},
		}); err != nil {
			return err
		}
		payload := map[string]any{"order_id": orderID, "vendor_id": o.VendorID}
		if err := s.OutboxTx.AppendTx(ctx, tx, "order", orderID, "order.picked_up.v1", payload, map[string]any{}); err != nil {
			return err
		}
		return s.AuditTx.WriteTx(ctx, tx, &employeeID, &evRole, "order.picked_up", "order", orderID, payload, "")
	})
}
```

**新增 handler + 路由**（handlers.go，沿用 `orderIDInput` 與 `requireEmployee`）：
```go
func (a *API) pickup(ctx context.Context, in *orderIDInput) (*struct{}, error) {
	u, err := a.requireEmployee(ctx)
	if err != nil {
		return nil, err
	}
	if err := a.Svc.Pickup(ctx, in.ID, u.ID); err != nil {
		return nil, mapErr(err)
	}
	return &struct{}{}, nil
}
```
路由：`OperationID: "pickupOrder"`, `POST /api/employee/orders/{id}/pickup`, Tags `{"employee","order"}`, Security bearer, DefaultStatus 204。

**Step 1 — 先寫失敗測試**（`service_test.go`，沿用既有測試 helper / 建單 → MarkReady → ...）：
- `TestService_Pickup_Happy`：本人 + ready → picked_up
- `TestService_Pickup_NotOwner`：他人 employeeID → `ErrForbidden`
- `TestService_Pickup_WrongStatus`：placed → `ErrInvalidTransition`
- `TestService_Pickup_Double`：核銷兩次 → 第二次 `ErrInvalidTransition`（沿用 1000-racer 或簡化雙呼叫）
- `TestService_Pickup_NotFound`：隨機 id → `ErrOrderNotFound`

**Step 2 — 跑測試確認失敗：** `go test ./services/api/internal/order/ -run Pickup -v`（需 Docker/testcontainers）→ FAIL（Pickup 未定義）。

**Step 3 — 實作 + 移除 TOTP（見上）。** 移除順序避免編譯錯：先刪 handler/service 對 totp 的引用，再刪 package；`Place()` 移除 secret 產生；`CreateTx` 的 `totp_secret` 參數傳 `nil`。

**Step 4 — 跑測試確認通過：** `go test ./services/api/internal/order/... -v` 全綠；`go build ./...` 無錯。

**Step 5 — 同步 contract：** `make contract-sync` → 確認 `contract/openapi/openapi.yaml` 出現 `/api/employee/orders/{id}/pickup`、消失 `pickup-code`/`verify-pickup`；`packages/api-client/src/schema.d.ts` 同步更新。

**回報：** 變更/刪除檔案清單、`go test` 輸出、contract diff 摘要。

---

## Task C：merchant — 貼紙匯出 + 出餐掃描 + 移除 verify modal

**依賴：** 波次 1（A 的 `@tbite/pickup`、B 的 contract）。

**Files:**
- Modify: `apps/merchant/src/routes/orders/+page.svelte`（移除 verify modal；「核銷」鈕改為「出餐」掃描入口）
- Modify: `apps/merchant/src/routes/orders/+page.server.ts`（移除 `verifyPickup` action；`markReady` 沿用，支援單筆）
- Create: `apps/merchant/src/routes/labels/+page.server.ts`（載當日訂單）
- Create: `apps/merchant/src/routes/labels/+page.svelte`（貼紙網格：訂單編號=`id.slice(0,8)` + QR by `buildPickupQR`；列印 CSS）
- Modify: `apps/merchant/package.json`（加掃描器 lib + `@tbite/pickup` workspace dep）

**重點：**
- 貼紙頁用 `qrcode` 產生 QR 圖（已是依賴），內容 = `buildPickupQR(order.id)`；每張貼紙顯示訂單編號（前 8 碼）+ QR + 取餐區/日期。`@media print` 排版成標籤網格。
- 出餐掃描：在備餐看板，掃 QR → `parsePickupQR` 取 orderId → 送現有 `?/markReady`（單筆 `order_ids:[id]`）→ placed/cutoff→ready。
- 移除手動輸入動態碼 modal 與相關 state。

**驗證：** `pnpm --filter @tbite/merchant check` 通過；vendor 登入後 `/labels` 可印、看板掃描可標出餐。回報變更清單。

---

## Task D：employee-app（Tauri）— 掃描核銷 + 移除 /totp

**依賴：** 波次 1。

**Files:**
- Create: 掃描核銷頁（如 `apps/employee-app/src/routes/scan/+page.svelte`），用裝置相機掃 QR → `parsePickupQR` → 呼叫新 API
- Modify: `apps/employee-app/src/lib/api.ts`（新增 `pickupOrder(orderId)` 呼叫 `POST /api/employee/orders/{id}/pickup`；移除 `getPickupCode`）
- Delete: `apps/employee-app/src/routes/totp/`
- Modify: 移除指向 `/totp` 的導覽連結
- Modify: `apps/employee-app/package.json`（掃描器 lib + `@tbite/pickup`）

**`api.ts` 新增：**
```ts
export async function pickupOrder(orderId: string): Promise<void> {
  const res = await client().POST("/api/employee/orders/{id}/pickup", {
    params: { path: { id: orderId } },
  });
  if (res.error) throw new Error(problem(res.error));
}
```
**驗證：** `pnpm --filter @tbite/employee-app check` 通過。回報變更清單。

---

## Task E：employee web — 掃描+手動輸入核銷 + 移除 TOTP 頁 + 申訴放寬

**依賴：** 波次 1。

**Files:**
- Create: 掃描核銷頁（如 `apps/employee/src/routes/scan/+page.svelte` + `+page.server.ts`）：瀏覽器相機掃描 + 「手動輸入訂單編號」退路（比對本人訂單前 8 碼後呼叫 pickup）。「找不到餐」連結 → `/orders/{id}/dispute`
- Delete: `apps/employee/src/routes/orders/[id]/pickup/`
- Delete: `apps/employee/src/lib/components/TotpView.svelte`、`TotpModal.svelte`（確認無其他引用）
- Modify: 移除指向 pickup/TOTP 的連結
- Modify: `apps/employee/src/routes/orders/[id]/dispute/+page.server.ts:6` — `DISPUTABLE` 加入 `"ready"`
- Modify: `apps/employee/package.json`（掃描器 lib + `@tbite/pickup`）

**手動輸入退路語意：** 員工輸入貼紙上的訂單編號（前 8 碼）→ server 比對「本人訂單中符合前綴者」→ 呼叫 `POST /api/employee/orders/{id}/pickup`（後端再次驗本人）。前綴若多筆相符則提示重輸完整 id。

**驗證：** `pnpm --filter @tbite/employee check` 通過。回報變更清單。

---

## Task F：E2E 改寫 + 全量驗證（序列）

**依賴：** 波次 2 全部完成。

**Files:**
- Modify: `tests/e2e/order-pickup.spec.ts` — 改為：商家標出餐 → 員工掃描/手動輸入核銷 happy path；非本人/錯誤輸入；找不到→申訴入口。移除 TOTP 展示頁相關斷言。

**驗證：**
- `make test-go` 全綠
- `make test-web`（`pnpm -r check && pnpm -r lint`）全綠
- `pnpm --filter @tbite/pickup test` 全綠
- 啟 dev 後 `make test-e2e`（或調整後的 spec）

---

## 收尾
- 確認 `grep -rn "totp\|TOTP\|pickup-code\|verify-pickup" services/ apps/` 無殘留引用（schema.d.ts 已更新）。
- 由協調者依波次統一 commit；最後彙整成 PR 供 review。
