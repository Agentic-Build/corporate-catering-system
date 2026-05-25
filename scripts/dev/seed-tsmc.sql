-- scripts/dev/seed-tsmc.sql
--
-- TSMC-themed plant + pickup-location seed for the "k3s, single-enterprise
-- prod, Cloudflare Tunnel, TSMC plants" demo playbook
-- (docs/deployment/k3s-cloudflare-tsmc.md). Run AFTER:
--
--   1. The chart's `hooks.dbMigrate` Job (schema migrations).
--   2. scripts/dev/seed-p2.sql (vendors, menu items, meal_supply).
--
-- Idempotent: re-running replaces the plant set without touching the menu
-- catalog, vendor records, or orders.
--
-- The schema does not carry a dedicated `pickup_location` table —
-- plant + pickup point are encoded as a single string in
-- `vendor_plant_mapping.plant` (and `user.plant`, `order.plant`). One
-- TSMC fab can therefore have 1–3 pickup locations by registering 1–3
-- plant strings; vendors then declare which pickup locations they serve
-- via vendor_plant_mapping. The service_window column is set per
-- vendor-plant pair.

BEGIN;

-- ---------------------------------------------------------------------------
-- Plant code naming
--
-- Format: <site>-<building>-<floor>
--
--   hc-hq    Hsinchu HQ              (新竹總部)
--   hc-12a   Hsinchu Fab 12A         (新竹 Fab 12A)
--   hc-12b   Hsinchu Fab 12B         (新竹 Fab 12B)
--   tc-15a   Taichung Fab 15A        (中科 Fab 15A)
--   tc-15b   Taichung Fab 15B        (中科 Fab 15B)
--   tn-14    Tainan Fab 14           (南科 Fab 14)
--   tn-18p1  Tainan Fab 18 Phase 1   (南科 Fab 18 P1)
--   tn-18p3  Tainan Fab 18 Phase 3   (南科 Fab 18 P3)
--   tn-18p7  Tainan Fab 18 Phase 7   (南科 Fab 18 P7)
--
-- The pickup-location suffix encodes building + floor of the canteen or
-- break room:
--
--   <plant>-r1-b1   R1 building basement (admin tower B1 staff cafeteria)
--   <plant>-p5-1f   P5 building 1F (fab P5 ground floor canteen)
--   <plant>-r2-2f   R2 building 2F (small break room, drinks/snacks only)
--   <plant>-1f      single 1F pickup point (small fabs)
--   <plant>-2f      single 2F pickup point
--
-- Pickup count by site size:
--   hc-hq, tn-18p1, tn-18p3 — large, 3 locations each
--   hc-12a, hc-12b, tc-15a, tc-15b — mid, 2 locations each
--   tn-14, tn-18p7 — smaller, 1 location each
-- ---------------------------------------------------------------------------

-- Register the TSMC pickup locations first: vendor_plant_mapping.plant
-- references plant(code) via FK (migration 000018), so every code below must
-- exist in the registry before any mapping row. label/address are what the
-- 福委會 admin maintains and the employee/merchant apps display.
INSERT INTO plant (code, label, address, sort_order) VALUES
  ('hc-hq-r1-b1', '新竹總部 R1 棟 B1 員工餐廳', '新竹市東區力行路 8 號 R1 棟 B1', 10),
  ('hc-hq-p5-1f', '新竹總部 P5 棟 1F 餐廳',     '新竹市東區力行路 8 號 P5 棟 1F', 11),
  ('hc-hq-r2-2f', '新竹總部 R2 棟 2F 茶水間',   '新竹市東區力行路 8 號 R2 棟 2F', 12),
  ('hc-12a-1f',   '新竹 Fab 12A 1F',           '新竹科學園區園區三路 12 號 1F', 13),
  ('hc-12a-3f',   '新竹 Fab 12A 3F',           '新竹科學園區園區三路 12 號 3F', 14),
  ('hc-12b-1f',   '新竹 Fab 12B 1F',           '新竹科學園區園區三路 14 號 1F', 15),
  ('hc-12b-3f',   '新竹 Fab 12B 3F',           '新竹科學園區園區三路 14 號 3F', 16),
  ('tc-15a-1f',   '中科 Fab 15A 1F',           '臺中市西屯區中科路 15 號 1F', 17),
  ('tc-15a-3f',   '中科 Fab 15A 3F',           '臺中市西屯區中科路 15 號 3F', 18),
  ('tc-15b-1f',   '中科 Fab 15B 1F',           '臺中市西屯區中科路 17 號 1F', 19),
  ('tc-15b-3f',   '中科 Fab 15B 3F',           '臺中市西屯區中科路 17 號 3F', 20),
  ('tn-14-2f',    '南科 Fab 14 2F',            '臺南市新市區南科三路 14 號 2F', 21),
  ('tn-18p1-1f',  '南科 Fab 18 P1 1F',         '臺南市善化區南科二路 18 號 P1 1F', 22),
  ('tn-18p1-3f',  '南科 Fab 18 P1 3F',         '臺南市善化區南科二路 18 號 P1 3F', 23),
  ('tn-18p1-b1',  '南科 Fab 18 P1 B1',         '臺南市善化區南科二路 18 號 P1 B1', 24),
  ('tn-18p3-1f',  '南科 Fab 18 P3 1F',         '臺南市善化區南科二路 18 號 P3 1F', 25),
  ('tn-18p3-3f',  '南科 Fab 18 P3 3F',         '臺南市善化區南科二路 18 號 P3 3F', 26),
  ('tn-18p3-b1',  '南科 Fab 18 P3 B1',         '臺南市善化區南科二路 18 號 P3 B1', 27),
  ('tn-18p7-2f',  '南科 Fab 18 P7 2F',         '臺南市善化區南科二路 18 號 P7 2F', 28)
ON CONFLICT (code) DO NOTHING;

-- Replace the legacy tn-a..tn-d plant set with the TSMC pickup-location codes.
DELETE FROM vendor_plant_mapping;

-- Insert (vendor × plant) rows. All 10 demo vendors (a1111111..aaaaaaaa)
-- serve every pickup location for the demo; in real deployments the
-- 福委會 admin app prunes per vendor catchment via the UI.
INSERT INTO vendor_plant_mapping (vendor_id, plant, service_window) VALUES
  -- Hsinchu HQ (3 pickup locations)
  ('a1111111-1111-1111-1111-111111111111', 'hc-hq-r1-b1', '11:30-13:00'),
  ('a1111111-1111-1111-1111-111111111111', 'hc-hq-p5-1f', '11:30-13:00'),
  ('a1111111-1111-1111-1111-111111111111', 'hc-hq-r2-2f', '14:00-17:00'),
  -- Hsinchu Fab 12A (2)
  ('a1111111-1111-1111-1111-111111111111', 'hc-12a-1f',  '11:45-13:00'),
  ('a1111111-1111-1111-1111-111111111111', 'hc-12a-3f',  '14:00-17:00'),
  -- Hsinchu Fab 12B (2)
  ('a1111111-1111-1111-1111-111111111111', 'hc-12b-1f',  '11:45-13:00'),
  ('a1111111-1111-1111-1111-111111111111', 'hc-12b-3f',  '14:00-17:00'),
  -- Taichung Fab 15A (2)
  ('a1111111-1111-1111-1111-111111111111', 'tc-15a-1f',  '11:45-13:00'),
  ('a1111111-1111-1111-1111-111111111111', 'tc-15a-3f',  '14:00-17:00'),
  -- Taichung Fab 15B (2)
  ('a1111111-1111-1111-1111-111111111111', 'tc-15b-1f',  '11:45-13:00'),
  ('a1111111-1111-1111-1111-111111111111', 'tc-15b-3f',  '14:00-17:00'),
  -- Tainan Fab 14 (1)
  ('a1111111-1111-1111-1111-111111111111', 'tn-14-2f',   '11:45-13:00'),
  -- Tainan Fab 18 Phase 1 (3)
  ('a1111111-1111-1111-1111-111111111111', 'tn-18p1-1f', '11:30-13:00'),
  ('a1111111-1111-1111-1111-111111111111', 'tn-18p1-3f', '14:00-17:00'),
  ('a1111111-1111-1111-1111-111111111111', 'tn-18p1-b1', '11:30-13:00'),
  -- Tainan Fab 18 Phase 3 (3)
  ('a1111111-1111-1111-1111-111111111111', 'tn-18p3-1f', '11:30-13:00'),
  ('a1111111-1111-1111-1111-111111111111', 'tn-18p3-3f', '14:00-17:00'),
  ('a1111111-1111-1111-1111-111111111111', 'tn-18p3-b1', '11:30-13:00'),
  -- Tainan Fab 18 Phase 7 (1)
  ('a1111111-1111-1111-1111-111111111111', 'tn-18p7-2f', '11:45-13:00');

-- Replay the same plant set for the remaining 9 vendors so every fab has
-- catchment options. service_window mirrors the canonical r001 mapping.
INSERT INTO vendor_plant_mapping (vendor_id, plant, service_window)
SELECT v.vendor_id, m.plant, m.service_window
FROM (
  VALUES
    ('a2222222-2222-2222-2222-222222222222'::uuid),
    ('a3333333-3333-3333-3333-333333333333'::uuid),
    ('a4444444-4444-4444-4444-444444444444'::uuid),
    ('a5555555-5555-5555-5555-555555555555'::uuid),
    ('a6666666-6666-6666-6666-666666666666'::uuid),
    ('a7777777-7777-7777-7777-777777777777'::uuid),
    ('a8888888-8888-8888-8888-888888888888'::uuid),
    ('a9999999-9999-9999-9999-999999999999'::uuid),
    ('aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa'::uuid)
) AS v(vendor_id)
CROSS JOIN vendor_plant_mapping m
WHERE m.vendor_id = 'a1111111-1111-1111-1111-111111111111'
ON CONFLICT (vendor_id, plant) DO NOTHING;

-- ---------------------------------------------------------------------------
-- Move the canonical demo user (e2e-employee) onto a TSMC plant so the
-- employee app's home page renders against a real-looking pickup point.
-- ---------------------------------------------------------------------------
UPDATE "user"
SET plant      = 'hc-12a-1f',
    department = 'R&D'
WHERE primary_email = 'e2e-employee@tbite.test';

-- Spread the four additional demo employees across the TSMC sites so the
-- merchant and admin views show multi-fab traffic.
UPDATE "user" SET plant = 'tn-18p1-1f', department = 'Fab Operations'
  WHERE primary_email = 'emp-tnb@tbite.test';
UPDATE "user" SET plant = 'tc-15a-1f', department = 'Quality'
  WHERE primary_email = 'emp-tnc@tbite.test';
UPDATE "user" SET plant = 'tn-14-2f',  department = 'Engineering'
  WHERE primary_email = 'emp-tnd@tbite.test';
UPDATE "user" SET plant = 'hc-hq-p5-1f', department = 'Welfare Committee'
  WHERE primary_email = 'emp-tna2@tbite.test';

COMMIT;
