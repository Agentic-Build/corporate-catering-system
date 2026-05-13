# P3 訂單下單流程 + state machine + audit + outbox + cutoff scheduler Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans (or superpowers:subagent-driven-development) to implement this plan task-by-task.

**Goal**：交付完整的員工下單流程 —— 從前端購物車到 Go 後端條件式 quota decrement + order state machine + audit + outbox event 寫入 + NATS publish + scheduler 17:00 自動鎖單。

**Architecture**：order domain 是 P3 的核心 aggregator，協調 quota、menu、identity。State machine 在 Go 程式碼 enforce 合法轉換；每次轉換寫 `order_state_event`（append-only via trigger）和 `outbox_event`（同 transaction）。outbox-relay worker 從 Postgres 拉未送出 event 推 NATS。Scheduler 透過 K8s Lease leader election 每天 17:00 把 PLACED 訂單轉 CUTOFF。

**Tech Stack**：沿用 + NATS JetStream (`github.com/nats-io/nats.go`)。新增：`nats-server` self-host pod in single-node overlay；NATS managed for GCP overlay。

**Branch**：`feat/p3-order-lifecycle`（已切，從 `feat/p2-menu-vendor-quota` tip）

**Scope boundary**：
- P3 **做**：DRAFT→PLACED→CUTOFF→CANCELLED 狀態轉換；下單時呼叫 `quota.Decrement`；取消時呼叫 `quota.Restore`；NATS `orders.v1` stream + 一個 `order.placed.v1` consumer; scheduler 處理 cutoff
- P3 **不**做：READY/PICKED_UP/NO_SHOW/REFUNDED（留 P4 + TOTP pickup）；fulfillment aggregation；商家看板（P4）；payroll batches（P5）

---

## Task 1：Postgres migration — order schema + audit + outbox

**Files**:
- Create: `migrations/000003_order_lifecycle.up.sql`
- Create: `migrations/000003_order_lifecycle.down.sql`

**Step 1**：`000003_order_lifecycle.up.sql`

```sql
CREATE TYPE order_status AS ENUM (
  'draft', 'placed', 'cutoff', 'cancelled', 'ready', 'picked_up', 'no_show', 'refunded'
);

CREATE TABLE "order" (
  id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id         UUID NOT NULL REFERENCES "user"(id) ON DELETE RESTRICT,
  vendor_id       UUID NOT NULL REFERENCES vendor(id) ON DELETE RESTRICT,
  plant           TEXT NOT NULL,
  supply_date     DATE NOT NULL,
  status          order_status NOT NULL DEFAULT 'draft',
  total_price_minor BIGINT NOT NULL CHECK (total_price_minor >= 0),
  placed_at       TIMESTAMPTZ,
  cutoff_at       TIMESTAMPTZ NOT NULL,
  cancelled_at    TIMESTAMPTZ,
  created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX order_user_date_idx ON "order"(user_id, supply_date DESC);
CREATE INDEX order_vendor_date_idx ON "order"(vendor_id, supply_date);
CREATE INDEX order_status_idx ON "order"(status) WHERE status IN ('placed','cutoff','ready');
CREATE INDEX order_pending_cutoff_idx ON "order"(cutoff_at) WHERE status = 'placed';

CREATE TABLE order_item (
  id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  order_id        UUID NOT NULL REFERENCES "order"(id) ON DELETE CASCADE,
  menu_item_id    UUID NOT NULL REFERENCES menu_item(id) ON DELETE RESTRICT,
  qty             INTEGER NOT NULL CHECK (qty > 0),
  unit_price_minor BIGINT NOT NULL CHECK (unit_price_minor >= 0)
);
CREATE INDEX order_item_order_idx ON order_item(order_id);

CREATE TABLE order_state_event (
  id          BIGSERIAL PRIMARY KEY,
  order_id    UUID NOT NULL REFERENCES "order"(id) ON DELETE CASCADE,
  from_state  order_status,
  to_state    order_status NOT NULL,
  actor_id    UUID REFERENCES "user"(id),
  actor_role  user_role,
  reason      TEXT NOT NULL DEFAULT '',
  payload     JSONB NOT NULL DEFAULT '{}'::jsonb,
  at          TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX order_state_event_order_idx ON order_state_event(order_id, at DESC);

-- Append-only guard: no update/delete on order_state_event
CREATE OR REPLACE FUNCTION order_state_event_append_only() RETURNS trigger AS $$
BEGIN
  RAISE EXCEPTION 'order_state_event is append-only (op=%)', TG_OP;
END $$ LANGUAGE plpgsql;
CREATE TRIGGER order_state_event_no_update BEFORE UPDATE ON order_state_event
  FOR EACH ROW EXECUTE FUNCTION order_state_event_append_only();
CREATE TRIGGER order_state_event_no_delete BEFORE DELETE ON order_state_event
  FOR EACH ROW EXECUTE FUNCTION order_state_event_append_only();

CREATE TABLE audit_event (
  id           BIGSERIAL PRIMARY KEY,
  actor_id     UUID REFERENCES "user"(id),
  actor_role   user_role,
  action       TEXT NOT NULL,
  target_kind  TEXT NOT NULL,
  target_id    TEXT NOT NULL,
  payload      JSONB NOT NULL DEFAULT '{}'::jsonb,
  at           TIMESTAMPTZ NOT NULL DEFAULT now(),
  request_id   TEXT NOT NULL DEFAULT ''
);
CREATE INDEX audit_event_target_idx ON audit_event(target_kind, target_id, at DESC);
CREATE INDEX audit_event_actor_idx ON audit_event(actor_id, at DESC) WHERE actor_id IS NOT NULL;

CREATE OR REPLACE FUNCTION audit_event_append_only() RETURNS trigger AS $$
BEGIN
  RAISE EXCEPTION 'audit_event is append-only (op=%)', TG_OP;
END $$ LANGUAGE plpgsql;
CREATE TRIGGER audit_event_no_update BEFORE UPDATE ON audit_event
  FOR EACH ROW EXECUTE FUNCTION audit_event_append_only();
CREATE TRIGGER audit_event_no_delete BEFORE DELETE ON audit_event
  FOR EACH ROW EXECUTE FUNCTION audit_event_append_only();

CREATE TABLE outbox_event (
  id              BIGSERIAL PRIMARY KEY,
  aggregate_type  TEXT NOT NULL,
  aggregate_id    UUID NOT NULL,
  subject         TEXT NOT NULL,
  payload         JSONB NOT NULL,
  headers         JSONB NOT NULL DEFAULT '{}'::jsonb,
  created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
  published_at    TIMESTAMPTZ,
  attempts        INT NOT NULL DEFAULT 0,
  last_error      TEXT
);
CREATE INDEX outbox_unpublished_idx ON outbox_event(id) WHERE published_at IS NULL;
CREATE INDEX outbox_aggregate_idx ON outbox_event(aggregate_type, aggregate_id);
```

**Step 2**：down

```sql
DROP TABLE IF EXISTS outbox_event;
DROP TRIGGER IF EXISTS audit_event_no_delete ON audit_event;
DROP TRIGGER IF EXISTS audit_event_no_update ON audit_event;
DROP TABLE IF EXISTS audit_event;
DROP FUNCTION IF EXISTS audit_event_append_only();
DROP TRIGGER IF EXISTS order_state_event_no_delete ON order_state_event;
DROP TRIGGER IF EXISTS order_state_event_no_update ON order_state_event;
DROP TABLE IF EXISTS order_state_event;
DROP FUNCTION IF EXISTS order_state_event_append_only();
DROP TABLE IF EXISTS order_item;
DROP TABLE IF EXISTS "order";
DROP TYPE IF EXISTS order_status;
```

**Step 3**：local up/down verification (same script pattern as P1/P2).

**Step 4**：commit

```bash
git add migrations
git commit -m "feat(db): order + audit + outbox schema with append-only triggers"
```

---

## Task 2：order domain types + state machine

**Files**:
- Create: `services/api/internal/order/{types,errors,state_machine,repository}.go`
- Create: `services/api/internal/order/state_machine_test.go`

**Domain types**:

```go
package order

import "time"

type Status string

const (
    StatusDraft     Status = "draft"
    StatusPlaced    Status = "placed"
    StatusCutoff    Status = "cutoff"
    StatusCancelled Status = "cancelled"
    StatusReady     Status = "ready"      // reserved, P4
    StatusPickedUp  Status = "picked_up"  // reserved, P4
    StatusNoShow    Status = "no_show"    // reserved, P4
    StatusRefunded  Status = "refunded"   // reserved, P4
)

type Order struct {
    ID              string
    UserID          string
    VendorID        string
    Plant           string
    SupplyDate      time.Time
    Status          Status
    TotalPriceMinor int64
    PlacedAt        *time.Time
    CutoffAt        time.Time
    CancelledAt     *time.Time
    CreatedAt       time.Time
    UpdatedAt       time.Time
    Items           []Item
}

type Item struct {
    ID             string
    OrderID        string
    MenuItemID     string
    Qty            int
    UnitPriceMinor int64
}

type StateEvent struct {
    ID         int64
    OrderID    string
    FromState  *Status
    ToState    Status
    ActorID    *string
    ActorRole  *string
    Reason     string
    Payload    map[string]any
    At         time.Time
}
```

**State machine** (`state_machine.go`):

```go
package order

// allowedTransitions returns the set of valid next states from the given state.
// P3 enforces: draft → placed → (cutoff | cancelled). P4 will extend.
var allowedTransitions = map[Status]map[Status]bool{
    StatusDraft:     {StatusPlaced: true, StatusCancelled: true},
    StatusPlaced:    {StatusCutoff: true, StatusCancelled: true},
    StatusCutoff:    {StatusReady: true, StatusCancelled: true},  // ready / cancelled by admin only
    StatusReady:     {StatusPickedUp: true, StatusNoShow: true},
    StatusPickedUp:  {StatusRefunded: true},
    StatusNoShow:    {StatusRefunded: true},
    StatusCancelled: {},
    StatusRefunded:  {},
}

func CanTransition(from, to Status) bool {
    next, ok := allowedTransitions[from]
    if !ok { return false }
    return next[to]
}
```

**Tests** (`state_machine_test.go`):

```go
package order_test

import (
    "testing"
    "github.com/stretchr/testify/assert"
    "github.com/takalawang/corporate-catering-system/services/api/internal/order"
)

func TestStateMachine_HappyPath(t *testing.T) {
    assert.True(t, order.CanTransition(order.StatusDraft, order.StatusPlaced))
    assert.True(t, order.CanTransition(order.StatusPlaced, order.StatusCutoff))
    assert.True(t, order.CanTransition(order.StatusCutoff, order.StatusReady))
}

func TestStateMachine_Cancel(t *testing.T) {
    assert.True(t, order.CanTransition(order.StatusDraft, order.StatusCancelled))
    assert.True(t, order.CanTransition(order.StatusPlaced, order.StatusCancelled))
    assert.True(t, order.CanTransition(order.StatusCutoff, order.StatusCancelled))
}

func TestStateMachine_Forbidden(t *testing.T) {
    assert.False(t, order.CanTransition(order.StatusPlaced, order.StatusDraft))
    assert.False(t, order.CanTransition(order.StatusCancelled, order.StatusPlaced))
    assert.False(t, order.CanTransition(order.StatusCutoff, order.StatusPlaced))
    assert.False(t, order.CanTransition(order.StatusRefunded, order.StatusPickedUp))
}
```

**Errors** (`errors.go`):

```go
package order

import "errors"

var (
    ErrOrderNotFound      = errors.New("order: not found")
    ErrInvalidTransition  = errors.New("order: invalid state transition")
    ErrCutoffPassed       = errors.New("order: cutoff time has passed")
    ErrEmptyOrder         = errors.New("order: must contain at least one item")
    ErrPlantMismatch      = errors.New("order: plant does not match user")
    ErrVendorPlantMismatch = errors.New("order: vendor does not serve this plant")
    ErrForbidden          = errors.New("order: forbidden")
)
```

**Repository interfaces** (`repository.go`):

```go
package order

import (
    "context"
    "time"
)

type Repository interface {
    Create(ctx context.Context, o *Order) error
    GetByID(ctx context.Context, id string) (*Order, error)
    UpdateStatus(ctx context.Context, id string, from, to Status, actorID *string, actorRole *string, reason string) error
    ListByUser(ctx context.Context, userID string, sinceDate time.Time) ([]*Order, error)
    ListPlacedDueForCutoff(ctx context.Context, before time.Time) ([]*Order, error)
}

type StateEventRepository interface {
    Append(ctx context.Context, ev *StateEvent) error
    ListByOrder(ctx context.Context, orderID string) ([]*StateEvent, error)
}

type AuditRepository interface {
    Write(ctx context.Context, actorID *string, actorRole *string, action, targetKind, targetID string, payload map[string]any, requestID string) error
}

type OutboxRepository interface {
    // Append within an existing transaction (the order's transaction).
    Append(ctx context.Context, tx Tx, aggregateType, aggregateID, subject string, payload map[string]any, headers map[string]any) error
    // Used by relay worker (not service callers).
    LockBatch(ctx context.Context, limit int) ([]*OutboxEvent, Tx, error)
    MarkPublished(ctx context.Context, tx Tx, ids []int64) error
    MarkFailed(ctx context.Context, tx Tx, id int64, lastError string) error
}

type OutboxEvent struct {
    ID             int64
    AggregateType  string
    AggregateID    string
    Subject        string
    Payload        map[string]any
    Headers        map[string]any
    CreatedAt      time.Time
    PublishedAt    *time.Time
    Attempts       int
    LastError      *string
}

// Tx is an opaque database transaction handle used by Append/MarkPublished/MarkFailed.
type Tx interface{}
```

(For simplicity we'll define `Tx = pgx.Tx` in the postgres impl; service callers don't see the concrete type.)

**Commit**:

```bash
git add services/api/internal/order
git commit -m "feat(order): domain types, errors, state machine, repository interfaces"
```

---

## Task 3：order Postgres repository

**Files**:
- Create: `services/api/internal/order/postgres/{order_repo,state_event_repo,audit_repo,outbox_repo}.go` + tests
- Create: `services/api/internal/order/postgres/testhelper_test.go`

Order repo highlights:
- `Create` inserts the order + items in one transaction
- `UpdateStatus` is a conditional UPDATE (`WHERE id=$1 AND status=$2`) returning rows-affected; if 0, return `ErrInvalidTransition`; also inserts a `order_state_event` in the same transaction
- `ListPlacedDueForCutoff` queries `WHERE status='placed' AND cutoff_at <= $1 ORDER BY cutoff_at`

Outbox repo highlights:
- `Append` takes `Tx` to enforce same-transaction insert
- `LockBatch` uses `FOR UPDATE SKIP LOCKED` so multiple relay workers can run safely:
  ```sql
  SELECT id, ... FROM outbox_event
   WHERE published_at IS NULL
   ORDER BY id
   LIMIT $1
   FOR UPDATE SKIP LOCKED
  ```
- `MarkPublished` / `MarkFailed` commit within the same tx returned by LockBatch

Test highlights:
- TDD per repo (5-6 tests)
- Append-only trigger test: try to UPDATE `order_state_event` row → expect Postgres error
- Concurrency test: 2 goroutines each LockBatch, must see disjoint id sets

**Commit per repo** or **one commit covering all 4** (preferred for P3 pace):

```bash
git commit -m "feat(order): postgres repos (order, state_event, audit, outbox) with TDD"
```

---

## Task 4：NATS JetStream wiring + stream provisioning

**Files**:
- Create: `services/api/internal/platform/messaging/nats.go`
- Modify: `services/api/internal/config/config.go` (add NATS_URL — already present from P1 plan; verify or add)
- Modify: `services/api/cmd/tbite/main.go` (api role wires NATS client; worker role provisions streams)

**Design**:

```go
package messaging

import (
    "context"
    "fmt"

    "github.com/nats-io/nats.go"
    "github.com/nats-io/nats.go/jetstream"
)

type Client struct {
    NC *nats.Conn
    JS jetstream.JetStream
}

func New(ctx context.Context, url string) (*Client, error) {
    nc, err := nats.Connect(url, nats.MaxReconnects(-1))
    if err != nil { return nil, fmt.Errorf("nats connect: %w", err) }
    js, err := jetstream.New(nc)
    if err != nil { nc.Close(); return nil, fmt.Errorf("jetstream: %w", err) }
    return &Client{NC: nc, JS: js}, nil
}

func (c *Client) ProvisionStreams(ctx context.Context) error {
    _, err := c.JS.CreateOrUpdateStream(ctx, jetstream.StreamConfig{
        Name:     "ORDERS_V1",
        Subjects: []string{"order.>"},
        Storage:  jetstream.FileStorage,
        Replicas: 1, // 1 for single-node; raise to 3 in HA overlay later
        MaxAge:   30 * 24 * time.Hour,
    })
    return err
}

func (c *Client) Close() { c.NC.Close() }
```

`go get github.com/nats-io/nats.go`.

`main.go` (api role) wires the NATS Client and exposes it to service (for Publish at runtime? — actually publish is done by outbox-relay worker, NOT api). So api role only needs NATS for provisioning at boot (optional — can be done by worker). For simplicity, **let worker role do all NATS work; api role doesn't connect to NATS**.

Implementation: api role doesn't open NATS; only `--role=worker` does. The outbox relay (Task 5) is the only NATS publisher.

**Step 5**：commit

```bash
git add services/api/internal/platform/messaging services/api/internal/config services/api/cmd/tbite
git commit -m "feat(platform): NATS jetstream client + stream provisioning"
```

---

## Task 5：outbox-relay worker

**Files**:
- Create: `services/api/internal/order/relay/relay.go`
- Modify: `services/api/cmd/tbite/main.go` (`case config.RoleWorker` starts the relay loop)

**Design**:

```go
package relay

import (
    "context"
    "encoding/json"
    "log/slog"
    "time"

    "github.com/nats-io/nats.go/jetstream"

    "github.com/takalawang/corporate-catering-system/services/api/internal/order"
)

type Relay struct {
    Outbox order.OutboxRepository
    JS     jetstream.JetStream
    Logger *slog.Logger
    Batch  int
    Sleep  time.Duration
}

func (r *Relay) Run(ctx context.Context) error {
    for {
        select {
        case <-ctx.Done(): return ctx.Err()
        default:
        }
        n, err := r.cycle(ctx)
        if err != nil {
            r.Logger.Error("relay cycle", "err", err)
        }
        if n == 0 {
            select {
            case <-ctx.Done(): return ctx.Err()
            case <-time.After(r.Sleep):
            }
        }
    }
}

func (r *Relay) cycle(ctx context.Context) (int, error) {
    events, tx, err := r.Outbox.LockBatch(ctx, r.Batch)
    if err != nil { return 0, err }
    if len(events) == 0 {
        // No work — release tx (Rollback) and sleep.
        // The repo impl handles tx commit/rollback via MarkPublished.
        return 0, nil
    }
    successIDs := make([]int64, 0, len(events))
    for _, ev := range events {
        payload, _ := json.Marshal(ev.Payload)
        _, err := r.JS.Publish(ctx, ev.Subject, payload)
        if err != nil {
            r.Logger.Warn("publish failed", "event_id", ev.ID, "subject", ev.Subject, "err", err)
            _ = r.Outbox.MarkFailed(ctx, tx, ev.ID, err.Error())
            continue
        }
        successIDs = append(successIDs, ev.ID)
    }
    if len(successIDs) > 0 {
        if err := r.Outbox.MarkPublished(ctx, tx, successIDs); err != nil {
            return len(successIDs), err
        }
    }
    return len(events), nil
}
```

Worker entrypoint in `main.go`:

```go
case config.RoleWorker:
    pool, err := db.NewPool(ctx, cfg.DatabaseRW)
    if err != nil { logger.Error("pg pool", "err", err); os.Exit(1) }
    defer pool.Close()
    nats, err := messaging.New(ctx, cfg.NATSURL)
    if err != nil { logger.Error("nats", "err", err); os.Exit(1) }
    defer nats.Close()
    if err := nats.ProvisionStreams(ctx); err != nil {
        logger.Error("provision", "err", err); os.Exit(1)
    }
    outbox := opgrepo.NewOutboxRepo(pool)
    r := &relay.Relay{Outbox: outbox, JS: nats.JS, Logger: logger, Batch: 100, Sleep: 500 * time.Millisecond}
    if err := r.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
        logger.Error("relay", "err", err); os.Exit(1)
    }
```

Tests:
- Spin up NATS testcontainer (`testcontainers-go/modules/nats`) + Postgres testcontainer
- Insert 5 outbox rows via direct SQL
- Run one `cycle()`
- Assert 5 published_at != nil and 5 messages received on `JS.PullConsumer("ORDERS_V1", "...")`

**Commit**:

```bash
git commit -m "feat(order): outbox-relay worker bridges Postgres → NATS JetStream"
```

---

## Task 6：order service — place / modify / cancel

**Files**:
- Create: `services/api/internal/order/service.go` + `service_test.go`

**Operations**:

```go
type Service struct {
    Orders     Repository
    StateEvents StateEventRepository
    Audit      AuditRepository
    Outbox     OutboxRepository
    Items      menu.ItemRepository
    Plants     vendor.PlantMappingRepository
    Quota      quota.Repository
    Pool       *pgxpool.Pool // for transaction control
    Clock      Clock
}

type PlaceOrderInput struct {
    UserID     string
    Plant      string
    SupplyDate time.Time
    Items      []PlaceItem // {MenuItemID, Qty}
}

type PlaceItem struct{ MenuItemID string; Qty int }

// Place creates an order in PLACED state in a single transaction:
//   1. Validate inputs (items non-empty, items resolvable, quota available)
//   2. For each item: Decrement quota (returns ErrOutOfStock if any one fails)
//   3. Insert order + items
//   4. Insert order_state_event {from=null, to=PLACED}
//   5. Append outbox event order.placed.v1
//   6. Append audit_event
//   7. COMMIT
// If any step fails after decrements, ROLLBACK (Postgres releases the locks; quota
// updates were within tx so they're also rolled back).
func (s *Service) Place(ctx context.Context, in PlaceOrderInput) (*Order, error) {
    // ... full impl
}

func (s *Service) Cancel(ctx context.Context, orderID, userID string) error {
    // Verify ownership, transition to cancelled, Restore quota
}

func (s *Service) ListByUser(ctx context.Context, userID string) ([]*Order, error) {
    return s.Orders.ListByUser(ctx, userID, s.Clock.Now().AddDate(0, 0, -30))
}

func (s *Service) Get(ctx context.Context, id, userID string) (*Order, error) {
    o, err := s.Orders.GetByID(ctx, id)
    if err != nil { return nil, err }
    if o.UserID != userID { return nil, ErrForbidden }
    return o, nil
}
```

The hardest part: Place does many writes in one transaction. The cleanest approach is to **let the Service open a pgx transaction directly** (pass it through repo methods). Add `BeginTx(ctx)` to OrderRepository or have Service own `pool` and pass `pgx.Tx` to repos. The plan suggests Service holds `*pgxpool.Pool` and starts the transaction, passing it to a new `*TxScope` that wraps each repo method.

For P3 simplicity: **add Tx-aware variants** to the repos (`CreateTx(ctx, tx, o)`, etc.) and let Service orchestrate.

Tests:
- `Place_Happy`: orders + items created, quota decremented, state_event + outbox + audit all written
- `Place_OutOfStock`: one item with 0 quota → returns `ErrOutOfStock`, NO row inserted anywhere
- `Place_EmptyItems`: → `ErrEmptyOrder`
- `Place_PlantNotServedByVendor`: → `ErrVendorPlantMismatch`
- `Cancel_OwnPlaced`: state goes to cancelled, quota restored, audit/outbox written
- `Cancel_NotOwner`: → `ErrForbidden`
- `Cancel_Cutoff`: cutoff orders can't be cancelled by user (Service returns `ErrInvalidTransition` for non-admin)

**Commit**:

```bash
git commit -m "feat(order): service.Place / Cancel with quota integration + outbox + audit"
```

---

## Task 7：order huma handlers

**Files**:
- Create: `services/api/internal/order/http/handlers.go` + test

**Endpoints**:

| Method | Path | Op |
|---|---|---|
| POST   | `/api/employee/orders` | placeOrder |
| GET    | `/api/employee/orders` | listMyOrders |
| GET    | `/api/employee/orders/{id}` | getMyOrder |
| POST   | `/api/employee/orders/{id}/cancel` | cancelMyOrder |

**DTOs**: standard mapping from `Order` + items to JSON. Map errors:
- `ErrOutOfStock` → 409
- `ErrInvalidTransition` / `ErrCutoffPassed` → 409
- `ErrEmptyOrder` / `ErrVendorPlantMismatch` → 400
- `ErrOrderNotFound` → 404
- `ErrForbidden` → 403

**Wire in main.go + contract-export**

**Commit**:

```bash
git commit -m "feat(order): huma handlers for employee place/list/get/cancel"
```

---

## Task 8：cutoff scheduler

**Files**:
- Create: `services/api/internal/order/scheduler/cutoff.go`
- Modify: `services/api/cmd/tbite/main.go` (`case config.RoleScheduler`)
- Create: `services/api/internal/platform/leader/lease.go` (K8s Lease-based leader election; for local-only single-replica deployment, a no-op lease)

**Design**:

The scheduler runs periodically (e.g. every 60s) and:
1. Acquires leader Lease (or no-op if running locally)
2. Calls `Service.RunCutoff(ctx, now)`:
   - `ListPlacedDueForCutoff(now)` returns orders past cutoff
   - For each: transition placed → cutoff, append state_event, audit, outbox (`order.cutoff.v1`)
   - Done within a per-order transaction (small scope; doesn't block whole batch)

For P3, **leader election is a no-op** (single scheduler replica). K8s Lease integration is documented as future work.

Tests:
- Integration test: seed 3 orders (1 past cutoff, 2 future) → run RunCutoff → assert only 1 transitioned

**Commit**:

```bash
git commit -m "feat(scheduler): cutoff job transitions placed → cutoff after cutoff_at"
```

---

## Task 9：Employee cart UI + place order flow

**Files**:
- Modify: `apps/employee/src/routes/+page.svelte` — cart submission action
- Modify: `apps/employee/src/routes/+page.server.ts` — add form action that POSTs to `/api/employee/orders`
- Create: `apps/employee/src/routes/orders/+page.{svelte,server.ts}` — list my orders
- Create: `apps/employee/src/routes/orders/[id]/+page.{svelte,server.ts}` — order detail with cancel button
- Update: `+layout.svelte` to add a 「我的訂單」 link

**Place flow** (form action on `/`):
- User clicks "送出預訂"
- SvelteKit collects cart state into FormData (`item_id[i]`, `qty[i]`, `plant`, `supply_date`)
- Server load posts to `/api/employee/orders`
- On success: redirect to `/orders/[new-id]`
- On failure: show error in flash

**Orders list**: show date / vendor / total / status; click → detail.

**Order detail**: show items, plant, cutoff_at countdown, cancel button if `status === "placed"`.

**Commit**:

```bash
git commit -m "feat(employee): cart submission + orders list + order detail UI"
```

---

## Task 10：OpenAPI regen + TS client

```bash
make contract-sync
git add contract/openapi packages/api-client/src/schema.d.ts
git commit -m "feat(contract): regen openapi + ts client with order endpoints"
```

---

## Task 11：Wire NATS into single-node + GCP overlays

**Files**:
- Modify: `ops/kubernetes/overlays/single-node/nats.yaml` — already deploys NATS (P0 single-node overlay has it); verify it's there and exposes service `nats:4222`
- Modify: `ops/kubernetes/base/deployment-worker.yaml` — ensure `NATS_URL` env var is wired

If `nats.yaml` doesn't exist in overlays/single-node yet, add it. Similarly for GCP (`overlays/gcp/nats.yaml` from P0 design).

**Commit**:

```bash
git commit -m "ops: ensure NATS jetstream available in single-node + gcp overlays"
```

---

## Task 12：e2e + docs + PR

**Playwright spec** (`tests/e2e/order-place.spec.ts`):

```ts
import { test, expect } from "@playwright/test";

test("employee places an order from seeded menu", async ({ page }) => {
  await page.goto("/login");
  await page.getByText("使用 Google 繼續").click();
  await page.waitForURL(/\/$/);

  // Add first seeded item to cart
  const firstCard = page.locator("article").first();
  await firstCard.locator("button[aria-label=增加]").click();

  // Submit (assumes a "送出預訂" button is present when cart count > 0)
  await page.getByRole("button", { name: /送出預訂/ }).click();
  await page.waitForURL(/\/orders\//, { timeout: 10_000 });
  await expect(page.getByText(/已送出/)).toBeVisible();
});
```

Adjust selectors based on actual UI. If a submit button isn't yet implemented, add it in Task 9 first.

**Docs**: update README + design doc §15 with P2 → P3 ✅.

**Final exit criteria sweep**:

Same as previous phases: migrations / go test (incl. quota concurrency + outbox concurrency) / pnpm check+build / contract drift / overlays render / Playwright.

**Push + PR**:

```bash
git push origin feat/p3-order-lifecycle
gh pr create --title "P3: order lifecycle + audit + outbox + cutoff scheduler" --body "$(cat <<'EOF'
## Summary
- Schema 第三波: order, order_item, order_state_event (append-only), audit_event (append-only), outbox_event
- Go modules: order (domain + state machine + repos + service + http) + scheduler/cutoff + relay
- Place / Cancel / List operations with conditional quota.Decrement + Restore
- All state transitions write order_state_event + audit_event + outbox_event in a single transaction
- NATS JetStream `ORDERS_V1` stream provisioned; outbox-relay worker bridges DB → NATS
- Cutoff scheduler transitions placed → cutoff at cutoff_at
- Employee cart submit + /orders + /orders/[id] UI with cancel
- OpenAPI + TS client regenerated
- e2e: employee places order from seeded menu

## What's deferred to P4
- READY / PICKED_UP / NO_SHOW / REFUNDED transitions (商家 + TOTP)
- Vendor 「備餐 ready」 endpoint
- TOTP pickup verification
- Fulfillment aggregation views

🤖 Generated with [Claude Code](https://claude.com/claude-code)
EOF
)"
```

---

## Exit Criteria

- [ ] All migrations up/down/up clean
- [ ] State machine tests pass
- [ ] order repos + service tests pass (incl. transactional happy + rollback on quota fail)
- [ ] Outbox relay integration test passes (events reach NATS)
- [ ] Scheduler integration test passes (cutoff transitions correctly)
- [ ] Frontend check + build pass
- [ ] Contract drift gate green
- [ ] Playwright order-place spec passes (employee places + sees status)
- [ ] PR opened
