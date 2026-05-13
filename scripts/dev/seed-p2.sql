-- scripts/dev/seed-p2.sql
-- Idempotent dev seed for P2: 3 vendors + 12 items + 7 days of meal_supply.
-- Run after migrations. Safe to run multiple times.

BEGIN;

-- Vendors
INSERT INTO vendor (id, display_name, legal_name, contact_email, status, approved_at)
VALUES
  ('a1111111-1111-1111-1111-111111111111', '稻禾家便當', '稻禾家便當有限公司', 'daohe@tbite.test',   'approved', now()),
  ('a2222222-2222-2222-2222-222222222222', '綠源輕食',   '綠源輕食股份有限公司', 'lvyuan@tbite.test',   'approved', now()),
  ('a3333333-3333-3333-3333-333333333333', '禪緣素食',   '禪緣素食事業有限公司', 'chanyuan@tbite.test', 'approved', now())
ON CONFLICT (id) DO NOTHING;

-- Plant mappings
INSERT INTO vendor_plant_mapping (vendor_id, plant) VALUES
  ('a1111111-1111-1111-1111-111111111111', 'F12B-3F'),
  ('a1111111-1111-1111-1111-111111111111', 'F15-2F'),
  ('a1111111-1111-1111-1111-111111111111', 'F18-RF'),
  ('a2222222-2222-2222-2222-222222222222', 'F12B-3F'),
  ('a2222222-2222-2222-2222-222222222222', 'F15-2F'),
  ('a3333333-3333-3333-3333-333333333333', 'F12B-3F'),
  ('a3333333-3333-3333-3333-333333333333', 'F15-2F')
ON CONFLICT (vendor_id, plant) DO NOTHING;

-- Menu items (12)
INSERT INTO menu_item (id, vendor_id, name, description, price_minor, tags, badges, status)
VALUES
  -- 稻禾家便當 (Hot bento)
  ('b1000001-0000-0000-0000-000000000001', 'a1111111-1111-1111-1111-111111111111', '椒麻雞腿便當',  '經典招牌椒麻雞腿',     110, ARRAY['hot'],     ARRAY['可薪資代扣'],            'active'),
  ('b1000002-0000-0000-0000-000000000001', 'a1111111-1111-1111-1111-111111111111', '古早味滷肉飯',  '香滷肉燥配時蔬',       85,  ARRAY['hot'],     ARRAY['可薪資代扣'],            'active'),
  ('b1000003-0000-0000-0000-000000000001', 'a1111111-1111-1111-1111-111111111111', '三杯雞便當',    '九層塔三杯雞腿',       120, ARRAY['hot'],     ARRAY['可薪資代扣'],            'active'),
  ('b1000004-0000-0000-0000-000000000001', 'a1111111-1111-1111-1111-111111111111', '蔥燒排骨便當',  '招牌排骨',              115, ARRAY['hot'],     ARRAY['可薪資代扣'],            'active'),
  -- 綠源輕食 (Healthy)
  ('b2000001-0000-0000-0000-000000000002', 'a2222222-2222-2222-2222-222222222222', '藜麥雞胸沙拉碗', '高蛋白低 GI 配方',     145, ARRAY['healthy'], ARRAY['可薪資代扣','低於 500 kcal'], 'active'),
  ('b2000002-0000-0000-0000-000000000002', 'a2222222-2222-2222-2222-222222222222', '酪梨鮭魚溫沙拉', '挪威鮭魚 + 酪梨',       165, ARRAY['healthy'], ARRAY['可薪資代扣'],            'active'),
  ('b2000003-0000-0000-0000-000000000002', 'a2222222-2222-2222-2222-222222222222', '蔬菜雜糧捲餅',  '七種蔬菜全麥捲',       125, ARRAY['healthy'], ARRAY['可薪資代扣','全素'],        'active'),
  ('b2000004-0000-0000-0000-000000000002', 'a2222222-2222-2222-2222-222222222222', '溫野菜牛肉碗',  '少油慢燉牛腩',          155, ARRAY['healthy'], ARRAY['可薪資代扣'],            'active'),
  -- 禪緣素食 (Vegetarian)
  ('b3000001-0000-0000-0000-000000000003', 'a3333333-3333-3333-3333-333333333333', '紅藜時蔬便當',  '紅藜配當季蔬菜',       95,  ARRAY['veggie'],  ARRAY['可薪資代扣','全素'],        'active'),
  ('b3000002-0000-0000-0000-000000000003', 'a3333333-3333-3333-3333-333333333333', '麻油猴頭菇飯',  '麻油猴頭菇 + 五穀飯',  105, ARRAY['veggie'],  ARRAY['可薪資代扣','全素'],        'active'),
  ('b3000003-0000-0000-0000-000000000003', 'a3333333-3333-3333-3333-333333333333', '日式蔬食壽司捲', '8 入', 100, ARRAY['veggie'],  ARRAY['可薪資代扣','全素'],        'active'),
  ('b3000004-0000-0000-0000-000000000003', 'a3333333-3333-3333-3333-333333333333', '味噌豆腐丼',    '日式味噌風味', 90,  ARRAY['veggie'],  ARRAY['可薪資代扣','全素'],        'active')
ON CONFLICT (id) DO NOTHING;

-- Meal supplies for today + next 6 days (12 items × 7 days = 84 rows)
INSERT INTO meal_supply (menu_item_id, supply_date, capacity, remain, pickup_window, eta_label, cutoff_at)
SELECT
  mi.id,
  d::date AS supply_date,
  80 AS capacity,
  80 AS remain,
  '11:50-12:10' AS pickup_window,
  '11:50-12:10' AS eta_label,
  (d::date + INTERVAL '17 hours')::timestamptz AS cutoff_at
FROM
  menu_item mi
  CROSS JOIN generate_series(CURRENT_DATE, CURRENT_DATE + INTERVAL '6 days', INTERVAL '1 day') AS d
WHERE
  mi.vendor_id IN (
    'a1111111-1111-1111-1111-111111111111',
    'a2222222-2222-2222-2222-222222222222',
    'a3333333-3333-3333-3333-333333333333'
  )
ON CONFLICT (menu_item_id, supply_date) DO NOTHING;

-- Admin allowlist (test admin can log in via Google)
INSERT INTO admin_email_whitelist (email, added_by)
VALUES ('e2e-admin@tbite.test', 'seed-p2')
ON CONFLICT (email) DO NOTHING;

-- An invite code for merchant onboarding e2e (linked to vendor 稻禾家便當)
INSERT INTO vendor_invite (code, vendor_id, expires_at)
VALUES ('TBI-DEV-DAOHE-001', 'a1111111-1111-1111-1111-111111111111', now() + INTERVAL '30 days')
ON CONFLICT (code) DO NOTHING;

COMMIT;
