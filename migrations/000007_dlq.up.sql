CREATE TABLE dlq_message (
  id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  source_stream   TEXT NOT NULL,
  source_subject  TEXT NOT NULL,
  source_consumer TEXT NOT NULL,
  payload         JSONB NOT NULL,
  headers         JSONB NOT NULL DEFAULT '{}'::jsonb,
  last_error      TEXT NOT NULL DEFAULT '',
  first_seen_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
  replayed_at     TIMESTAMPTZ,
  replayed_by     UUID REFERENCES "user"(id),
  resolved_at     TIMESTAMPTZ,
  resolved_by     UUID REFERENCES "user"(id),
  resolved_notes  TEXT NOT NULL DEFAULT ''
);
CREATE INDEX dlq_message_pending_idx ON dlq_message(source_stream, first_seen_at DESC)
  WHERE replayed_at IS NULL AND resolved_at IS NULL;
CREATE INDEX dlq_message_subject_idx ON dlq_message(source_subject);
