-- Work-app authorization is independent of Lester sign-in identities.
CREATE TABLE app_connections (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id uuid NOT NULL,
    user_id uuid NOT NULL,
    provider text NOT NULL CHECK (provider IN ('github','gmail','x','feishu','slack')),
    status text NOT NULL DEFAULT 'disconnected' CHECK (status IN ('disconnected','connected','reauthorization_required')),
    account_id text NOT NULL DEFAULT '',
    account_name text NOT NULL DEFAULT '',
    scopes text[] NOT NULL DEFAULT '{}',
    token_ciphertext bytea,
    token_nonce bytea,
    expires_at timestamptz,
    refresh_pending boolean NOT NULL DEFAULT false,
    generation bigint NOT NULL DEFAULT 0,
    revision bigint NOT NULL DEFAULT 0,
    updated_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE(workspace_id,user_id,provider),
    FOREIGN KEY(workspace_id,user_id) REFERENCES workspace_members(workspace_id,user_id) ON DELETE CASCADE,
    CHECK ((token_ciphertext IS NULL) = (token_nonce IS NULL)),
    CHECK (status <> 'connected' OR token_ciphertext IS NOT NULL)
);
CREATE TABLE app_connection_oauth_flows (
    state_hash bytea PRIMARY KEY CHECK (octet_length(state_hash)=32),
    browser_hash bytea NOT NULL CHECK (octet_length(browser_hash)=32),
    connection_id uuid NOT NULL REFERENCES app_connections(id) ON DELETE CASCADE,
    session_hash bytea NOT NULL REFERENCES sessions(token_hash) ON DELETE CASCADE,
    generation bigint NOT NULL,
    return_to text NOT NULL DEFAULT '/app',
    verifier_ciphertext bytea NOT NULL,
    verifier_nonce bytea NOT NULL,
    expires_at timestamptz NOT NULL
);
CREATE INDEX app_connection_oauth_expiry_idx ON app_connection_oauth_flows(expires_at);
