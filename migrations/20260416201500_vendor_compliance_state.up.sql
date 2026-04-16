CREATE TABLE vendor_compliance_state (
    state_key TEXT PRIMARY KEY
        CHECK (state_key <> '' AND state_key = btrim(state_key)),
    payload JSONB NOT NULL
        CHECK (jsonb_typeof(payload) = 'object'),
    created_at_utc TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at_utc TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TRIGGER vendor_compliance_state_set_updated_at_utc
BEFORE UPDATE ON vendor_compliance_state
FOR EACH ROW
EXECUTE FUNCTION set_updated_at_utc();
