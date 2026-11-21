CREATE TABLE users (
    id            uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    email         text NOT NULL UNIQUE, -- stored lowercase
    password_hash text NOT NULL, -- bcrypt
    role          text NOT NULL CHECK (role IN ('admin', 'viewer')), -- viewer is read-only
    created_at    timestamptz NOT NULL DEFAULT now()
);

-- opaque bearer sessions; only the SHA-256 of the token is stored
CREATE TABLE sessions (
    token_hash text PRIMARY KEY,
    user_id    uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    expires_at timestamptz NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX sessions_expiry ON sessions (expires_at);
