CREATE TABLE agents (
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
 workspace_id uuid NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
 name text NOT NULL CHECK (length(btrim(name)) BETWEEN 1 AND 80),
 description text NOT NULL DEFAULT '' CHECK (length(description) <= 500),
 instructions text NOT NULL CHECK (length(btrim(instructions)) BETWEEN 1 AND 20000),
 skill_slugs text[] NOT NULL DEFAULT '{}',
 version integer NOT NULL DEFAULT 1,
 created_at timestamptz NOT NULL DEFAULT now(),
 updated_at timestamptz NOT NULL DEFAULT now(),
 UNIQUE(workspace_id,name)
);
CREATE INDEX agents_workspace_updated_idx ON agents(workspace_id,updated_at DESC);
ALTER TABLE conversations DROP CONSTRAINT conversations_agent_slug_check;
ALTER TABLE conversations ADD COLUMN agent_name text NOT NULL DEFAULT '';
ALTER TABLE conversations ADD COLUMN agent_instructions text NOT NULL DEFAULT '';
ALTER TABLE conversations ADD COLUMN agent_skill_slugs text[] NOT NULL DEFAULT '{}';
ALTER TABLE conversations ADD COLUMN agent_skills_installed boolean NOT NULL DEFAULT true;
