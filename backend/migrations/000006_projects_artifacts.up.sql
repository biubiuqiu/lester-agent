CREATE TABLE projects (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id uuid NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    name text NOT NULL CHECK (length(btrim(name)) BETWEEN 1 AND 80),
    is_default boolean NOT NULL DEFAULT false,
    pinned boolean NOT NULL DEFAULT false,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE(workspace_id, id)
);
CREATE UNIQUE INDEX projects_default_idx ON projects(workspace_id) WHERE is_default;
CREATE INDEX projects_workspace_idx ON projects(workspace_id, pinned DESC, created_at);
INSERT INTO projects(workspace_id,name,is_default) SELECT id,'默认项目',true FROM workspaces;
CREATE FUNCTION create_default_project() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    INSERT INTO projects(workspace_id,name,is_default) VALUES(NEW.id,'默认项目',true);
    RETURN NEW;
END $$;
CREATE TRIGGER workspace_default_project AFTER INSERT ON workspaces FOR EACH ROW EXECUTE FUNCTION create_default_project();
ALTER TABLE conversations ADD COLUMN project_id uuid, ADD COLUMN pinned boolean NOT NULL DEFAULT false;
UPDATE conversations c SET project_id=p.id FROM projects p WHERE p.workspace_id=c.workspace_id AND p.is_default;
ALTER TABLE conversations ALTER COLUMN project_id SET NOT NULL;
ALTER TABLE conversations ADD CONSTRAINT conversations_project_fk FOREIGN KEY(workspace_id,project_id) REFERENCES projects(workspace_id,id);
ALTER TABLE conversations ADD CONSTRAINT conversations_workspace_id_unique UNIQUE(workspace_id,id);
CREATE FUNCTION assign_default_project() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.project_id IS NULL THEN
        SELECT id INTO NEW.project_id FROM projects WHERE workspace_id=NEW.workspace_id AND is_default;
    END IF;
    RETURN NEW;
END $$;
CREATE TRIGGER conversation_default_project BEFORE INSERT ON conversations FOR EACH ROW EXECUTE FUNCTION assign_default_project();
CREATE INDEX conversations_project_idx ON conversations(workspace_id,project_id,pinned DESC,updated_at DESC);

CREATE TABLE artifacts (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id uuid NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    conversation_id uuid NOT NULL,
    name text NOT NULL CHECK(length(btrim(name)) BETWEEN 1 AND 120),
    source_path text NOT NULL,
    entry_path text NOT NULL,
    version uuid NOT NULL,
    status text NOT NULL DEFAULT 'published' CHECK(status IN ('published','unpublished')),
    manifest jsonb NOT NULL,
    file_count integer NOT NULL CHECK(file_count > 0),
    size_bytes bigint NOT NULL CHECK(size_bytes >= 0),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    FOREIGN KEY(workspace_id,conversation_id) REFERENCES conversations(workspace_id,id) ON DELETE CASCADE
);
CREATE INDEX artifacts_workspace_idx ON artifacts(workspace_id,created_at DESC);
CREATE INDEX artifacts_conversation_idx ON artifacts(conversation_id);
