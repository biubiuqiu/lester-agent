-- Retire the former 30-day single credential. Users sign in once after upgrade;
-- no account, profile, workspace or file is removed.
DELETE FROM sessions;
ALTER TABLE auth_oauth_flows ADD COLUMN return_to text NOT NULL DEFAULT '/app';
CREATE TABLE auth_access_tokens (
    token_hash bytea PRIMARY KEY CHECK (octet_length(token_hash)=32),
    session_id uuid NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
    expires_at timestamptz NOT NULL
);
CREATE INDEX auth_access_tokens_session_idx ON auth_access_tokens(session_id);
CREATE INDEX auth_access_tokens_expiry_idx ON auth_access_tokens(expires_at);
CREATE TABLE auth_refresh_tokens (
    token_hash bytea PRIMARY KEY CHECK (octet_length(token_hash)=32),
    session_id uuid NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
    expires_at timestamptz NOT NULL,
    consumed_at timestamptz
);
CREATE UNIQUE INDEX auth_refresh_tokens_current_idx ON auth_refresh_tokens(session_id) WHERE consumed_at IS NULL;
CREATE INDEX auth_refresh_tokens_session_idx ON auth_refresh_tokens(session_id);
CREATE INDEX auth_refresh_tokens_expiry_idx ON auth_refresh_tokens(expires_at);
