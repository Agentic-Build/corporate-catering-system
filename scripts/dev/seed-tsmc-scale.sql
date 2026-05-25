-- scripts/dev/seed-tsmc-scale.sql
--
-- Enterprise-scale TSMC demo seed. Run AFTER:
--
--   1. scripts/dev/seed-p2.sql
--   2. scripts/dev/seed-demo.sql
--   3. scripts/dev/seed-tsmc.sql
--
-- Idempotent: re-running refreshes only the synthetic tsmcNNNNN@tbite.test
-- employee population and scales meal_supply capacity for the demo week.

BEGIN;

DO $$
DECLARE
  missing_plants TEXT;
BEGIN
  SELECT string_agg(p.plant, ', ' ORDER BY p.plant)
  INTO missing_plants
  FROM (
    VALUES
      ('hc-hq-r1-b1'), ('hc-hq-p5-1f'), ('hc-hq-r2-2f'),
      ('hc-12a-1f'), ('hc-12a-3f'),
      ('hc-12b-1f'), ('hc-12b-3f'),
      ('tc-15a-1f'), ('tc-15a-3f'),
      ('tc-15b-1f'), ('tc-15b-3f'),
      ('tn-14-2f'),
      ('tn-18p1-1f'), ('tn-18p1-3f'), ('tn-18p1-b1'),
      ('tn-18p3-1f'), ('tn-18p3-3f'), ('tn-18p3-b1'),
      ('tn-18p7-2f')
  ) AS p(plant)
  WHERE NOT EXISTS (
    SELECT 1
    FROM vendor_plant_mapping vpm
    WHERE vpm.plant = p.plant AND vpm.active
  );

  IF missing_plants IS NOT NULL THEN
    RAISE EXCEPTION
      'seed-tsmc-scale.sql requires scripts/dev/seed-tsmc.sql first. Missing active TSMC pickup locations: %',
      missing_plants;
  END IF;
END $$;

-- 50,000 employees across nine TSMC fab/admin sites and nineteen pickup
-- locations. The distribution is intentionally uneven: large Fab 18 phases
-- and Hsinchu/Taichung fabs carry more headcount than smaller locations.
WITH plant_weights(plant, headcount) AS (
  VALUES
    ('hc-hq-r1-b1', 2200), ('hc-hq-p5-1f', 2200), ('hc-hq-r2-2f', 1100),
    ('hc-12a-1f', 3600), ('hc-12a-3f', 2400),
    ('hc-12b-1f', 3300), ('hc-12b-3f', 2200),
    ('tc-15a-1f', 3120), ('tc-15a-3f', 2080),
    ('tc-15b-1f', 2880), ('tc-15b-3f', 1920),
    ('tn-14-2f', 3000),
    ('tn-18p1-1f', 3000), ('tn-18p1-3f', 2250), ('tn-18p1-b1', 2250),
    ('tn-18p3-1f', 3400), ('tn-18p3-3f', 2550), ('tn-18p3-b1', 2550),
    ('tn-18p7-2f', 4000)
),
numbered AS (
  SELECT
    row_number() OVER (ORDER BY plant_weights.plant, plant_seq) AS global_seq,
    plant_weights.plant,
    plant_seq
  FROM plant_weights
  CROSS JOIN LATERAL generate_series(1, plant_weights.headcount) AS plant_seq
),
employees AS (
  SELECT
    md5('tbite:tsmc-scale:employee:' || global_seq::TEXT)::UUID AS id,
    'tsmc' || lpad(global_seq::TEXT, 5, '0') || '@tbite.test' AS primary_email,
    'TSMC Employee ' || lpad(global_seq::TEXT, 5, '0') AS display_name,
    'TSMC' || lpad(global_seq::TEXT, 5, '0') AS employee_id,
    plant,
    CASE (global_seq % 9)
      WHEN 0 THEN 'Fab Operations'
      WHEN 1 THEN 'Process Integration'
      WHEN 2 THEN 'Yield Engineering'
      WHEN 3 THEN 'Equipment'
      WHEN 4 THEN 'Facilities'
      WHEN 5 THEN 'Manufacturing IT'
      WHEN 6 THEN 'Quality'
      WHEN 7 THEN 'Procurement'
      ELSE 'Welfare Committee'
    END AS department
  FROM numbered
)
INSERT INTO "user" (id, primary_email, display_name, role, status, employee_id, plant, department)
SELECT
  id,
  primary_email,
  display_name,
  'employee',
  'active',
  employee_id,
  plant,
  department
FROM employees
ON CONFLICT (primary_email) DO UPDATE SET
  display_name = EXCLUDED.display_name,
  role = EXCLUDED.role,
  status = EXCLUDED.status,
  employee_id = EXCLUDED.employee_id,
  plant = EXCLUDED.plant,
  department = EXCLUDED.department,
  updated_at = now();

-- The base catalog has 150 menu items. Capacity 300 gives 45,000 available
-- portions per day across the seven-day demo window: enough for a 50k-person
-- enterprise with realistic partial daily adoption, while still letting the
-- lunch-crunch drill exhaust one hot item.
UPDATE meal_supply
SET
  capacity = 300,
  remain = 300,
  sold_out = false,
  updated_at = now()
WHERE supply_date BETWEEN CURRENT_DATE AND CURRENT_DATE + INTERVAL '6 days';

DO $$
DECLARE
  employee_count INTEGER;
BEGIN
  SELECT count(*)
  INTO employee_count
  FROM "user"
  WHERE primary_email ~ '^tsmc[0-9]{5}@tbite\.test$';

  IF employee_count <> 50000 THEN
    RAISE EXCEPTION 'expected 50000 synthetic TSMC employees, got %', employee_count;
  END IF;
END $$;

SELECT 'tsmc_scale_employees' AS metric, count(*) AS value
FROM "user"
WHERE primary_email ~ '^tsmc[0-9]{5}@tbite\.test$'
UNION ALL
SELECT 'tsmc_pickup_locations', count(DISTINCT plant)
FROM vendor_plant_mapping
WHERE plant IN (
  'hc-hq-r1-b1', 'hc-hq-p5-1f', 'hc-hq-r2-2f',
  'hc-12a-1f', 'hc-12a-3f',
  'hc-12b-1f', 'hc-12b-3f',
  'tc-15a-1f', 'tc-15a-3f',
  'tc-15b-1f', 'tc-15b-3f',
  'tn-14-2f',
  'tn-18p1-1f', 'tn-18p1-3f', 'tn-18p1-b1',
  'tn-18p3-1f', 'tn-18p3-3f', 'tn-18p3-b1',
  'tn-18p7-2f'
)
UNION ALL
SELECT 'scaled_daily_portions', sum(capacity)::BIGINT
FROM meal_supply
WHERE supply_date = CURRENT_DATE;

COMMIT;
