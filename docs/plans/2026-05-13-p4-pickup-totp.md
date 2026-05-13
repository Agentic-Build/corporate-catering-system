# P4 TOTP 核銷 + 商家備餐 + 領餐流程 Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans (or superpowers:subagent-driven-development) to implement this plan task-by-task.

**Goal**：交付完整的領餐流程 — 商家對訂單批量標記 READY、員工到場出示 TOTP QR、商家掃描完成核銷（PICKED_UP）、no-show 自動處理；並通過 1000 並發 verify 證明 p95 < 100ms。

**Architecture**：TOTP secret 隨 order 一起 generate（per-order，不 per-user，避免 cross-order replay）；條件式 `UPDATE "order" SET status='picked_up' WHERE id=? AND status='ready' AND totp_valid` 保證原子轉換。Fulfillment 視圖是 Postgres GROUP BY 查詢（不需 Redis projection — P5+ 再考慮）。商家「備餐看板」是 server-rendered, 每 N 秒重新整理。

**Tech Stack**：沿用 + 標準 `crypto/hmac` + `crypto/sha256` 做 TOTP（不額外引入 lib）。前端 QR 用 `qrcode` (3KB pure-JS).

**Branch**：`feat/p4-pickup-totp`（已切）

**Scope boundary**：
- P4 **做**：cutoff→ready (vendor)、ready→picked_up (TOTP) / no_show (admin)、TOTP 產生 + verify、商家備餐看板 (今日 by plant)、員工 pickup QR、no-show TTL scan in scheduler、1000-racer concurrency proof
- P4 **不**做**：refunds (留 P5)、payroll batches (留 P5)、商家退單 (sup vendor cancel — 留 P5)、QR offline-friendly (純 online verify)

---

## Task 1：Migration — totp + no_show fields

**Files**: `migrations/000004_pickup_totp.up.sql` + `.down.sql`

**Step 1**：up.sql

```sql
-- Per-order TOTP secret used for pickup verification.
ALTER TABLE "order"
  ADD COLUMN totp_secret BYTEA NOT NULL DEFAULT decode('00', 'hex'),
  ADD COLUMN ready_at TIMESTAMPTZ,
  ADD COLUMN picked_up_at TIMESTAMPTZ,
  ADD COLUMN no_show_at TIMESTAMPTZ;

CREATE INDEX order_ready_idx ON "order"(vendor_id, supply_date) WHERE status = 'ready';
CREATE INDEX order_pickup_pending_idx ON "order"(ready_at) WHERE status = 'ready';
```

Note: existing P3 rows get a default zero-byte secret; P4 service code rotates them when transitioning to PLACED. The default is fine because existing rows are seeded test data.

**Step 2**：down.sql

```sql
DROP INDEX IF EXISTS order_pickup_pending_idx;
DROP INDEX IF EXISTS order_ready_idx;
ALTER TABLE "order"
  DROP COLUMN IF EXISTS no_show_at,
  DROP COLUMN IF EXISTS picked_up_at,
  DROP COLUMN IF EXISTS ready_at,
  DROP COLUMN IF EXISTS totp_secret;
```

**Step 3**：verify up/down/up via Docker (same script as P3).

**Step 4**：commit

```bash
git add migrations
git commit -m "feat(db): order.totp_secret + ready_at/picked_up_at/no_show_at columns"
```

---

## Task 2：TOTP utility package

**Files**: `services/api/internal/pickup/totp/totp.go` + `totp_test.go`

```go
package totp

import (
    "crypto/hmac"
    "crypto/rand"
    "crypto/sha256"
    "encoding/binary"
    "fmt"
    "time"
)

// SecretBytes is the length of a TOTP secret in bytes (256-bit).
const SecretBytes = 32

// StepSeconds is the time-step duration. Tokens rotate every 30s.
const StepSeconds = 30

// Digits is the displayed TOTP length.
const Digits = 6

// NewSecret returns a fresh 32-byte random secret.
func NewSecret() ([]byte, error) {
    b := make([]byte, SecretBytes)
    if _, err := rand.Read(b); err != nil {
        return nil, fmt.Errorf("totp secret: %w", err)
    }
    return b, nil
}

// Generate returns a TOTP code for the given secret at the given time.
func Generate(secret []byte, t time.Time) string {
    counter := uint64(t.Unix() / StepSeconds)
    return hotp(secret, counter)
}

// Verify checks if `code` matches the secret within ±1 time-step window.
// Constant-time compare prevents timing side channels.
func Verify(secret []byte, code string, now time.Time) bool {
    counter := uint64(now.Unix() / StepSeconds)
    for _, delta := range []int64{0, -1, 1} {
        expected := hotp(secret, uint64(int64(counter)+delta))
        if hmac.Equal([]byte(expected), []byte(code)) {
            return true
        }
    }
    return false
}

func hotp(secret []byte, counter uint64) string {
    buf := make([]byte, 8)
    binary.BigEndian.PutUint64(buf, counter)
    mac := hmac.New(sha256.New, secret)
    mac.Write(buf)
    sum := mac.Sum(nil)
    offset := sum[len(sum)-1] & 0x0f
    code := binary.BigEndian.Uint32(sum[offset:offset+4]) & 0x7fffffff
    mod := uint32(1)
    for i := 0; i < Digits; i++ { mod *= 10 }
    return fmt.Sprintf("%0*d", Digits, code%mod)
}
```

**Tests**：

```go
package totp_test

import (
    "testing"
    "time"

    "github.com/stretchr/testify/assert"
    "github.com/stretchr/testify/require"

    "github.com/takalawang/corporate-catering-system/services/api/internal/pickup/totp"
)

func TestNewSecret_Length(t *testing.T) {
    s, err := totp.NewSecret()
    require.NoError(t, err)
    assert.Len(t, s, totp.SecretBytes)
}

func TestGenerate_Deterministic(t *testing.T) {
    sec := make([]byte, 32)
    now := time.Unix(1700000000, 0)
    assert.Equal(t, totp.Generate(sec, now), totp.Generate(sec, now))
    assert.Equal(t, totp.Digits, len(totp.Generate(sec, now)))
}

func TestVerify_AcceptsCurrentAndPrevWindow(t *testing.T) {
    sec, _ := totp.NewSecret()
    now := time.Now()
    prev := now.Add(-30 * time.Second)
    next := now.Add(30 * time.Second)
    codeNow := totp.Generate(sec, now)
    codePrev := totp.Generate(sec, prev)
    codeNext := totp.Generate(sec, next)

    assert.True(t, totp.Verify(sec, codeNow, now))
    assert.True(t, totp.Verify(sec, codePrev, now))
    assert.True(t, totp.Verify(sec, codeNext, now))

    // 2 windows back must fail
    twoAgo := now.Add(-60 * time.Second)
    codeTwoAgo := totp.Generate(sec, twoAgo)
    assert.False(t, totp.Verify(sec, codeTwoAgo, now))
}

func TestVerify_WrongSecretFails(t *testing.T) {
    sec1, _ := totp.NewSecret()
    sec2, _ := totp.NewSecret()
    now := time.Now()
    assert.False(t, totp.Verify(sec1, totp.Generate(sec2, now), now))
}
```

**Commit**:

```bash
git add services/api/internal/pickup
git commit -m "feat(pickup): TOTP utility (HMAC-SHA256, 30s window, ±1 tolerance)"
```

---

## Task 3：Extend order service for MarkReady, VerifyPickup, MarkNoShow

**Files**: 
- Modify: `services/api/internal/order/service.go` (add methods)
- Modify: `services/api/internal/order/postgres/order_repo.go` (add Tx variants for ready/picked_up/no_show updates that write the timestamp columns)
- Modify: `services/api/internal/order/types.go` (add `TOTPSecret []byte`, `ReadyAt *time.Time`, `PickedUpAt *time.Time`, `NoShowAt *time.Time` fields)
- Modify: postgres scan paths to include new columns

**New service methods**:

```go
// MarkReady transitions orders from cutoff → ready (vendor side).
// Used by vendor "備餐完成" UI; vendor may pass a list of order_ids or
// a (vendor_id, supply_date) filter to mark all at once.
func (s *Service) MarkReady(ctx context.Context, vendorID string, orderIDs []string, actorID string) error {
    return pgx.BeginFunc(ctx, s.Pool, func(tx pgx.Tx) error {
        for _, id := range orderIDs {
            o, err := s.Orders.GetByID(ctx, id)  // could be inside tx; for simplicity keep outside lock
            if err != nil { return err }
            if o.VendorID != vendorID { return ErrForbidden }
            if !CanTransition(o.Status, StatusReady) { return ErrInvalidTransition }
            if err := s.OrdersTx.MarkReadyTx(ctx, tx, id); err != nil { return err }
            // state event + audit + outbox
            from := o.Status
            evRole := "vendor_operator"
            if err := s.StateTx.AppendTx(ctx, tx, &StateEvent{
                OrderID: id, FromState: &from, ToState: StatusReady,
                ActorID: &actorID, ActorRole: &evRole,
                Reason: "vendor_ready", Payload: map[string]any{},
            }); err != nil { return err }
            payload := map[string]any{"order_id": id, "vendor_id": vendorID}
            if err := s.OutboxTx.AppendTx(ctx, tx, "order", id, "order.ready.v1", payload, map[string]any{}); err != nil { return err }
            if err := s.AuditTx.WriteTx(ctx, tx, &actorID, &evRole, "order.ready", "order", id, payload, ""); err != nil { return err }
        }
        return nil
    })
}

// VerifyPickup atomically transitions an order from READY → PICKED_UP.
// Returns ErrInvalidTransition if order status != ready or TOTP doesn't verify.
// This is the hot path for 1000-racer test.
func (s *Service) VerifyPickup(ctx context.Context, orderID, code string, vendorActorID string) error {
    o, err := s.Orders.GetByID(ctx, orderID)
    if err != nil { return err }
    if o.Status != StatusReady { return ErrInvalidTransition }
    if !totp.Verify(o.TOTPSecret, code, s.Clock.Now()) { return ErrInvalidPickupCode }

    return pgx.BeginFunc(ctx, s.Pool, func(tx pgx.Tx) error {
        if err := s.OrdersTx.MarkPickedUpTx(ctx, tx, orderID); err != nil { return err }
        from := StatusReady
        evRole := "vendor_operator"
        if err := s.StateTx.AppendTx(ctx, tx, &StateEvent{
            OrderID: orderID, FromState: &from, ToState: StatusPickedUp,
            ActorID: &vendorActorID, ActorRole: &evRole,
            Reason: "totp_verify", Payload: map[string]any{},
        }); err != nil { return err }
        payload := map[string]any{"order_id": orderID, "vendor_id": o.VendorID}
        if err := s.OutboxTx.AppendTx(ctx, tx, "order", orderID, "order.picked_up.v1", payload, map[string]any{}); err != nil { return err }
        return s.AuditTx.WriteTx(ctx, tx, &vendorActorID, &evRole, "order.picked_up", "order", orderID, payload, "")
    })
}

// MarkNoShow transitions READY orders older than `cutoffAge` to NO_SHOW.
// Run periodically by the scheduler (end-of-meal-window).
func (s *Service) MarkNoShow(ctx context.Context, cutoffAge time.Duration) (int, error) {
    threshold := s.Clock.Now().Add(-cutoffAge)
    pending, err := s.Orders.ListReadyOlderThan(ctx, threshold)
    if err != nil { return 0, err }
    n := 0
    for _, o := range pending {
        err := pgx.BeginFunc(ctx, s.Pool, func(tx pgx.Tx) error {
            if err := s.OrdersTx.MarkNoShowTx(ctx, tx, o.ID); err != nil { return err }
            from := StatusReady
            sysRole := "welfare_admin" // system-attributed via admin role; use null actor
            if err := s.StateTx.AppendTx(ctx, tx, &StateEvent{
                OrderID: o.ID, FromState: &from, ToState: StatusNoShow,
                ActorRole: &sysRole, Reason: "no_show_timeout",
                Payload: map[string]any{},
            }); err != nil { return err }
            payload := map[string]any{"order_id": o.ID}
            if err := s.OutboxTx.AppendTx(ctx, tx, "order", o.ID, "order.no_show.v1", payload, map[string]any{}); err != nil { return err }
            return s.AuditTx.WriteTx(ctx, tx, nil, &sysRole, "order.no_show", "order", o.ID, payload, "")
        })
        if err == nil { n++ }
    }
    return n, nil
}
```

Also: extend `Place` to generate a TOTP secret on order creation:

```go
// Inside Place(...), after building o:
secret, err := totp.NewSecret()
if err != nil { return nil, fmt.Errorf("generate totp: %w", err) }
o.TOTPSecret = secret
```

And add to `OrderRepo.CreateTx` the `totp_secret` column.

Add `ErrInvalidPickupCode = errors.New("order: invalid pickup code")` to `errors.go`.

New repo methods:

```go
func (r *OrderRepo) MarkReadyTx(ctx context.Context, tx pgx.Tx, id string) error {
    tag, err := tx.Exec(ctx, `UPDATE "order" SET status='ready', ready_at=now(), updated_at=now() WHERE id=$1 AND status IN ('cutoff','placed')`, id)
    if err != nil { return err }
    if tag.RowsAffected() == 0 { return order.ErrInvalidTransition }
    return nil
}
func (r *OrderRepo) MarkPickedUpTx(ctx context.Context, tx pgx.Tx, id string) error {
    tag, err := tx.Exec(ctx, `UPDATE "order" SET status='picked_up', picked_up_at=now(), updated_at=now() WHERE id=$1 AND status='ready'`, id)
    if err != nil { return err }
    if tag.RowsAffected() == 0 { return order.ErrInvalidTransition }
    return nil
}
func (r *OrderRepo) MarkNoShowTx(ctx context.Context, tx pgx.Tx, id string) error {
    tag, err := tx.Exec(ctx, `UPDATE "order" SET status='no_show', no_show_at=now(), updated_at=now() WHERE id=$1 AND status='ready'`, id)
    if err != nil { return err }
    if tag.RowsAffected() == 0 { return order.ErrInvalidTransition }
    return nil
}
func (r *OrderRepo) ListReadyOlderThan(ctx context.Context, threshold time.Time) ([]*order.Order, error) { /* SELECT ... WHERE status='ready' AND ready_at < $1 */ }
func (r *OrderRepo) ListByVendorDay(ctx context.Context, vendorID string, day time.Time, statuses []order.Status) ([]*order.Order, error) { /* used by merchant board */ }
```

**Tests** (extend `order_repo_test.go` + `service_test.go`):
- MarkReadyTx happy + wrong-status conflict
- MarkPickedUpTx happy + wrong-status conflict
- VerifyPickup with valid + invalid TOTP
- 1000-concurrent VerifyPickup: only 1 succeeds, 999 ErrInvalidTransition (no double pickup)

The 1000-racer test:

```go
func TestService_VerifyPickup_NoDoubleAtomic(t *testing.T) {
    // Setup an order in READY status with known TOTP secret
    // 1000 goroutines call VerifyPickup with the same code
    // Assert exactly 1 succeeds, 999 fail with ErrInvalidTransition
}
```

**Commit**:

```bash
git commit -m "feat(order): MarkReady / VerifyPickup / MarkNoShow + TOTP integration"
```

---

## Task 4：Order huma handlers extensions

**Files**: Modify `services/api/internal/order/http/handlers.go`

Add 3 endpoints:

| Method | Path | Op | Role |
|---|---|---|---|
| POST | `/api/merchant/orders/mark-ready` | markVendorReady | vendor |
| POST | `/api/merchant/orders/{id}/verify-pickup` | verifyPickup | vendor |
| GET  | `/api/employee/orders/{id}/pickup-code` | getPickupCode | employee (owner) |
| GET  | `/api/merchant/orders` | listVendorOrders | vendor (today's by plant) |

`getPickupCode`:

```go
func (a *API) getPickupCode(ctx context.Context, in *orderIDInput) (*pickupCodeOutput, error) {
    u, err := a.requireEmployee(ctx)
    if err != nil { return nil, err }
    o, err := a.Svc.Get(ctx, in.ID, u.ID)
    if err != nil { return nil, mapErr(err) }
    if o.Status != order.StatusReady { return nil, huma.Error409Conflict("order not ready") }
    code := totp.Generate(o.TOTPSecret, time.Now())
    var resp pickupCodeOutput
    resp.Body.Code = code
    resp.Body.OrderID = o.ID
    resp.Body.ExpiresInSeconds = totp.StepSeconds - int(time.Now().Unix() % int64(totp.StepSeconds))
    return &resp, nil
}
```

`verifyPickup` (vendor-side):

```go
func (a *API) verifyPickup(ctx context.Context, in *verifyPickupInput) (*struct{}, error) {
    u, _, err := a.requireVendor(ctx) // see below
    if err != nil { return nil, err }
    if err := a.Svc.VerifyPickup(ctx, in.ID, in.Body.Code, u.ID); err != nil {
        return nil, mapErr(err)  // mapErr also handles ErrInvalidPickupCode → 409
    }
    return &struct{}{}, nil
}

func (a *API) requireVendor(ctx context.Context) (*identity.User, string, error) {
    u, ok := idhttp.UserFromContext(ctx)
    if !ok { return nil, "", huma.Error401Unauthorized("not authenticated") }
    if u.Role != identity.RoleVendorOperator { return nil, "", huma.Error403Forbidden("vendor operator required") }
    if u.VendorID == nil { return nil, "", huma.Error403Forbidden("user not bound to vendor") }
    return u, *u.VendorID, nil
}
```

Update mapErr to translate `ErrInvalidPickupCode` → 409.

`markVendorReady` accepts `{order_ids: [...]}` body and calls `s.Svc.MarkReady(ctx, vendorID, ids, u.ID)`.

`listVendorOrders`: query `?date=YYYY-MM-DD&plant=...&status=ready|cutoff|placed`; default today + all statuses.

**Commit**:

```bash
git commit -m "feat(order): pickup-code / verify-pickup / mark-ready / merchant orders endpoints"
```

---

## Task 5：Scheduler — no-show sweep

**Files**: Modify `services/api/internal/order/scheduler/cutoff.go` OR add a new file `no_show.go`.

Add `NoShowSweep` struct with same pattern as `Cutoff`:

```go
type NoShowSweep struct {
    Svc      *order.Service
    Interval time.Duration
    MaxAge   time.Duration  // e.g. 2 hours after ready_at
    Logger   *slog.Logger
}
func (n *NoShowSweep) Run(ctx context.Context) error { /* tick + svc.MarkNoShow(ctx, n.MaxAge) */ }
```

Wire into `case config.RoleScheduler` so the scheduler runs **both** Cutoff and NoShowSweep concurrently via `errgroup`.

**Commit**:

```bash
git commit -m "feat(scheduler): no-show sweep transitions ready → no_show after MAX_AGE"
```

---

## Task 6：Merchant 「備餐看板」 UI

**Files**: Create `apps/merchant/src/routes/orders/+page.{svelte,server.ts}`

- Server load: GET `/api/merchant/orders?date=today` (group by plant client-side; or backend pre-groups via aggregation endpoint — defer the aggregation API and group client-side for P4 simplicity)
- UI: 一個廠區一張卡，列出該廠區今日 cutoff/ready/picked_up 訂單
- Each order row has a checkbox; bottom "標記已備餐完成" button calls `markVendorReady` with selected IDs
- Each ready order row shows a "掃描核銷" button → opens scan modal (P4 simplification: a text input that pastes the 6-digit code)
- Auto-refresh every 15s via `setInterval` in `<script>` calling `invalidate("/")` or `goto(...)` 

**Commit**:

```bash
git commit -m "feat(merchant): 備餐看板 with plant-grouped orders and pickup scan"
```

---

## Task 7：Employee pickup QR page

**Files**: 
- Create `apps/employee/src/routes/orders/[id]/pickup/+page.{svelte,server.ts}`
- Add `qrcode` dependency: `pnpm --filter @tbite/employee add qrcode @types/qrcode`

Server load: GET `/api/employee/orders/{id}/pickup-code`; redirect to `/orders/{id}` if order status != ready.

UI: large QR code (encoded with `tbite://pickup?order=...&code=...`), centered 6-digit code below, countdown timer "剩餘 N 秒"; refresh every 10s via SvelteKit `invalidate`.

```svelte
<script lang="ts">
  import QRCode from "qrcode";
  import { onMount } from "svelte";
  import { invalidateAll } from "$app/navigation";

  let { data } = $props();
  let qrDataURL = $state("");
  let secondsLeft = $state(data.code.expires_in_seconds);

  onMount(() => {
    refresh();
    const tick = setInterval(() => {
      secondsLeft = secondsLeft - 1;
      if (secondsLeft <= 0) { invalidateAll(); secondsLeft = 30; refresh(); }
    }, 1000);
    return () => clearInterval(tick);
  });

  async function refresh() {
    qrDataURL = await QRCode.toDataURL(`tbite://pickup?order=${data.code.order_id}&code=${data.code.code}`, {
      width: 280, margin: 2, color: { dark: "#0f172a", light: "#ffffff" },
    });
  }
  $effect(() => { refresh(); });
</script>

<section class="max-w-md mx-auto space-y-4 text-center">
  <h1 class="text-2xl font-black">領餐核銷</h1>
  <img src={qrDataURL} alt="pickup QR" class="mx-auto rounded-tb-2xl border border-tb-slate-200" />
  <p class="font-jetbrains-mono text-5xl font-black tabular-nums">{data.code.code}</p>
  <p class="text-sm text-tb-slate-500">剩餘 {secondsLeft} 秒 · 30 秒自動換</p>
  <p class="text-xs uppercase tracking-eyebrow text-tb-slate-500">於領餐區出示此 QR Code 與動態碼</p>
</section>
```

Add a 「出示領餐碼」 button on the order detail page (`/orders/[id]`) that links to `/orders/[id]/pickup` when status == ready.

**Commit**:

```bash
git commit -m "feat(employee): pickup QR page with 30s countdown + auto-refresh"
```

---

## Task 8：Concurrency proof — 1000-racer p95 < 100ms

**Files**: `services/api/internal/order/perf/pickup_perf_test.go` (build-tag `perf`)

```go
//go:build perf
package perf_test

// Spin Postgres testcontainer. Seed:
//   - 1 vendor, 1 plant, 1 menu_item with high capacity, 1000 separate orders all in READY state
// Run 1000 goroutines each calling VerifyPickup on a DIFFERENT order with that order's correct code.
// All 1000 must succeed (no contention beyond row-level locks).
// Assert: p50 < 30ms, p95 < 100ms, p99 < 200ms.
```

This isn't the "single-row-1000-racer" — the design doc test is exactly P3 §4.5's quota proof (already done in P2). For pickup the realistic concurrency pattern is 1000 different orders being verified by 1000 different employees — measure p95 latency.

Build-tag-gated so CI doesn't run by default (run via `go test -tags=perf`).

**Commit**:

```bash
git commit -m "test(order): perf gate for pickup verification (1000 orders, p95<100ms)"
```

---

## Task 9：OpenAPI regen + TS client

```bash
make contract-sync
git add contract/openapi packages/api-client/src/schema.d.ts
git commit -m "feat(contract): regen openapi + ts client with pickup endpoints"
```

---

## Task 10：e2e + docs + PR

**Files**: `tests/e2e/order-pickup.spec.ts`

```ts
import { test, expect } from "@playwright/test";

test("employee shows pickup QR after order goes ready", async ({ page }) => {
  // login + place + (admin sets order to ready via DB seed in fixture? OR test only that
  // /pickup page renders 409 when status != ready, AND that pickup page renders correctly
  // when status == ready)
  // P4 e2e simplification: seed an order in READY state via SQL, then login + visit /pickup
  await page.goto("/login");
  await page.getByText("使用 Google 繼續").click();
  await page.waitForURL(/\/$/);
  // (rest of flow depends on seed; refine per actual test fixture)
});
```

Mark README + design doc §15 P4 ✅.

Run full exit-criteria sweep, push, open PR.

**Commit**:

```bash
git commit -m "docs: mark P4 done; e2e pickup spec"
git push origin feat/p4-pickup-totp
gh pr create --title "P4: TOTP pickup + 商家備餐 + ready/picked_up/no_show transitions" \
  --body "..."
```

---

## Exit Criteria

- [ ] Migration up/down/up clean
- [ ] TOTP unit tests pass (4 tests)
- [ ] Order service tests including 1000-racer NoDouble pass
- [ ] pickup-code / verify-pickup / mark-ready endpoints return correct codes
- [ ] Scheduler no-show sweep transitions correctly
- [ ] Merchant 備餐看板 UI loads
- [ ] Employee /pickup page renders QR
- [ ] `make contract-sync` no drift
- [ ] Perf gate (manual): `go test -tags=perf ./...` p95 < 100ms
- [ ] PR opened
