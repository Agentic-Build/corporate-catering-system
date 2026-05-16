-- P1 identity schema.

CREATE EXTENSION IF NOT EXISTS "pgcrypto";

CREATE TYPE user_role AS ENUM ('employee', 'vendor_operator', 'welfare_admin');
CREATE TYPE user_status AS ENUM ('active', 'suspended', 'terminated');

CREATE TABLE "user" (
  id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  primary_email TEXT NOT NULL,
  display_name  TEXT NOT NULL,
  role          user_role NOT NULL,
  status        user_status NOT NULL DEFAULT 'active',
  employee_id   TEXT,
  vendor_id     UUID,
  plant         TEXT,
  department    TEXT,
  created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
  CONSTRAINT user_primary_email_lower CHECK (primary_email = lower(primary_email))
);
CREATE UNIQUE INDEX user_primary_email_idx ON "user"(primary_email);
CREATE INDEX user_role_idx ON "user"(role);
CREATE INDEX user_employee_id_idx ON "user"(employee_id) WHERE employee_id IS NOT NULL;

CREATE TABLE user_identity (
  id               UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id          UUID NOT NULL REFERENCES "user"(id) ON DELETE CASCADE,
  provider         TEXT NOT NULL CHECK (provider ~ '^[a-z0-9][a-z0-9_.-]*$'),
  external_subject TEXT NOT NULL,
  raw_claims       JSONB NOT NULL DEFAULT '{}'::jsonb,
  linked_at        TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX user_identity_provider_subject_idx
  ON user_identity(provider, external_subject);
CREATE INDEX user_identity_user_idx ON user_identity(user_id);

COMMENT ON TABLE "user" IS 'Canonical user record. Multiple user_identity rows can link to one user.';
COMMENT ON TABLE user_identity IS 'External OIDC provider subjects linked to a user. Provider is a configurable slug.';
