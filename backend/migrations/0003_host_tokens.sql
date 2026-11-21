-- SHA-256 hex of the host's ingest token; the plaintext is never stored
ALTER TABLE hosts ADD COLUMN token_hash text UNIQUE;
