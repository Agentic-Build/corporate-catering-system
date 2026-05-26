-- Allow disputes against current-period orders that have no payroll entry yet.
-- order_id stays NOT NULL as the durable link; entry_id becomes optional and is
-- backfilled at resolution time once the order's entry exists.
ALTER TABLE payroll_dispute ALTER COLUMN entry_id DROP NOT NULL;
