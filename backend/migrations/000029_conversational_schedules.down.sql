-- Refuse to silently remove durable schedule confirmations or their audit trail.
-- Removing an interaction never undoes an already applied schedule or task.
DO $$ BEGIN
  IF EXISTS (SELECT 1 FROM run_interactions WHERE kind='automation') THEN
    RAISE EXCEPTION 'Conversational schedule interactions exist; archive them explicitly before rollback. Applied automations and task effects are not undone.';
  END IF;
END $$;
ALTER TABLE run_interactions DROP CONSTRAINT run_interactions_kind_check;
ALTER TABLE run_interactions ADD CONSTRAINT run_interactions_kind_check CHECK (kind IN ('question','plan'));
