ALTER TABLE users ADD COLUMN role text NOT NULL DEFAULT 'member' CHECK (role IN ('member','admin'));
ALTER TABLE users ADD COLUMN disabled boolean NOT NULL DEFAULT false;
-- Reserved scope for administrator-managed models; no user membership is created.
INSERT INTO workspaces(id,name,kind) VALUES ('00000000-0000-0000-0000-000000000001','System models','system');
ALTER TABLE model_deployments ADD COLUMN enabled boolean NOT NULL DEFAULT true;
