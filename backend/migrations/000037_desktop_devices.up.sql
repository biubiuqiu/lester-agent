CREATE TABLE desktop_devices (
  id uuid PRIMARY KEY,
  workspace_id uuid NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
  user_id uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  session_id uuid REFERENCES sessions(id) ON DELETE SET NULL,
  name text NOT NULL CHECK(length(name) BETWEEN 1 AND 100),
  platform text NOT NULL CHECK(platform IN ('darwin','win32','linux')),
  token_hash bytea NOT NULL UNIQUE CHECK(octet_length(token_hash)=32),
  generation bigint NOT NULL DEFAULT 1,
  token_expires_at timestamptz NOT NULL DEFAULT now()+interval '30 days',
  revoked_at timestamptz,
  last_seen_at timestamptz,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX desktop_devices_owner_idx ON desktop_devices(workspace_id,user_id);

CREATE TABLE desktop_device_models (
  device_id uuid NOT NULL REFERENCES desktop_devices(id) ON DELETE CASCADE,
  model_key text NOT NULL CHECK(length(model_key) BETWEEN 1 AND 128),
  connection_id uuid NOT NULL UNIQUE REFERENCES model_connections(id) ON DELETE CASCADE,
  deployment_id uuid NOT NULL UNIQUE REFERENCES model_deployments(id) ON DELETE CASCADE,
  PRIMARY KEY(device_id,model_key)
);

CREATE TABLE desktop_conversations (
  id uuid PRIMARY KEY,
  device_id uuid NOT NULL REFERENCES desktop_devices(id) ON DELETE CASCADE,
  workspace_id uuid NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
  user_id uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  project_id uuid,
  project_name text NOT NULL DEFAULT '' CHECK(length(project_name)<=120),
  title text NOT NULL CHECK(length(title) BETWEEN 1 AND 240),
  status text NOT NULL CHECK(status IN ('idle','running','waiting','completed','failed','cancelled','interrupted','unknown')),
  revision bigint NOT NULL CHECK(revision>0),
  updated_at timestamptz NOT NULL,
  synced_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX desktop_conversations_owner_idx ON desktop_conversations(workspace_id,user_id,updated_at DESC,id DESC);
