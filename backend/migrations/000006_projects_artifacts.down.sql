DROP TABLE artifacts;
DROP TRIGGER conversation_default_project ON conversations;
DROP FUNCTION assign_default_project();
ALTER TABLE conversations DROP CONSTRAINT conversations_workspace_id_unique;
ALTER TABLE conversations DROP COLUMN project_id, DROP COLUMN pinned;
DROP TRIGGER workspace_default_project ON workspaces;
DROP FUNCTION create_default_project();
DROP TABLE projects;
