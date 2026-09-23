DO $$ BEGIN
 IF EXISTS (SELECT 1 FROM conversations WHERE agent_slug NOT IN ('lester','franklin','michael','trevor')) THEN
  RAISE EXCEPTION 'Cannot roll back agents while custom-agent conversations exist';
 END IF;
END $$;
ALTER TABLE conversations DROP COLUMN agent_skills_installed;
ALTER TABLE conversations DROP COLUMN agent_skill_slugs;
ALTER TABLE conversations DROP COLUMN agent_instructions;
ALTER TABLE conversations DROP COLUMN agent_name;
ALTER TABLE conversations ADD CONSTRAINT conversations_agent_slug_check CHECK(agent_slug IN ('lester','franklin','michael','trevor'));
DROP TABLE agents;
