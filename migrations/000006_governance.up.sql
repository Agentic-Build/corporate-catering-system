CREATE TYPE vendor_document_kind AS ENUM (
    'business_license', 'food_safety_permit', 'tax_registration', 'insurance', 'other'
);
CREATE TYPE vendor_document_status AS ENUM ('pending', 'approved', 'rejected', 'expired');
CREATE TYPE anomaly_severity AS ENUM ('low', 'medium', 'high', 'critical');
CREATE TYPE anomaly_status AS ENUM ('open', 'triaged', 'closed');

CREATE TABLE vendor_document (
  id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  vendor_id    UUID NOT NULL REFERENCES vendor(id) ON DELETE CASCADE,
  kind         vendor_document_kind NOT NULL,
  blob_uri     TEXT NOT NULL,
  filename     TEXT NOT NULL,
  uploaded_by  UUID REFERENCES "user"(id),
  expires_at   DATE,
  status       vendor_document_status NOT NULL DEFAULT 'pending',
  reviewed_by  UUID REFERENCES "user"(id),
  reviewed_at  TIMESTAMPTZ,
  notes        TEXT NOT NULL DEFAULT '',
  created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX vendor_document_vendor_idx ON vendor_document(vendor_id);
CREATE INDEX vendor_document_status_idx ON vendor_document(status);
CREATE INDEX vendor_document_expiring_idx ON vendor_document(expires_at) WHERE status = 'approved' AND expires_at IS NOT NULL;

CREATE TABLE anomaly_alert (
  id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  kind         TEXT NOT NULL,           -- 'on_time_rate_drop', 'document_expiring', etc.
  target_kind  TEXT NOT NULL,           -- 'vendor', 'order', etc.
  target_id    TEXT NOT NULL,
  severity     anomaly_severity NOT NULL DEFAULT 'medium',
  status       anomaly_status NOT NULL DEFAULT 'open',
  payload      JSONB NOT NULL DEFAULT '{}'::jsonb,
  evidence_uri TEXT[] NOT NULL DEFAULT '{}',
  triaged_at   TIMESTAMPTZ,
  triaged_by   UUID REFERENCES "user"(id),
  closed_at    TIMESTAMPTZ,
  closed_by    UUID REFERENCES "user"(id),
  notes        TEXT NOT NULL DEFAULT '',
  created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX anomaly_alert_target_idx ON anomaly_alert(target_kind, target_id);
CREATE INDEX anomaly_alert_status_idx ON anomaly_alert(status);
CREATE INDEX anomaly_alert_open_severity_idx ON anomaly_alert(severity, created_at DESC) WHERE status = 'open';

-- Dedup constraint: at most ONE open anomaly per (kind, target_kind, target_id) at any time
CREATE UNIQUE INDEX anomaly_alert_dedup_idx
  ON anomaly_alert(kind, target_kind, target_id) WHERE status = 'open';
