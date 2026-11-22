-- Multi-tenancy: every piece of customer data belongs to exactly one organization.
-- A host's id is now assigned by the server (a uuid) and its name is only a label.
CREATE TABLE orgs (
    id         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    name       text NOT NULL CHECK (char_length(name) BETWEEN 1 AND 80),
    created_at timestamptz NOT NULL DEFAULT now()
);

-- an install that already has data keeps working: all of it moves into one default organization
INSERT INTO orgs (name)
SELECT 'Default'
WHERE EXISTS (SELECT 1 FROM users) OR EXISTS (SELECT 1 FROM hosts)
   OR EXISTS (SELECT 1 FROM alert_rules) OR EXISTS (SELECT 1 FROM incidents);

ALTER TABLE users       ADD COLUMN org_id uuid REFERENCES orgs (id) ON DELETE CASCADE;
ALTER TABLE hosts       ADD COLUMN org_id uuid REFERENCES orgs (id) ON DELETE CASCADE, ADD COLUMN name text;
ALTER TABLE alert_rules ADD COLUMN org_id uuid REFERENCES orgs (id) ON DELETE CASCADE;
ALTER TABLE incidents   ADD COLUMN org_id uuid REFERENCES orgs (id) ON DELETE CASCADE;
-- no foreign key: the audit trail outlives what it describes
ALTER TABLE audit_log   ADD COLUMN org_id uuid;

UPDATE users       SET org_id = (SELECT id FROM orgs LIMIT 1);
UPDATE hosts       SET org_id = (SELECT id FROM orgs LIMIT 1), name = id;
UPDATE alert_rules SET org_id = (SELECT id FROM orgs LIMIT 1);
UPDATE incidents i SET org_id = h.org_id FROM hosts h WHERE h.id = i.host_id;

-- the audit log is append-only by trigger, so the backfill has to step around it, once
ALTER TABLE audit_log DISABLE TRIGGER audit_log_no_update_delete;
UPDATE audit_log SET org_id = (SELECT id FROM orgs LIMIT 1);
ALTER TABLE audit_log ENABLE TRIGGER audit_log_no_update_delete;

ALTER TABLE users       ALTER COLUMN org_id SET NOT NULL;
ALTER TABLE hosts       ALTER COLUMN org_id SET NOT NULL, ALTER COLUMN name SET NOT NULL;
ALTER TABLE alert_rules ALTER COLUMN org_id SET NOT NULL;
ALTER TABLE incidents   ALTER COLUMN org_id SET NOT NULL;

ALTER TABLE hosts ADD CONSTRAINT hosts_name_len CHECK (char_length(name) BETWEEN 1 AND 100);
CREATE UNIQUE INDEX hosts_org_name ON hosts (org_id, name);
CREATE INDEX users_org ON users (org_id);
CREATE INDEX alert_rules_org ON alert_rules (org_id);
CREATE INDEX incidents_org_opened ON incidents (org_id, opened_at DESC);
CREATE INDEX audit_log_org ON audit_log (org_id, id DESC);
