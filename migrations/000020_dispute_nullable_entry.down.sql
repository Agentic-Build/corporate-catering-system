-- Restore NOT NULL. Any entry-less disputes must be backfilled or removed first.
ALTER TABLE payroll_dispute ALTER COLUMN entry_id SET NOT NULL;
