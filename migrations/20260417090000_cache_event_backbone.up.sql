CREATE TABLE domain_event_outbox (
    id global_pk PRIMARY KEY DEFAULT gen_random_uuid(),
    event_id TEXT NOT NULL UNIQUE
        CHECK (event_id <> '' AND event_id = btrim(event_id)),
    subject TEXT NOT NULL
        CHECK (subject <> '' AND subject = btrim(subject)),
    payload JSONB NOT NULL
        CHECK (jsonb_typeof(payload) = 'object'),
    publish_attempts INTEGER NOT NULL DEFAULT 0
        CHECK (publish_attempts >= 0),
    last_publish_error TEXT,
    created_at_utc TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    published_at_utc TIMESTAMPTZ
);

CREATE INDEX domain_event_outbox_unpublished_idx
    ON domain_event_outbox (published_at_utc, created_at_utc)
    WHERE published_at_utc IS NULL;

CREATE TABLE jetstream_consumer_dedup (
    id global_pk PRIMARY KEY DEFAULT gen_random_uuid(),
    consumer_name TEXT NOT NULL
        CHECK (consumer_name <> '' AND consumer_name = btrim(consumer_name)),
    event_id TEXT NOT NULL
        CHECK (event_id <> '' AND event_id = btrim(event_id)),
    processed_at_utc TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE (consumer_name, event_id)
);

CREATE TABLE order_state_event_projection (
    order_id TEXT PRIMARY KEY
        CHECK (order_id <> '' AND order_id = btrim(order_id)),
    vendor_id TEXT NOT NULL
        CHECK (vendor_id <> '' AND vendor_id = btrim(vendor_id)),
    plant_id TEXT NOT NULL
        CHECK (plant_id <> '' AND plant_id = btrim(plant_id)),
    order_state TEXT NOT NULL
        CHECK (order_state <> '' AND order_state = btrim(order_state)),
    event_id TEXT NOT NULL
        CHECK (event_id <> '' AND event_id = btrim(event_id)),
    updated_at_utc TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE jetstream_dead_letter (
    id global_pk PRIMARY KEY DEFAULT gen_random_uuid(),
    consumer_name TEXT NOT NULL
        CHECK (consumer_name <> '' AND consumer_name = btrim(consumer_name)),
    source_subject TEXT NOT NULL
        CHECK (source_subject <> '' AND source_subject = btrim(source_subject)),
    delivery_attempt INTEGER NOT NULL
        CHECK (delivery_attempt > 0),
    failure_reason TEXT NOT NULL
        CHECK (failure_reason <> '' AND failure_reason = btrim(failure_reason)),
    payload JSONB NOT NULL
        CHECK (jsonb_typeof(payload) = 'object'),
    failed_at_utc TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);
