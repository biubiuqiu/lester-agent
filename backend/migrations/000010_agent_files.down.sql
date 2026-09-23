DO $$ BEGIN
 IF EXISTS (SELECT 1 FROM conversation_agent_files) THEN
  RAISE EXCEPTION 'Cannot roll back Agent files while conversation snapshots exist';
 END IF;
END $$;
ALTER TABLE conversations DROP COLUMN agent_files_installed;
DROP TABLE conversation_agent_files;
DROP TABLE agent_files;
