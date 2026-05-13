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
