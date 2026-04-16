DROP TRIGGER IF EXISTS audit_event_append_only_guard ON audit_event;
DROP TRIGGER IF EXISTS payroll_ledger_entry_append_only_guard ON payroll_ledger_entry;

DROP TRIGGER IF EXISTS actor_account_append_audit_event ON actor_account;
DROP TRIGGER IF EXISTS plant_append_audit_event ON plant;
DROP TRIGGER IF EXISTS vendor_append_audit_event ON vendor;
DROP TRIGGER IF EXISTS vendor_plant_service_window_append_audit_event ON vendor_plant_service_window;
DROP TRIGGER IF EXISTS menu_item_append_audit_event ON menu_item;
DROP TRIGGER IF EXISTS employee_order_append_audit_event ON employee_order;
DROP TRIGGER IF EXISTS employee_order_line_item_append_audit_event ON employee_order_line_item;
DROP TRIGGER IF EXISTS payroll_ledger_entry_append_audit_event ON payroll_ledger_entry;

DROP TRIGGER IF EXISTS actor_account_set_updated_at_utc ON actor_account;
DROP TRIGGER IF EXISTS plant_set_updated_at_utc ON plant;
DROP TRIGGER IF EXISTS vendor_set_updated_at_utc ON vendor;
DROP TRIGGER IF EXISTS vendor_plant_service_window_set_updated_at_utc ON vendor_plant_service_window;
DROP TRIGGER IF EXISTS menu_item_set_updated_at_utc ON menu_item;
DROP TRIGGER IF EXISTS employee_order_set_updated_at_utc ON employee_order;

DROP TABLE IF EXISTS employee_order_line_item;
DROP TABLE IF EXISTS payroll_ledger_entry;
DROP TABLE IF EXISTS employee_order;
DROP TABLE IF EXISTS menu_item;
DROP TABLE IF EXISTS vendor_plant_service_window;
DROP TABLE IF EXISTS vendor;
DROP TABLE IF EXISTS plant;
DROP TABLE IF EXISTS actor_account;
DROP TABLE IF EXISTS audit_event;

DROP FUNCTION IF EXISTS enforce_append_only();
DROP FUNCTION IF EXISTS append_audit_event();
DROP FUNCTION IF EXISTS set_updated_at_utc();

DROP TYPE IF EXISTS audit_action;
DROP TYPE IF EXISTS payroll_source_kind;
DROP TYPE IF EXISTS payroll_entry_kind;
DROP TYPE IF EXISTS order_status;
DROP TYPE IF EXISTS service_window_status;
DROP TYPE IF EXISTS vendor_status;
DROP TYPE IF EXISTS authentication_source;
DROP TYPE IF EXISTS actor_role;

DROP DOMAIN IF EXISTS currency_code;
DROP DOMAIN IF EXISTS money_minor;
DROP DOMAIN IF EXISTS global_pk;
