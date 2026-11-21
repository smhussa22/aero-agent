CREATE TABLE hosts (
    id         text PRIMARY KEY, -- host_id reported by the agent
    mode       text NOT NULL DEFAULT 'observe' CHECK (mode IN ('observe', 'recommend', 'autonomous')),
    last_seen  timestamptz,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE alert_rules (
    id               uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    host_id          text REFERENCES hosts (id) ON DELETE CASCADE, -- NULL applies to every host
    name             text NOT NULL,
    metric           text NOT NULL,
    label            text NOT NULL DEFAULT '',
    operator         text NOT NULL CHECK (operator IN ('>', '>=', '<', '<=')),
    threshold        double precision NOT NULL,
    duration_seconds integer NOT NULL DEFAULT 0 CHECK (duration_seconds >= 0), -- must hold this long before firing
    severity         text NOT NULL CHECK (severity IN ('info', 'warning', 'critical')),
    enabled          boolean NOT NULL DEFAULT true,
    created_at       timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE incidents (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    host_id     text NOT NULL REFERENCES hosts (id) ON DELETE CASCADE,
    rule_id     uuid REFERENCES alert_rules (id) ON DELETE SET NULL,
    alert_id    text NOT NULL,
    status      text NOT NULL DEFAULT 'open' CHECK (status IN ('open', 'investigating', 'remediating', 'resolved', 'escalated')),
    summary     text, -- plain-English cause from the AI investigation
    opened_at   timestamptz NOT NULL DEFAULT now(),
    resolved_at timestamptz
);
CREATE INDEX incidents_host_open ON incidents (host_id, opened_at DESC);

CREATE TABLE remediation_actions (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    incident_id uuid NOT NULL REFERENCES incidents (id) ON DELETE CASCADE,
    action_id   text NOT NULL, -- allowlisted action name
    params      jsonb NOT NULL DEFAULT '{}',
    rationale   text,
    dangerous   boolean NOT NULL DEFAULT false,
    dry_run     boolean NOT NULL DEFAULT false,
    status      text NOT NULL DEFAULT 'proposed' CHECK (status IN (
        'proposed', 'awaiting_approval', 'approved', 'rejected', 'running',
        'succeeded', 'failed', 'verified', 'escalated')),
    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE approvals (
    id         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    action_id  uuid NOT NULL REFERENCES remediation_actions (id) ON DELETE CASCADE,
    approved   boolean NOT NULL,
    decided_by text NOT NULL,
    decided_at timestamptz NOT NULL DEFAULT now()
);

-- append-only: rows can be added, never changed or removed
CREATE TABLE audit_log (
    id          bigserial PRIMARY KEY,
    at          timestamptz NOT NULL DEFAULT now(),
    actor       text NOT NULL, -- user, agent, or system
    event       text NOT NULL,
    incident_id uuid, -- no FK so history survives incident deletion
    details     jsonb NOT NULL DEFAULT '{}'
);

CREATE FUNCTION audit_log_immutable() RETURNS trigger AS $$
BEGIN
    RAISE EXCEPTION 'audit_log is append-only';
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER audit_log_no_update_delete
    BEFORE UPDATE OR DELETE ON audit_log
    FOR EACH ROW EXECUTE FUNCTION audit_log_immutable();

CREATE TRIGGER audit_log_no_truncate
    BEFORE TRUNCATE ON audit_log
    FOR EACH STATEMENT EXECUTE FUNCTION audit_log_immutable();
