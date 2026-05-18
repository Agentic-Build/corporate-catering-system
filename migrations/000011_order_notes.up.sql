-- Free-text special-requirements note carried on an order and shown on the
-- merchant prep board (e.g. allergy / spice preferences).
ALTER TABLE "order" ADD COLUMN notes TEXT NOT NULL DEFAULT '';
