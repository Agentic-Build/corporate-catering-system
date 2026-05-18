-- Document resupply: a replacement document links to the one it supersedes.
-- "Superseded" is derived (a row is superseded iff another row's `supersedes`
-- points at it), so no new status enum value is needed.
ALTER TABLE vendor_document ADD COLUMN supersedes UUID REFERENCES vendor_document(id);
CREATE INDEX vendor_document_supersedes_idx ON vendor_document(supersedes) WHERE supersedes IS NOT NULL;
