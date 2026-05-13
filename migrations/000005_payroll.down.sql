DROP TRIGGER IF EXISTS payroll_entry_no_delete_trg ON payroll_entry;
DROP TABLE IF EXISTS payroll_dispute;
DROP TABLE IF EXISTS payroll_entry;
DROP FUNCTION IF EXISTS payroll_entry_no_delete();
DROP TABLE IF EXISTS payroll_batch;
DROP TYPE IF EXISTS payroll_dispute_status;
DROP TYPE IF EXISTS payroll_batch_status;
