-- P1 identity schema.

CREATE EXTENSION IF NOT EXISTS "pgcrypto";

CREATE TYPE user_role AS ENUM ('employee', 'vendor_operator', 'welfare_admin');
CREATE TYPE user_status AS ENUM ('active', 'suspended', 'terminated');
CREATE TYPE identity_provider AS ENUM ('google', 'github');

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
  provider         identity_provider NOT NULL,
  external_subject TEXT NOT NULL,
  raw_claims       JSONB NOT NULL DEFAULT '{}'::jsonb,
  linked_at        TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX user_identity_provider_subject_idx
  ON user_identity(provider, external_subject);
CREATE INDEX user_identity_user_idx ON user_identity(user_id);

CREATE TABLE employee_directory (
  employee_id   TEXT PRIMARY KEY,
  primary_email TEXT NOT NULL,
  display_name  TEXT NOT NULL,
  plant         TEXT,
  department    TEXT,
  status        user_status NOT NULL DEFAULT 'active',
  created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
  CONSTRAINT employee_dir_email_lower CHECK (primary_email = lower(primary_email))
);
CREATE UNIQUE INDEX employee_directory_email_idx
  ON employee_directory(primary_email);

CREATE TABLE vendor_invite (
  code        TEXT PRIMARY KEY,
  vendor_id   UUID NOT NULL,
  email_hint  TEXT,
  expires_at  TIMESTAMPTZ NOT NULL,
  consumed_at TIMESTAMPTZ,
  consumed_by UUID REFERENCES "user"(id),
  created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX vendor_invite_unconsumed_idx
  ON vendor_invite(vendor_id) WHERE consumed_at IS NULL;

CREATE TABLE admin_email_whitelist (
  email      TEXT PRIMARY KEY,
  added_by   TEXT NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  CONSTRAINT admin_email_lower CHECK (email = lower(email))
);

COMMENT ON TABLE "user" IS 'Canonical user record. Multiple user_identity rows can link to one user.';
COMMENT ON TABLE user_identity IS 'External OIDC subjects (Google/GitHub) linked to a user.';
COMMENT ON TABLE employee_directory IS 'Pre-imported employee whitelist used for first-login binding.';
COMMENT ON TABLE vendor_invite IS 'Single-use invite codes sent by welfare admin to vendor operators.';
COMMENT ON TABLE admin_email_whitelist IS 'Email-based allowlist for welfare admin role; users not in this list cannot become admin.';
