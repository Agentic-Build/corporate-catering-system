# P2 menu / vendor / quota + 員工瀏覽今日菜單 Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans (or superpowers:subagent-driven-development) to implement this plan task-by-task.

**Goal**：建立商家入駐、菜單管理、每日供應量（quota）三個核心模組，讓員工可瀏覽今日可送達菜單，並用 Postgres 條件式 UPDATE 確保 quota 扣減的正確性；同步交付對應的 SvelteKit UI（merchant CRUD、admin 審核、employee 瀏覽）。

**Architecture**：沿用 P1 的 ports/adapters 模式：每個 domain 模組（vendor / menu / quota）各自有 `types.go`、`errors.go`、`repository.go`（介面）、`postgres/*.go`（實作）、`service.go`（業務）、`http/*.go`（huma handlers）。Quota 扣減 source of truth = Postgres `meal_supply` 上的條件式 UPDATE — TDD 用 `t.Parallel()` 起 N goroutine 證明不超賣。Redis 只當 read-through cache 給「顯示用剩餘」加速。

**Tech Stack**：沿用 P1（Go 1.23 + huma + pgx + redis + chi + golang-migrate + testcontainers + Svelte 5 + SvelteKit 2 + Tailwind + Playwright）。新增：無新依賴；只新增 Go domain 模組與 Svelte 元件。

**Branch**：`feat/p2-menu-vendor-quota`（已切，從 `feat/p1-identity` tip）

**Scope boundary**：
- P2 **不**做：員工真正下單（會建立 order/order_state_event — 留 P3）、TOTP 核銷、月結、配送輸出、商家自助申請 UI（admin 直接 approve 種子商家）
- P2 **做**：vendor 入駐 / 廠區映射、menu CRUD、meal_supply CRUD、employee 菜單瀏覽、商家邀請碼 admin 端產生流程（完成 P1 留下的缺口）、4 個 packages/ui 元件移植

---

## Task 0：P2 setup — 分支確認

**Files**: none yet — just verify start state.

**Step 1**：

```bash
git status                            # clean
git log --oneline -3                  # latest = 06d50d1 docs P0/P1 done
git rev-parse --abbrev-ref HEAD       # feat/p2-menu-vendor-quota
```

無新依賴需要在此 task 加。後續 task 各自 `go get` 缺的 deps。

**Step 2**：建立 `docs/plans/` 中的本 plan 已是 in-progress 狀態，此處不 commit code change；直接進 Task 1。

---

## Task 1：Postgres migration — vendor + menu + supply

**Files**:
- Create: `migrations/000002_menu_vendor_quota.up.sql`
- Create: `migrations/000002_menu_vendor_quota.down.sql`

**Step 1**：撰寫 up migration

```sql
-- 000002_menu_vendor_quota.up.sql

CREATE TYPE vendor_status AS ENUM ('pending', 'approved', 'suspended', 'terminated');
CREATE TYPE menu_item_status AS ENUM ('draft', 'active', 'archived');

CREATE TABLE vendor (
  id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  display_name TEXT NOT NULL,
  legal_name   TEXT NOT NULL,
  contact_email TEXT NOT NULL,
  status       vendor_status NOT NULL DEFAULT 'pending',
  approved_at  TIMESTAMPTZ,
  approved_by  UUID REFERENCES "user"(id),
  created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
  CONSTRAINT vendor_contact_email_lower CHECK (contact_email = lower(contact_email))
);
CREATE UNIQUE INDEX vendor_contact_email_idx ON vendor(contact_email);
CREATE INDEX vendor_status_idx ON vendor(status);

-- 商家 × 廠區映射（哪些廠區可被該商家服務）
CREATE TABLE vendor_plant_mapping (
  id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  vendor_id  UUID NOT NULL REFERENCES vendor(id) ON DELETE CASCADE,
  plant      TEXT NOT NULL,           -- e.g. "F12B-3F"
  active     BOOLEAN NOT NULL DEFAULT true,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX vendor_plant_unique_idx ON vendor_plant_mapping(vendor_id, plant);
CREATE INDEX vendor_plant_active_idx ON vendor_plant_mapping(plant) WHERE active;

-- vendor 的 invite code（P1 schema 已建 vendor_invite，這裡只補 FK）
ALTER TABLE vendor_invite
  ADD CONSTRAINT vendor_invite_vendor_fk
  FOREIGN KEY (vendor_id) REFERENCES vendor(id) ON DELETE CASCADE;

-- 把 user.vendor_id 也補上 FK
ALTER TABLE "user"
  ADD CONSTRAINT user_vendor_fk
  FOREIGN KEY (vendor_id) REFERENCES vendor(id) ON DELETE SET NULL;

CREATE TABLE menu_category (
  id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  vendor_id  UUID NOT NULL REFERENCES vendor(id) ON DELETE CASCADE,
  name       TEXT NOT NULL,
  sort_order INTEGER NOT NULL DEFAULT 0,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX menu_category_vendor_idx ON menu_category(vendor_id, sort_order);

CREATE TABLE menu_item (
  id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  vendor_id   UUID NOT NULL REFERENCES vendor(id) ON DELETE CASCADE,
  category_id UUID REFERENCES menu_category(id) ON DELETE SET NULL,
  name        TEXT NOT NULL,
  description TEXT NOT NULL DEFAULT '',
  price_minor BIGINT NOT NULL CHECK (price_minor >= 0),
  tags        TEXT[] NOT NULL DEFAULT '{}',
  badges      TEXT[] NOT NULL DEFAULT '{}',
  status      menu_item_status NOT NULL DEFAULT 'draft',
  archived_at TIMESTAMPTZ,
  created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX menu_item_vendor_idx ON menu_item(vendor_id) WHERE status != 'archived';
CREATE INDEX menu_item_status_idx ON menu_item(status);

CREATE TABLE menu_item_image (
  id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  menu_item_id UUID NOT NULL REFERENCES menu_item(id) ON DELETE CASCADE,
  blob_uri     TEXT NOT NULL,           -- s3://bucket/path or https://cdn/...
  alt          TEXT NOT NULL DEFAULT '',
  sort_order   INTEGER NOT NULL DEFAULT 0,
  created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX menu_item_image_item_idx ON menu_item_image(menu_item_id, sort_order);

CREATE TABLE meal_supply (
  id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  menu_item_id    UUID NOT NULL REFERENCES menu_item(id) ON DELETE CASCADE,
  supply_date     DATE NOT NULL,
  capacity        INTEGER NOT NULL CHECK (capacity >= 0),
  remain          INTEGER NOT NULL CHECK (remain >= 0),
  pickup_window   TEXT NOT NULL DEFAULT '',  -- e.g. "11:50-12:10"
  eta_label       TEXT NOT NULL DEFAULT '',  -- "11:50-12:10"
  cutoff_at       TIMESTAMPTZ NOT NULL,
  created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
  CONSTRAINT meal_supply_remain_le_capacity CHECK (remain <= capacity)
);
CREATE UNIQUE INDEX meal_supply_item_date_idx ON meal_supply(menu_item_id, supply_date);
CREATE INDEX meal_supply_date_idx ON meal_supply(supply_date);

COMMENT ON TABLE vendor IS 'External catering vendors approved by welfare admin.';
COMMENT ON TABLE vendor_plant_mapping IS 'Which plant areas a vendor is allowed to serve.';
COMMENT ON TABLE menu_item IS 'Vendor menu items (catalog rows).';
COMMENT ON TABLE meal_supply IS 'Daily capacity + remaining count per menu_item, the quota source of truth.';
```

**Step 2**：down migration

```sql
ALTER TABLE "user" DROP CONSTRAINT IF EXISTS user_vendor_fk;
ALTER TABLE vendor_invite DROP CONSTRAINT IF EXISTS vendor_invite_vendor_fk;
DROP TABLE IF EXISTS meal_supply;
DROP TABLE IF EXISTS menu_item_image;
DROP TABLE IF EXISTS menu_item;
DROP TABLE IF EXISTS menu_category;
DROP TABLE IF EXISTS vendor_plant_mapping;
DROP TABLE IF EXISTS vendor;
DROP TYPE IF EXISTS menu_item_status;
DROP TYPE IF EXISTS vendor_status;
```

**Step 3**：local up/down 驗證（Docker pg + migrate.sh）

```bash
docker run -d --name p2-pg -e POSTGRES_USER=tbite -e POSTGRES_PASSWORD=tbite -e POSTGRES_DB=tbite -p 55432:5432 postgres:16-alpine
sleep 5
DATABASE_URL="postgres://tbite:tbite@localhost:55432/tbite?sslmode=disable" scripts/db/migrate.sh up
docker exec p2-pg psql -U tbite -d tbite -c "\dt"
DATABASE_URL="postgres://tbite:tbite@localhost:55432/tbite?sslmode=disable" scripts/db/migrate.sh down 1
docker exec p2-pg psql -U tbite -d tbite -c "\dt"  # 應只剩 P1 表 + schema_migrations
DATABASE_URL="postgres://tbite:tbite@localhost:55432/tbite?sslmode=disable" scripts/db/migrate.sh up  # 再 up
docker rm -f p2-pg
```

**Step 4**：commit

```bash
git add migrations
git commit -m "feat(db): menu/vendor/quota schema (vendor, menu_item, meal_supply, plant mapping)"
```

---

## Task 2：vendor domain + Postgres repos

**Files**:
- Create: `services/api/internal/vendor/types.go`
- Create: `services/api/internal/vendor/errors.go`
- Create: `services/api/internal/vendor/repository.go`
- Create: `services/api/internal/vendor/postgres/{vendor_repo,plant_mapping_repo}.go` + tests
- Create: `services/api/internal/vendor/postgres/testhelper_test.go`（共用 setupPostgres + migrationsDir）

**Step 1**：domain types

```go
package vendor

import "time"

type Status string

const (
    StatusPending    Status = "pending"
    StatusApproved   Status = "approved"
    StatusSuspended  Status = "suspended"
    StatusTerminated Status = "terminated"
)

type Vendor struct {
    ID           string
    DisplayName  string
    LegalName    string
    ContactEmail string
    Status       Status
    ApprovedAt   *time.Time
    ApprovedBy   *string
    CreatedAt    time.Time
    UpdatedAt    time.Time
}

type PlantMapping struct {
    ID        string
    VendorID  string
    Plant     string
    Active    bool
    CreatedAt time.Time
}
```

**Step 2**：errors

```go
package vendor

import "errors"

var (
    ErrVendorNotFound  = errors.New("vendor: not found")
    ErrAlreadyApproved = errors.New("vendor: already approved")
    ErrInvalidStatus   = errors.New("vendor: invalid status transition")
)
```

**Step 3**：repo interfaces

```go
package vendor

import "context"

type Repository interface {
    GetByID(ctx context.Context, id string) (*Vendor, error)
    GetByEmail(ctx context.Context, email string) (*Vendor, error)
    Create(ctx context.Context, v *Vendor) error
    UpdateStatus(ctx context.Context, id string, status Status, approvedBy *string) error
    List(ctx context.Context, statuses []Status) ([]*Vendor, error)
}

type PlantMappingRepository interface {
    ListByVendor(ctx context.Context, vendorID string) ([]*PlantMapping, error)
    ListVendorsForPlant(ctx context.Context, plant string) ([]string, error)
    Set(ctx context.Context, vendorID string, plants []string) error // replaces all mappings for the vendor
}
```

**Step 4**：testhelper_test.go (sibling of P1 testhelper — same pattern, just package vendor_postgres_test)

可以從 `services/api/internal/identity/postgres/testhelper_test.go` 複製，調整 import 路徑 / package name。

**Step 5–6**：TDD VendorRepo

`services/api/internal/vendor/postgres/vendor_repo_test.go`（red）:

```go
package postgres_test

import (
    "context"
    "testing"

    "github.com/stretchr/testify/assert"
    "github.com/stretchr/testify/require"

    "github.com/takalawang/corporate-catering-system/services/api/internal/vendor"
    "github.com/takalawang/corporate-catering-system/services/api/internal/vendor/postgres"
)

func TestVendorRepo_CreateAndGet(t *testing.T) {
    pool, cleanup := setupPostgres(t)
    defer cleanup()
    repo := postgres.NewVendorRepo(pool)
    ctx := context.Background()

    v := &vendor.Vendor{
        DisplayName:  "稻禾家便當",
        LegalName:    "稻禾家便當有限公司",
        ContactEmail: "ops@daohe.tw",
        Status:       vendor.StatusPending,
    }
    require.NoError(t, repo.Create(ctx, v))
    require.NotEmpty(t, v.ID)

    got, err := repo.GetByID(ctx, v.ID)
    require.NoError(t, err)
    assert.Equal(t, "稻禾家便當", got.DisplayName)

    got2, err := repo.GetByEmail(ctx, "ops@daohe.tw")
    require.NoError(t, err)
    assert.Equal(t, v.ID, got2.ID)
}

func TestVendorRepo_NotFound(t *testing.T) {
    pool, cleanup := setupPostgres(t)
    defer cleanup()
    repo := postgres.NewVendorRepo(pool)
    _, err := repo.GetByID(context.Background(), "00000000-0000-0000-0000-000000000000")
    assert.ErrorIs(t, err, vendor.ErrVendorNotFound)
}

func TestVendorRepo_UpdateStatusAndList(t *testing.T) {
    pool, cleanup := setupPostgres(t)
    defer cleanup()
    repo := postgres.NewVendorRepo(pool)
    ctx := context.Background()
    a := &vendor.Vendor{DisplayName:"A",LegalName:"A Ltd",ContactEmail:"a@x.com",Status:vendor.StatusPending}
    b := &vendor.Vendor{DisplayName:"B",LegalName:"B Ltd",ContactEmail:"b@x.com",Status:vendor.StatusPending}
    _ = repo.Create(ctx, a)
    _ = repo.Create(ctx, b)
    approvedBy := "admin-1"
    require.NoError(t, repo.UpdateStatus(ctx, a.ID, vendor.StatusApproved, &approvedBy))

    approved, err := repo.List(ctx, []vendor.Status{vendor.StatusApproved})
    require.NoError(t, err)
    assert.Len(t, approved, 1)
    assert.Equal(t, "A", approved[0].DisplayName)
    assert.NotNil(t, approved[0].ApprovedAt)
}
```

Step: red → fail, implement `vendor_repo.go` (same shape as P1 user_repo: pgx, sentinel via pgx.ErrNoRows), green。

`vendor_repo.go`:

```go
package postgres

import (
    "context"
    "errors"
    "fmt"

    "github.com/jackc/pgx/v5"
    "github.com/jackc/pgx/v5/pgxpool"

    "github.com/takalawang/corporate-catering-system/services/api/internal/vendor"
)

type VendorRepo struct{ pool *pgxpool.Pool }

func NewVendorRepo(p *pgxpool.Pool) *VendorRepo { return &VendorRepo{pool: p} }

func (r *VendorRepo) Create(ctx context.Context, v *vendor.Vendor) error {
    return r.pool.QueryRow(ctx, `
INSERT INTO vendor (display_name, legal_name, contact_email, status)
VALUES ($1,$2,$3,$4)
RETURNING id, created_at, updated_at`,
        v.DisplayName, v.LegalName, v.ContactEmail, string(v.Status),
    ).Scan(&v.ID, &v.CreatedAt, &v.UpdatedAt)
}

func (r *VendorRepo) GetByID(ctx context.Context, id string) (*vendor.Vendor, error) {
    return r.one(ctx, `WHERE id = $1`, id)
}

func (r *VendorRepo) GetByEmail(ctx context.Context, email string) (*vendor.Vendor, error) {
    return r.one(ctx, `WHERE contact_email = $1`, email)
}

func (r *VendorRepo) one(ctx context.Context, where string, args ...any) (*vendor.Vendor, error) {
    var v vendor.Vendor
    var status string
    q := `SELECT id, display_name, legal_name, contact_email, status, approved_at, approved_by, created_at, updated_at FROM vendor ` + where
    err := r.pool.QueryRow(ctx, q, args...).Scan(
        &v.ID, &v.DisplayName, &v.LegalName, &v.ContactEmail, &status,
        &v.ApprovedAt, &v.ApprovedBy, &v.CreatedAt, &v.UpdatedAt,
    )
    if errors.Is(err, pgx.ErrNoRows) {
        return nil, vendor.ErrVendorNotFound
    }
    if err != nil {
        return nil, fmt.Errorf("vendor scan: %w", err)
    }
    v.Status = vendor.Status(status)
    return &v, nil
}

func (r *VendorRepo) UpdateStatus(ctx context.Context, id string, status vendor.Status, approvedBy *string) error {
    if status == vendor.StatusApproved {
        _, err := r.pool.Exec(ctx, `UPDATE vendor SET status=$2, approved_at=now(), approved_by=$3, updated_at=now() WHERE id=$1`, id, string(status), approvedBy)
        return err
    }
    _, err := r.pool.Exec(ctx, `UPDATE vendor SET status=$2, updated_at=now() WHERE id=$1`, id, string(status))
    return err
}

func (r *VendorRepo) List(ctx context.Context, statuses []vendor.Status) ([]*vendor.Vendor, error) {
    args := []any{}
    where := ""
    if len(statuses) > 0 {
        in := make([]string, len(statuses))
        for i, s := range statuses {
            args = append(args, string(s))
            in[i] = fmt.Sprintf("$%d", i+1)
        }
        where = "WHERE status IN (" + joinStrings(in, ",") + ")"
    }
    q := `SELECT id, display_name, legal_name, contact_email, status, approved_at, approved_by, created_at, updated_at FROM vendor ` + where + ` ORDER BY created_at DESC`
    rows, err := r.pool.Query(ctx, q, args...)
    if err != nil { return nil, err }
    defer rows.Close()
    var out []*vendor.Vendor
    for rows.Next() {
        var v vendor.Vendor
        var status string
        if err := rows.Scan(&v.ID, &v.DisplayName, &v.LegalName, &v.ContactEmail, &status,
            &v.ApprovedAt, &v.ApprovedBy, &v.CreatedAt, &v.UpdatedAt); err != nil { return nil, err }
        v.Status = vendor.Status(status)
        out = append(out, &v)
    }
    return out, rows.Err()
}

func joinStrings(s []string, sep string) string {
    out := ""
    for i, x := range s {
        if i > 0 { out += sep }
        out += x
    }
    return out
}
```

(可改用 `strings.Join` 並 import；上面 `joinStrings` 是為避免新 import；implementer 自由選擇)

**Step 7**：PlantMappingRepo TDD（同 pattern）

Test:
```go
func TestPlantMappingRepo_SetAndList(t *testing.T) {
    pool, cleanup := setupPostgres(t)
    defer cleanup()
    vrepo := postgres.NewVendorRepo(pool)
    prepo := postgres.NewPlantMappingRepo(pool)
    ctx := context.Background()
    v := &vendor.Vendor{DisplayName:"V",LegalName:"V Ltd",ContactEmail:"v@x.com",Status:vendor.StatusApproved}
    _ = vrepo.Create(ctx, v)
    require.NoError(t, prepo.Set(ctx, v.ID, []string{"F12B-3F","F15-2F"}))

    list, err := prepo.ListByVendor(ctx, v.ID)
    require.NoError(t, err)
    assert.Len(t, list, 2)

    vs, err := prepo.ListVendorsForPlant(ctx, "F12B-3F")
    require.NoError(t, err)
    assert.Contains(t, vs, v.ID)

    // Re-set replaces
    require.NoError(t, prepo.Set(ctx, v.ID, []string{"F18-RF"}))
    list, _ = prepo.ListByVendor(ctx, v.ID)
    assert.Len(t, list, 1)
    assert.Equal(t, "F18-RF", list[0].Plant)
}
```

Impl `plant_mapping_repo.go`：`Set` 用 transaction：DELETE WHERE vendor_id=$1, INSERT ... 多 rows。

**Step 8**：commit

```bash
git add services/api/internal/vendor
git commit -m "feat(vendor): domain + postgres repos (TDD with testcontainers)"
```

---

## Task 3：vendor service + huma handlers

**Files**:
- Create: `services/api/internal/vendor/service.go`
- Create: `services/api/internal/vendor/service_test.go`（mock-based）
- Create: `services/api/internal/vendor/http/handlers.go`
- Create: `services/api/internal/vendor/http/handlers_test.go`

**Service responsibilities**：
- `CreatePending(ctx, displayName, legalName, contactEmail)` — admin 建立 pending vendor + 自動發 invite code
- `Approve(ctx, vendorID, adminUserID, plants []string)` — 設 status=approved + 設廠區
- `Suspend(ctx, vendorID)` / `Reinstate(ctx, vendorID)`
- `List(ctx, statuses)` 
- `ListPlantsFor(ctx, vendorID)`
- `IssueInvite(ctx, vendorID, ttl)` — 產生新 invite code（用 `vendor_invite` 表，沿用 P1 schema）

State machine 規則：
- `pending → approved`：合法
- `approved → suspended → approved`：合法（reinstate）
- `approved → terminated`：合法
- 任何其他轉換 → `ErrInvalidStatus`

**Step 1**：service.go

```go
package vendor

import (
    "context"
    "crypto/rand"
    "encoding/base64"
    "fmt"
    "time"

    "github.com/takalawang/corporate-catering-system/services/api/internal/identity"
)

type InviteRepository = identity.VendorInviteRepository

type Service struct {
    Vendors     Repository
    Plants      PlantMappingRepository
    Invites     InviteRepository
    Clock       Clock
    InviteTTL   time.Duration
}

type Clock interface{ Now() time.Time }

func (s *Service) CreatePending(ctx context.Context, displayName, legalName, email string) (*Vendor, error) {
    v := &Vendor{DisplayName: displayName, LegalName: legalName, ContactEmail: email, Status: StatusPending}
    if err := s.Vendors.Create(ctx, v); err != nil { return nil, err }
    return v, nil
}

func (s *Service) Approve(ctx context.Context, id string, adminID string, plants []string) error {
    v, err := s.Vendors.GetByID(ctx, id)
    if err != nil { return err }
    if v.Status == StatusApproved { return ErrAlreadyApproved }
    if v.Status != StatusPending && v.Status != StatusSuspended {
        return ErrInvalidStatus
    }
    if err := s.Vendors.UpdateStatus(ctx, id, StatusApproved, &adminID); err != nil { return err }
    return s.Plants.Set(ctx, id, plants)
}

func (s *Service) Suspend(ctx context.Context, id string) error {
    v, err := s.Vendors.GetByID(ctx, id)
    if err != nil { return err }
    if v.Status != StatusApproved { return ErrInvalidStatus }
    return s.Vendors.UpdateStatus(ctx, id, StatusSuspended, nil)
}

func (s *Service) Reinstate(ctx context.Context, id string, adminID string) error {
    v, err := s.Vendors.GetByID(ctx, id)
    if err != nil { return err }
    if v.Status != StatusSuspended { return ErrInvalidStatus }
    return s.Vendors.UpdateStatus(ctx, id, StatusApproved, &adminID)
}

func (s *Service) List(ctx context.Context, statuses []Status) ([]*Vendor, error) {
    return s.Vendors.List(ctx, statuses)
}

func (s *Service) ListPlants(ctx context.Context, id string) ([]string, error) {
    list, err := s.Plants.ListByVendor(ctx, id)
    if err != nil { return nil, err }
    out := make([]string, 0, len(list))
    for _, p := range list { if p.Active { out = append(out, p.Plant) } }
    return out, nil
}

// IssueInvite generates a single-use code stored in vendor_invite (identity schema).
// Returned code is what's emailed/handed to the vendor operator.
func (s *Service) IssueInvite(ctx context.Context, vendorID string) (string, error) {
    code := makeInviteCode()
    inv := &identity.VendorInvite{
        Code:      code,
        VendorID:  vendorID,
        ExpiresAt: s.Clock.Now().Add(s.InviteTTL),
    }
    // We need a Put method on identity.VendorInviteRepository — see Task 9 (extends identity repo).
    // For P2, add the method in this task to keep dependencies simple:
    if put, ok := s.Invites.(interface {
        Put(ctx context.Context, inv *identity.VendorInvite) error
    }); ok {
        if err := put.Put(ctx, inv); err != nil { return "", err }
        return code, nil
    }
    return "", fmt.Errorf("vendor: invite repo does not support Put")
}

func makeInviteCode() string {
    b := make([]byte, 12)
    _, _ = rand.Read(b)
    return "TBI-" + base64.RawURLEncoding.EncodeToString(b)
}
```

依賴 `identity.VendorInviteRepository.Put` — 此 method 在 P1 schema 已有 `Get/Consume` 但沒 `Put`。Task 3 在 `services/api/internal/identity/repository.go` 加 `Put` method 到 `VendorInviteRepository` interface，並在 `identity/postgres/vendor_invite_repo.go` 加 implementation：

```go
// vendor_invite_repo.go (add to existing file)
func (r *VendorInviteRepo) Put(ctx context.Context, inv *identity.VendorInvite) error {
    _, err := r.pool.Exec(ctx, `
INSERT INTO vendor_invite (code, vendor_id, email_hint, expires_at)
VALUES ($1,$2,$3,$4)
ON CONFLICT (code) DO NOTHING`,
        inv.Code, inv.VendorID, inv.EmailHint, inv.ExpiresAt)
    return err
}
```

並補一個 test。記得 update `identity/repository.go` 的介面：

```go
type VendorInviteRepository interface {
    Get(ctx context.Context, code string) (*VendorInvite, error)
    Put(ctx context.Context, inv *VendorInvite) error
    Consume(ctx context.Context, code, userID string) error
}
```

**Step 2**：service_test.go — mock-based for 4 業務情境（happy approve / already approved / invalid status / list filters）

**Step 3**：HTTP handlers (huma)

```go
package vhttp

import (
    "context"
    "net/http"

    "github.com/danielgtaylor/huma/v2"

    "github.com/takalawang/corporate-catering-system/services/api/internal/identity"
    idhttp "github.com/takalawang/corporate-catering-system/services/api/internal/identity/http"
    "github.com/takalawang/corporate-catering-system/services/api/internal/vendor"
)

type API struct {
    Svc *vendor.Service
}

func (a *API) Register(api huma.API) {
    huma.Register(api, huma.Operation{
        OperationID: "listVendors",
        Method:      http.MethodGet,
        Path:        "/api/admin/vendors",
        Summary:     "List vendors (admin)",
        Tags:        []string{"admin", "vendor"},
        Security:    []map[string][]string{{"bearer": {}}},
    }, a.list)

    huma.Register(api, huma.Operation{
        OperationID: "createPendingVendor",
        Method:      http.MethodPost,
        Path:        "/api/admin/vendors",
        Summary:     "Create a pending vendor",
        Tags:        []string{"admin", "vendor"},
        Security:    []map[string][]string{{"bearer": {}}},
    }, a.create)

    huma.Register(api, huma.Operation{
        OperationID: "approveVendor",
        Method:      http.MethodPost,
        Path:        "/api/admin/vendors/{id}/approve",
        Summary:     "Approve vendor + set plants",
        Tags:        []string{"admin", "vendor"},
        Security:    []map[string][]string{{"bearer": {}}},
    }, a.approve)

    huma.Register(api, huma.Operation{
        OperationID: "suspendVendor",
        Method:      http.MethodPost,
        Path:        "/api/admin/vendors/{id}/suspend",
        Tags:        []string{"admin", "vendor"},
        Security:    []map[string][]string{{"bearer": {}}},
    }, a.suspend)

    huma.Register(api, huma.Operation{
        OperationID: "issueVendorInvite",
        Method:      http.MethodPost,
        Path:        "/api/admin/vendors/{id}/invite",
        Tags:        []string{"admin", "vendor"},
        Security:    []map[string][]string{{"bearer": {}}},
    }, a.invite)
}

// ... 各 handler 都先用 idhttp.UserFromContext(ctx) 拿 user，檢查 role == welfare_admin，否則 huma.Error403Forbidden

```

DTO + impl 細節由 implementer 補。

**Step 4**：Wire into main.go — 在 P1 的 api role branch 末尾加 `vendorAPI := &vhttp.API{...}` + `vendorAPI.Register(api)`。

**Step 5**：commit

```bash
git add services/api/internal/vendor services/api/internal/identity/repository.go services/api/internal/identity/postgres/vendor_invite_repo.go services/api/cmd/tbite/main.go
git commit -m "feat(vendor): admin service + huma handlers + identity invite Put"
```

---

## Task 4：menu domain + postgres repos

**Files**:
- Create: `services/api/internal/menu/{types.go,errors.go,repository.go}`
- Create: `services/api/internal/menu/postgres/{category_repo.go,item_repo.go,image_repo.go,testhelper_test.go}` + tests

**Domain**:
- `Category` — vendor_id, name, sort_order
- `Item` — vendor_id, category_id?, name, description, price_minor, tags[], badges[], status (draft|active|archived)
- `Image` — item_id, blob_uri, alt, sort_order

**Repo**:
- `CategoryRepository`: Create / Update / Delete / ListByVendor
- `ItemRepository`: Create / Update / Archive / GetByID / ListByVendor (with optional status filter) / ListActiveByPlant(plant, day)
  - `ListActiveByPlant` JOINs vendor_plant_mapping + vendor (status=approved) + meal_supply (date=day)
- `ImageRepository`: AddImage / RemoveImage / ListByItem

**TDD**:
- CategoryRepo: CreateAndList, Delete
- ItemRepo: Create, Update, Archive, ListByVendor filters by status
- ItemRepo: ListActiveByPlant — 不會包含 suspended vendor、不會包含未 approved 廠區
- ImageRepo: Add + List sort order

**Step**：red-green per repo, 1 commit per repo or grouped — choose 2 commits:

```bash
git add services/api/internal/menu/{types.go,errors.go,repository.go,postgres/testhelper_test.go,postgres/category_repo.go,postgres/category_repo_test.go,postgres/item_repo.go,postgres/item_repo_test.go,postgres/image_repo.go,postgres/image_repo_test.go}
git commit -m "feat(menu): domain types + postgres repos (TDD)"
```

---

## Task 5：menu service + huma handlers

**Files**:
- Create: `services/api/internal/menu/service.go` + test
- Create: `services/api/internal/menu/http/handlers.go` + test

**Service**:
- `CreateCategory(ctx, vendorID, name, sortOrder)`
- `CreateItem(ctx, vendorID, input)` — creates item with status=draft
- `UpdateItem(ctx, itemID, input)` 
- `Publish(ctx, itemID)` — sets status=active
- `Archive(ctx, itemID)`
- `ListByVendor(ctx, vendorID, includeArchived bool)`
- `ListForEmployee(ctx, plant, day)` — returns ListActiveByPlant + image URIs

**Authorization**:
- vendor operator can only CRUD items where item.vendor_id == ctx.user.vendor_id
- in service: enforce vendor ownership in Update/Archive/Publish
- in handler: 403 if no vendor_id in user context

**HTTP routes**:
- `POST /api/merchant/categories`
- `GET /api/merchant/categories`
- `POST /api/merchant/menu-items`
- `PATCH /api/merchant/menu-items/{id}`
- `POST /api/merchant/menu-items/{id}/publish`
- `POST /api/merchant/menu-items/{id}/archive`
- `GET /api/merchant/menu-items?include_archived=`
- `GET /api/employee/menu?plant=&day=` — public to authenticated employees

**Step**：標準 TDD，single commit at end:

```bash
git commit -m "feat(menu): service + huma handlers (vendor CRUD + employee read)"
```

---

## Task 6：quota domain + Postgres conditional decrement + concurrency test

**Files**:
- Create: `services/api/internal/quota/{types.go,errors.go,repository.go}`
- Create: `services/api/internal/quota/postgres/supply_repo.go` + test
- Create: `services/api/internal/quota/postgres/concurrency_test.go` — **the centerpiece**

**Types**:

```go
package quota

import "time"

type Supply struct {
    ID           string
    MenuItemID   string
    SupplyDate   time.Time   // date (UTC midnight)
    Capacity     int
    Remain       int
    PickupWindow string
    ETALabel     string
    CutoffAt     time.Time
    CreatedAt    time.Time
    UpdatedAt    time.Time
}

type DecrementResult struct {
    NewRemain int
    Sold      int
}
```

**Errors**:

```go
package quota

import "errors"

var (
    ErrSupplyNotFound = errors.New("quota: supply not found")
    ErrOutOfStock     = errors.New("quota: out of stock")
)
```

**Repo interface**:

```go
package quota

import (
    "context"
    "time"
)

type Repository interface {
    Upsert(ctx context.Context, s *Supply) error // for vendor setting daily capacity
    Get(ctx context.Context, menuItemID string, date time.Time) (*Supply, error)
    ListByVendor(ctx context.Context, vendorID string, date time.Time) ([]*Supply, error)

    // Decrement returns NewRemain. If would go negative, returns ErrOutOfStock.
    // This is the source-of-truth quota operation.
    Decrement(ctx context.Context, menuItemID string, date time.Time, n int) (int, error)

    // Restore (for cancellations) — increment but capped at capacity.
    Restore(ctx context.Context, menuItemID string, date time.Time, n int) error
}
```

**Postgres impl** — Decrement uses conditional UPDATE:

```go
func (r *SupplyRepo) Decrement(ctx context.Context, itemID string, date time.Time, n int) (int, error) {
    if n <= 0 { return 0, fmt.Errorf("quota: n must be positive") }
    var newRemain int
    err := r.pool.QueryRow(ctx, `
UPDATE meal_supply
   SET remain = remain - $3, updated_at = now()
 WHERE menu_item_id = $1
   AND supply_date  = $2
   AND remain >= $3
RETURNING remain`,
        itemID, date, n,
    ).Scan(&newRemain)
    if errors.Is(err, pgx.ErrNoRows) {
        // Either supply doesn't exist OR remain < n. Disambiguate:
        var exists bool
        if err2 := r.pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM meal_supply WHERE menu_item_id=$1 AND supply_date=$2)`, itemID, date).Scan(&exists); err2 != nil {
            return 0, err2
        }
        if !exists { return 0, quota.ErrSupplyNotFound }
        return 0, quota.ErrOutOfStock
    }
    if err != nil { return 0, fmt.Errorf("decrement: %w", err) }
    return newRemain, nil
}
```

**Concurrency test** — the entire point of this task:

```go
func TestSupplyRepo_DecrementNoOversell(t *testing.T) {
    pool, cleanup := setupPostgres(t)
    defer cleanup()
    // seed: vendor + item + supply with capacity 100
    vendorID, itemID := seedVendorAndItem(t, pool)
    supplyDate := time.Now().UTC().Truncate(24 * time.Hour)
    sr := postgres.NewSupplyRepo(pool)
    require.NoError(t, sr.Upsert(context.Background(), &quota.Supply{
        MenuItemID: itemID, SupplyDate: supplyDate,
        Capacity: 100, Remain: 100,
        PickupWindow: "11:50-12:10", ETALabel: "11:50-12:10",
        CutoffAt: time.Now().Add(24 * time.Hour),
    }))

    // Fire 500 concurrent decrement(1) attempts; capacity is 100, so exactly 100 must succeed.
    const N = 500
    var wg sync.WaitGroup
    succeeded := atomic.Int32{}
    outOfStock := atomic.Int32{}
    other := atomic.Int32{}
    for i := 0; i < N; i++ {
        wg.Add(1)
        go func() {
            defer wg.Done()
            _, err := sr.Decrement(context.Background(), itemID, supplyDate, 1)
            switch {
            case err == nil: succeeded.Add(1)
            case errors.Is(err, quota.ErrOutOfStock): outOfStock.Add(1)
            default: other.Add(1)
            }
        }()
    }
    wg.Wait()
    assert.Equal(t, int32(100), succeeded.Load(), "exactly 100 must succeed")
    assert.Equal(t, int32(400), outOfStock.Load(), "exactly 400 must be ErrOutOfStock")
    assert.Equal(t, int32(0), other.Load(), "no other errors")

    final, _ := sr.Get(context.Background(), itemID, supplyDate)
    assert.Equal(t, 0, final.Remain)
}
```

`seedVendorAndItem` 直接 INSERT via SQL — minimal helper in the test file.

This test is the **proof** of design doc §4.5 quota correctness claim.

**Step**：red → green → commit

```bash
git add services/api/internal/quota
git commit -m "feat(quota): supply repo with conditional decrement + concurrency test (500 racers → 100 wins, 400 OUT_OF_STOCK)"
```

---

## Task 7：quota service + huma handlers (vendor capacity + employee remaining)

**Files**:
- Create: `services/api/internal/quota/service.go`
- Create: `services/api/internal/quota/http/handlers.go`

**Service**:
- `SetCapacity(ctx, vendorID, itemID, date, capacity, pickupWindow, eta, cutoff)` — Upsert, with vendor ownership check
- `GetForItem(ctx, itemID, date)`
- `ListForVendor(ctx, vendorID, date)`

**HTTP routes**:
- `PUT /api/merchant/supply/{itemID}/{date}` — set capacity for given item+date
- `GET /api/merchant/supply?date=` — list vendor's supplies for date
- `GET /api/employee/menu` 已在 Task 5 包含 supply — 此 task 確認 employee endpoint 帶上 remain

**Redis display cache** (optional, plan compatibility): skip for P2. Direct read from Postgres is sufficient for typical loads; cache layer is deferred to P3+ if needed.

**Step**: standard service+handlers, 1 commit:

```bash
git commit -m "feat(quota): service + handlers (vendor capacity, employee remain)"
```

---

## Task 8：Employee 菜單瀏覽聚合端點

**Files**:
- Modify: `services/api/internal/menu/http/handlers.go`（Task 5 已建；此 task 補 `GET /api/employee/menu`）
- Modify: `services/api/internal/menu/service.go`（補 `ListForEmployee`）

`ListForEmployee` 將：
1. 取得使用者廠區（from ctx.user.plant）
2. 找出該廠區可服務的 approved vendors
3. 找出這些 vendors 在指定日期有 meal_supply 的 active menu_items
4. 連帶帶出 `images`、`supply.remain`、`supply.capacity`、`supply.pickup_window`、`supply.eta_label`
5. 依 vendor + sort_order 排序

回應 schema (huma):

```go
type employeeMenuResponse struct {
    Body struct {
        Day    string `json:"day"`
        Plant  string `json:"plant"`
        Items  []employeeMenuItem `json:"items"`
    }
}
type employeeMenuItem struct {
    ID          string   `json:"id"`
    Vendor      string   `json:"vendor"`        // display name
    VendorID    string   `json:"vendor_id"`
    Name        string   `json:"name"`
    Description string   `json:"description"`
    PriceMinor  int64    `json:"price_minor"`
    Tags        []string `json:"tags"`
    Badges      []string `json:"badges"`
    Images      []string `json:"images,omitempty"`
    Remain      int      `json:"remain"`
    Capacity    int      `json:"capacity"`
    PickupWindow string  `json:"pickup_window"`
    ETALabel    string   `json:"eta_label"`
    SoldOut     bool     `json:"sold_out"`
}
```

Test: integration test with 2 vendors × 3 items × supply on day D, plant filter pulls only mapped vendor, suspended vendor's items hidden, archived items hidden.

**Step**：commit

```bash
git commit -m "feat(menu): /api/employee/menu aggregation by plant + day"
```

---

## Task 9：完成 P1 留下的 vendor invite 登入流

**Files**:
- Modify: `services/api/internal/identity/service.go` — `bootstrapUser` for `RoleVendorOperator` no longer hard-rejects
- New: support an `invite_code` field in the OIDC flow

**Design**：
- Merchant 在 `/login` 之前先在 `/onboard?invite=TBI-...` 收 invite code via query param
- SvelteKit merchant `/auth/start` 把 `invite_code` 一併 POST 到 Go API `/auth/google/start` body
- Go API 把 invite_code 存進 `oidc.StatePayload`
- callback 完成後：service 收 invite_code → Get + 校驗 (not expired / not consumed) → Consume → 用 invite 的 vendor_id 建 user

**Changes**:
- Extend `oidc.StatePayload` 加 `InviteCode string`
- Extend identity `StartLoginInput` + `CompleteLoginInput` 帶 `InviteCode`
- `bootstrapUser` for `RoleVendorOperator`:
  - 必須帶 invite_code，否則 `ErrInviteNotFound`
  - Get invite → 檢 expired/consumed → consume → create user with vendor_id, role=vendor_operator

**Test**:
- mock-based service test：
  - vendor invite happy
  - missing invite → ErrInviteNotFound
  - expired invite → ErrInviteExpired
  - consumed invite → ErrInviteAlreadyUsed
  - invite vendor_id mismatch (vendor not approved) → ErrInvalidStatus or similar

**SvelteKit changes (merchant only)**:
- `apps/merchant/src/routes/onboard/+page.svelte` — landing page that reads `?invite=` query and stores in session/cookie
- `apps/merchant/src/routes/auth/start/+server.ts` — read invite_code from cookie/query and pass through

**Step**：commit

```bash
git commit -m "feat(identity): vendor invite onboarding flow (merchant)"
```

---

## Task 10：OpenAPI export + TS client regen

**Files**:
- Modify: `services/api/cmd/contract-export/main.go` — register vendor + menu + quota APIs
- Run `make contract-sync`
- Commit generated artifacts

**Step**：

```bash
make contract-sync
git add contract/openapi packages/api-client/src/schema.d.ts services/api/cmd/contract-export
git commit -m "feat(contract): regen openapi + ts client with menu/vendor/quota endpoints"
```

---

## Task 11：packages/ui port — MealCard / StateTag / StatCard / LocationBar

**Files**:
- Create: `packages/ui/src/MealCard.svelte`
- Create: `packages/ui/src/StateTag.svelte`
- Create: `packages/ui/src/StatCard.svelte`
- Create: `packages/ui/src/LocationBar.svelte`
- Modify: `packages/ui/src/index.ts`（exports）

**Idiom**（依 T-Bite Design System 規格，不抄 reference jsx 原碼，按設計規格從零寫 Svelte 5 等效元件）：

- **StateTag**：rounded-full pill, 4 tones (`success` emerald, `warning` amber, `danger` rose, `info` red)，size = text-xs / px-2 py-0.5
- **StatCard**：rounded-tb-2xl + hairline border + tabular numerals 大字（font-noto-tc 900）+ 小 eyebrow label uppercase tracking-eyebrow-wide
- **LocationBar**：兩個 segmented pill controls — left for plant (4 entries), right for day (today / tomorrow / 3-7 days)，slate-100 bg, active = slate-900 bg + white text
- **MealCard**：image area (16:9, rounded-tb-2xl top), bottom area with name (font-bold), vendor name (text-tb-slate-500 text-xs), tags (StateTag chips), price (font-jetbrains-mono font-black), capacity bar (remain/capacity green→amber→rose), stepper (+/-) at the right. Sold-out state: opacity-60 + mask overlay with "本日已售罄". Low-stock state: rose pulse pill "僅剩 N 份".

Each component takes T-Bite typed props. No tests in this task (visual components; lightweight Storybook-style review happens via apps in Tasks 12-14).

**Step**：

```bash
git add packages/ui
git commit -m "feat(ui): port MealCard + StateTag + StatCard + LocationBar"
```

---

## Task 12：Employee app — menu browsing UI

**Files**:
- New: `apps/employee/src/routes/+page.server.ts` — load LocationBar selection (default: 今天 + 使用者廠區)
- Modify: `apps/employee/src/routes/+page.svelte` — show LocationBar + grid of MealCard
- New: `apps/employee/src/routes/menu/[day]/+page.server.ts` + `+page.svelte`
- Optional: query string for plant override

**Server load** calls `GET /api/employee/menu?plant=&day=` via `@tbite/api-client` with session token.

**Step**：

```bash
git commit -m "feat(employee): today menu browsing UI"
```

---

## Task 13：Merchant app — onboarding + menu CRUD + supply management

**Files** (apps/merchant/src/routes):
- `/onboard/+page.{svelte,server.ts}` — landing page taking `?invite=`
- `/+page.svelte` — dashboard with stat cards (今日訂單 dummy 0 / 上週銷售 0 — actual stats land in P4)
- `/menus/+page.{svelte,server.ts}` — list categories + items
- `/menus/new/+page.{svelte,server.ts}` — create item
- `/menus/[id]/+page.{svelte,server.ts}` — edit / publish / archive
- `/supply/+page.{svelte,server.ts}` — view+edit per-day capacities (today + next 6 days, calendar grid)

**Step**：

```bash
git commit -m "feat(merchant): onboarding + menu CRUD + supply management UI"
```

---

## Task 14：Admin app — vendor approval + invite codes

**Files** (apps/admin/src/routes):
- `/+page.svelte` — governance dashboard (vendor count by status, etc.)
- `/vendors/+page.{svelte,server.ts}` — list with status filter, approve/suspend buttons
- `/vendors/[id]/+page.{svelte,server.ts}` — vendor detail + plants + invite code generator
- 新增 vendor form (in-page or modal)

**Step**：

```bash
git commit -m "feat(admin): vendor approval + invite issuance UI"
```

---

## Task 15：Dev seed data

**Files**:
- New: `scripts/dev/seed-p2.sql`
- Modify: `scripts/dev/dev-up.sh`（apply this after seed-e2e）

Seed:
- 3 approved vendors (稻禾家便當 / 綠源輕食 / 禪緣素食)
- Each vendor mapped to 2 plants (F12B-3F, F15-2F shared; F18-RF only for 稻禾家)
- 3-5 items per vendor with prices + tags
- meal_supply for today + next 6 days, capacity=80 each, remain=80

**Step**：

```bash
git commit -m "test(dev): seed approved vendors + menu + supply for the next 7 days"
```

---

## Task 16：e2e + docs + PR

**Files**:
- New: `tests/e2e/menu-browse.spec.ts` — employee logs in, sees today's menu with seeded items
- Update: `README.md` Phases — P2 ✅
- Update: `docs/plans/2026-05-13-tbite-refactor-design.md` §15 — P2 done

**E2E spec**:
```ts
import { test, expect } from "@playwright/test";

test("employee sees today's menu", async ({ page }) => {
  await page.goto("/login");
  await page.getByText("使用 Google 繼續").click();
  await page.waitForURL(/\/$/);
  // Expect at least one seeded item
  await expect(page.getByText("椒麻雞腿便當")).toBeVisible();
});
```

Final exit-criteria run:

```bash
go build ./...
go vet ./...
go test ./... -timeout 10m
pnpm -r check
pnpm -r build
make contract-sync && git diff --exit-code -- contract packages/api-client/src/schema.d.ts
make render-overlay env=single-node | head -3
make render-overlay env=gcp | head -3
# E2E smoke (with FAKE_OIDC + seed + 1 employee app dev server)
pnpm test:e2e
```

Commit + push + open PR:

```bash
git add tests/e2e README.md docs
git commit -m "docs: mark P2 done; e2e menu browse spec"
git push origin feat/p2-menu-vendor-quota
gh pr create --title "P2: menu / vendor / quota + 員工瀏覽今日菜單" --body "$(cat <<'EOF'
## Summary
- Postgres schema 第二波：vendor, vendor_plant_mapping, menu_category, menu_item, menu_item_image, meal_supply
- Go modules: vendor, menu, quota (domain + repos + service + huma handlers each)
- Postgres-anchored conditional decrement; concurrency test proves no oversell (500 racers → exactly 100 wins)
- Employee menu aggregation: /api/employee/menu?plant=&day=
- Vendor invite onboarding flow完成 (merchant login via TBI-... code)
- packages/ui port: MealCard, StateTag, StatCard, LocationBar
- 三 app UI: employee menu browsing / merchant CRUD+supply / admin vendor approval
- OpenAPI + TS client regenerated (contract drift gate green)
- Dev seed: 3 vendors, 12+ items, 7 days of supply

## What's deferred to P3
- 員工真實下單 (cart / order placement)
- order_state_event audit trail
- TOTP pickup verification
- Vendor 自助申請（admin 直接 create pending vendor）

🤖 Generated with [Claude Code](https://claude.com/claude-code)
EOF
)"
```

---

## Exit Criteria

- [ ] All migration up/down/up cycles clean
- [ ] `go test ./... -timeout 10m` passes (incl. quota concurrency test)
- [ ] `pnpm -r check && pnpm -r build` clean
- [ ] `make contract-sync` no drift
- [ ] `make render-overlay env=single-node|gcp` clean
- [ ] Playwright menu-browse spec passes
- [ ] PR opened
