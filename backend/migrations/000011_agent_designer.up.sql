ALTER TABLE conversations ADD COLUMN created_agent_id uuid REFERENCES agents(id) ON DELETE SET NULL;
CREATE INDEX conversations_created_agent_idx ON conversations(created_agent_id) WHERE created_agent_id IS NOT NULL;
