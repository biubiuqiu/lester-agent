ALTER TABLE app_connections ADD COLUMN github_code_enabled boolean NOT NULL DEFAULT false;
ALTER TABLE app_connection_oauth_flows ADD COLUMN github_code_enabled boolean NOT NULL DEFAULT false;

CREATE TABLE github_checkouts (
 id uuid PRIMARY KEY,
 workspace_id uuid NOT NULL,
 user_id uuid NOT NULL,
 conversation_id uuid NOT NULL REFERENCES conversations(id) ON DELETE CASCADE,
 run_id uuid NOT NULL REFERENCES runs(id) ON DELETE CASCADE,
 tool_call_id text NOT NULL,
 connection_id uuid NOT NULL REFERENCES app_connections(id) ON DELETE CASCADE,
 grant_generation bigint NOT NULL,
 account_id text NOT NULL,
 repository text NOT NULL,
 directory text NOT NULL,
 source_ref text NOT NULL,
 base_sha text NOT NULL,
 tree_sha text NOT NULL,
 local_head text NOT NULL DEFAULT '',
 manifest jsonb NOT NULL,
 status text NOT NULL CHECK(status IN ('preparing','ready','failed')),
 created_at timestamptz NOT NULL DEFAULT now(),
 UNIQUE(conversation_id,directory),
 UNIQUE(run_id,tool_call_id),
 FOREIGN KEY(workspace_id,user_id) REFERENCES workspace_members(workspace_id,user_id) ON DELETE CASCADE
);
CREATE INDEX github_checkouts_owner_idx ON github_checkouts(workspace_id,user_id,conversation_id);
CREATE TABLE github_publish_proposals (
 id uuid PRIMARY KEY,
 checkout_id uuid NOT NULL REFERENCES github_checkouts(id) ON DELETE CASCADE,
 workspace_id uuid NOT NULL,
 user_id uuid NOT NULL,
 conversation_id uuid NOT NULL REFERENCES conversations(id) ON DELETE CASCADE,
 run_id uuid NOT NULL REFERENCES runs(id) ON DELETE CASCADE,
 tool_call_id text NOT NULL,
 digest text NOT NULL,
 repository text NOT NULL,
 branch text NOT NULL,
 summary text NOT NULL,
 payload_ciphertext bytea NOT NULL,
 payload_nonce bytea NOT NULL,
 status text NOT NULL DEFAULT 'prepared' CHECK(status IN ('prepared','publishing','pushed','completed','unknown','failed')),
 stage text NOT NULL DEFAULT 'prepared',
 commit_sha text NOT NULL DEFAULT '',
 commit_url text NOT NULL DEFAULT '',
 pr_url text NOT NULL DEFAULT '',
 created_at timestamptz NOT NULL DEFAULT now(),
 expires_at timestamptz NOT NULL,
 updated_at timestamptz NOT NULL DEFAULT now(),
 UNIQUE(run_id,tool_call_id),
 FOREIGN KEY(workspace_id,user_id) REFERENCES workspace_members(workspace_id,user_id) ON DELETE CASCADE
);
CREATE INDEX github_publish_owner_idx ON github_publish_proposals(workspace_id,user_id,conversation_id,created_at DESC);
