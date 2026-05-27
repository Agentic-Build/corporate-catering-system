CREATE SEQUENCE IF NOT EXISTS order_number_seq AS BIGINT START WITH 1001;
ALTER TABLE "order" ADD COLUMN order_number BIGINT;
WITH ordered AS (
  SELECT id, row_number() OVER (ORDER BY created_at, id) AS rn FROM "order"
)
UPDATE "order" o SET order_number = 1000 + ordered.rn FROM ordered WHERE o.id = ordered.id;
SELECT setval('order_number_seq', GREATEST((SELECT COALESCE(MAX(order_number), 1000) FROM "order"), 1000));
ALTER TABLE "order" ALTER COLUMN order_number SET DEFAULT nextval('order_number_seq');
ALTER TABLE "order" ALTER COLUMN order_number SET NOT NULL;
ALTER TABLE "order" ADD CONSTRAINT order_order_number_key UNIQUE (order_number);
