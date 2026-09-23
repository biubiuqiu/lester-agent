CREATE TABLE agent_files (
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
 agent_id uuid NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
 workspace_id uuid NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
 name text NOT NULL CHECK(length(name) BETWEEN 1 AND 180),
 content_type text NOT NULL DEFAULT 'application/octet-stream',
 size_bytes bigint NOT NULL CHECK(size_bytes BETWEEN 1 AND 10485760),
 object_key text NOT NULL UNIQUE,
 created_at timestamptz NOT NULL DEFAULT now(),
 UNIQUE(agent_id,name)
);
CREATE INDEX agent_files_workspace_idx ON agent_files(workspace_id,agent_id);
CREATE TABLE conversation_agent_files (
 conversation_id uuid NOT NULL REFERENCES conversations(id) ON DELETE CASCADE,
 file_id uuid NOT NULL,
 name text NOT NULL,
 content_type text NOT NULL,
 size_bytes bigint NOT NULL,
 object_key text NOT NULL,
 PRIMARY KEY(conversation_id,file_id)
);
ALTER TABLE conversations ADD COLUMN agent_files_installed boolean NOT NULL DEFAULT true;
