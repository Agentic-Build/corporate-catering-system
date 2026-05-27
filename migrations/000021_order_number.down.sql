ALTER TABLE "order" DROP CONSTRAINT IF EXISTS order_order_number_key;
ALTER TABLE "order" DROP COLUMN IF EXISTS order_number;
DROP SEQUENCE IF EXISTS order_number_seq;
