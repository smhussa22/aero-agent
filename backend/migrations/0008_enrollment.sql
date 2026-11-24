-- A short-lived, single-use code an admin hands to one new server. The server trades it for its own host token,
-- so the long-lived token is never typed into a shell history or pasted around. Only a hash is stored.
CREATE TABLE enroll_codes (
    code_hash  text PRIMARY KEY,
    org_id     uuid NOT NULL REFERENCES orgs (id) ON DELETE CASCADE,
    created_by uuid REFERENCES users (id) ON DELETE SET NULL,
    expires_at timestamptz NOT NULL,
    used_at    timestamptz,
    created_at timestamptz NOT NULL DEFAULT now()
);
