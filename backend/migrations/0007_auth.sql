-- Sign-in moves to an identity provider (OIDC). We keep no passwords for new users; the provider owns them.
ALTER TABLE users ALTER COLUMN password_hash DROP NOT NULL;
ALTER TABLE users ADD COLUMN idp_issuer text, ADD COLUMN idp_subject text;
CREATE UNIQUE INDEX users_idp ON users (idp_issuer, idp_subject) WHERE idp_subject IS NOT NULL;

-- one row per sign-in attempt in flight: the browser is bound to it by a cookie, and it can be used once
CREATE TABLE auth_flows (
    state_hash    text PRIMARY KEY,
    nonce         text NOT NULL,
    code_verifier text NOT NULL,
    expires_at    timestamptz NOT NULL
);

-- an invitation is matched to a verified email address, so there is no secret link to leak
CREATE TABLE invites (
    id         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id     uuid NOT NULL REFERENCES orgs (id) ON DELETE CASCADE,
    email      text NOT NULL,
    role       text NOT NULL CHECK (role IN ('admin', 'viewer')),
    created_by uuid REFERENCES users (id) ON DELETE SET NULL,
    expires_at timestamptz NOT NULL,
    accepted_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX invites_pending ON invites (org_id, email) WHERE accepted_at IS NULL;
CREATE INDEX invites_email ON invites (email) WHERE accepted_at IS NULL;

-- The audit log stays append-only, with exactly one exception: deleting a whole organization's data on request.
-- That path names the organization first (delete_org below); nothing else can delete a row.
CREATE OR REPLACE FUNCTION audit_log_immutable() RETURNS trigger AS $$
BEGIN
    IF TG_OP = 'DELETE' THEN
        IF OLD.org_id IS NOT NULL AND OLD.org_id::text = current_setting('aero.purge_org', true) THEN
            RETURN OLD;
        END IF;
    END IF;
    RAISE EXCEPTION 'audit_log is append-only';
END;
$$ LANGUAGE plpgsql;

-- Removes an organization and everything of its: hosts and their metrics, rules, incidents, actions, users,
-- sessions, invites, and its audit entries.
CREATE FUNCTION delete_org(p_org uuid) RETURNS void AS $$
BEGIN
    PERFORM set_config('aero.purge_org', p_org::text, true); -- local to this transaction
    DELETE FROM metrics WHERE host_id IN (SELECT id FROM hosts WHERE org_id = p_org);
    DELETE FROM audit_log WHERE org_id = p_org;
    DELETE FROM orgs WHERE id = p_org; -- cascades to the rest
END;
$$ LANGUAGE plpgsql;
