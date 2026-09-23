CREATE TABLE context_entries (
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
 workspace_id uuid NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
 title text NOT NULL CHECK(length(btrim(title)) BETWEEN 1 AND 80),
 description text NOT NULL DEFAULT '' CHECK(length(description)<=240),
 content text NOT NULL CHECK(length(btrim(content)) BETWEEN 1 AND 20000),
 version integer NOT NULL DEFAULT 1,
 created_at timestamptz NOT NULL DEFAULT now(),
 updated_at timestamptz NOT NULL DEFAULT now(),
 UNIQUE(workspace_id,title)
);
CREATE INDEX context_entries_workspace_updated_idx ON context_entries(workspace_id,updated_at DESC);
