ALTER TABLE run_interactions DROP CONSTRAINT run_interactions_kind_check;
ALTER TABLE run_interactions ADD CONSTRAINT run_interactions_kind_check CHECK (kind IN ('question','plan','automation'));
