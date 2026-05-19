-- scripts/dev/seed-demo.sql
-- Demo-friendly seed layered on top of seed-p2.sql.
-- Adds menu item images + a demo employee with a few recent orders so all
-- three web apps look populated. Run after seed-p2.sql. Safe to re-run.

BEGIN;

-- ---------------------------------------------------------------------------
-- Menu item images
-- Static brand photos already ship with the employee app under
-- apps/employee/static/brand/items/. They are served at /brand/items/iNNN.jpg.
-- menu_item_image has no unique constraint, so guard inserts with NOT EXISTS.
-- ---------------------------------------------------------------------------
INSERT INTO menu_item_image (menu_item_id, blob_uri, alt, sort_order)
SELECT v.menu_item_id, v.blob_uri, v.alt, 0
FROM (VALUES
  ('b1000001-0000-0000-0000-000000000001'::uuid, '/brand/items/i001.jpg', '椒麻雞腿便當'),
  ('b1000002-0000-0000-0000-000000000001'::uuid, '/brand/items/i016.jpg', '古早味滷肉飯'),
  ('b1000003-0000-0000-0000-000000000001'::uuid, '/brand/items/i031.jpg', '三杯雞便當'),
  ('b1000004-0000-0000-0000-000000000001'::uuid, '/brand/items/i046.jpg', '蔥燒排骨便當'),
  ('b2000001-0000-0000-0000-000000000002'::uuid, '/brand/items/i061.jpg', '藜麥雞胸沙拉碗'),
  ('b2000002-0000-0000-0000-000000000002'::uuid, '/brand/items/i076.jpg', '酪梨鮭魚溫沙拉'),
  ('b2000003-0000-0000-0000-000000000002'::uuid, '/brand/items/i091.jpg', '蔬菜雜糧捲餅'),
  ('b2000004-0000-0000-0000-000000000002'::uuid, '/brand/items/i107.jpg', '溫野菜牛肉碗'),
  ('b3000001-0000-0000-0000-000000000003'::uuid, '/brand/items/i121.jpg', '紅藜時蔬便當'),
  ('b3000002-0000-0000-0000-000000000003'::uuid, '/brand/items/i136.jpg', '麻油猴頭菇飯')
) AS v(menu_item_id, blob_uri, alt)
WHERE NOT EXISTS (
  SELECT 1 FROM menu_item_image mii
  WHERE mii.menu_item_id = v.menu_item_id
    AND mii.blob_uri = v.blob_uri
);

-- Vendor cover/logo images: the schema has no column for vendor branding
-- (vendor only gained cutoff_hour / preorder_window_days), so these are skipped.

-- ---------------------------------------------------------------------------
-- Demo employee
-- e2e-employee@tbite.test is the canonical demo employee (matches Authentik
-- dev blueprint + e2e fixtures). Fixed UUID so demo orders can reference it.
-- ---------------------------------------------------------------------------
INSERT INTO "user" (id, primary_email, display_name, role, status, employee_id, plant, department)
VALUES (
  'c0000000-0000-0000-0000-0000000000e1',
  'e2e-employee@tbite.test',
  'E2E 員工',
  'employee',
  'active',
  'E2E001',
  'F12B-3F',
  'IT'
)
ON CONFLICT (primary_email) DO NOTHING;

-- ---------------------------------------------------------------------------
-- Demo orders
-- Three orders for the demo employee across statuses placed / ready / picked_up
-- so the merchant prep board, pickup view, and employee order history all show
-- data. Fixed order UUIDs make this idempotent. Each order has one order_item.
-- placed_at / ready_at / picked_up_at are set per status to satisfy the
-- lifecycle; totp_secret keeps its column default.
-- ---------------------------------------------------------------------------
INSERT INTO "order"
  (id, user_id, vendor_id, plant, supply_date, status, total_price_minor,
   placed_at, cutoff_at, ready_at, picked_up_at)
VALUES
  -- placed today: awaiting cutoff, visible on merchant prep board
  ('d0000000-0000-0000-0000-000000000001',
   'c0000000-0000-0000-0000-0000000000e1',
   'a1111111-1111-1111-1111-111111111111',
   'F12B-3F', CURRENT_DATE, 'placed', 110,
   now() - INTERVAL '2 hours',
   (CURRENT_DATE + INTERVAL '17 hours')::timestamptz,
   NULL, NULL),
  -- ready today: prepared, awaiting pickup
  ('d0000000-0000-0000-0000-000000000002',
   'c0000000-0000-0000-0000-0000000000e1',
   'a2222222-2222-2222-2222-222222222222',
   'F12B-3F', CURRENT_DATE, 'ready', 145,
   now() - INTERVAL '3 hours',
   (CURRENT_DATE + INTERVAL '17 hours')::timestamptz,
   now() - INTERVAL '30 minutes', NULL),
  -- picked_up yesterday: completed, shows in order history
  ('d0000000-0000-0000-0000-000000000003',
   'c0000000-0000-0000-0000-0000000000e1',
   'a3333333-3333-3333-3333-333333333333',
   'F12B-3F', CURRENT_DATE - 1, 'picked_up', 95,
   (CURRENT_DATE - 1 + INTERVAL '9 hours')::timestamptz,
   (CURRENT_DATE - 1 + INTERVAL '17 hours')::timestamptz,
   (CURRENT_DATE - 1 + INTERVAL '11 hours')::timestamptz,
   (CURRENT_DATE - 1 + INTERVAL '12 hours')::timestamptz)
ON CONFLICT (id) DO NOTHING;

INSERT INTO order_item (order_id, menu_item_id, qty, unit_price_minor)
SELECT v.order_id, v.menu_item_id, 1, v.unit_price_minor
FROM (VALUES
  ('d0000000-0000-0000-0000-000000000001'::uuid, 'b1000001-0000-0000-0000-000000000001'::uuid, 110::bigint),
  ('d0000000-0000-0000-0000-000000000002'::uuid, 'b2000001-0000-0000-0000-000000000002'::uuid, 145::bigint),
  ('d0000000-0000-0000-0000-000000000003'::uuid, 'b3000001-0000-0000-0000-000000000003'::uuid, 95::bigint)
) AS v(order_id, menu_item_id, unit_price_minor)
WHERE NOT EXISTS (
  SELECT 1 FROM order_item oi WHERE oi.order_id = v.order_id
);

COMMIT;
