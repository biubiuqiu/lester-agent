-- Submission identity survives lost responses, navigation and completed runs.
ALTER TABLE runs ADD COLUMN submission_id uuid;
ALTER TABLE runs ADD COLUMN submitted_by uuid REFERENCES users(id) ON DELETE SET NULL;
ALTER TABLE runs ADD COLUMN submission_sha256 text
    CHECK (submission_sha256 IS NULL OR submission_sha256 ~ '^[a-f0-9]{64}$');
-- Keep the accepted model identity even if its deployment is later removed.
ALTER TABLE runs ADD COLUMN model_deployment_id uuid;
CREATE UNIQUE INDEX runs_submission_identity_unique ON runs(conversation_id,submission_id)
    WHERE submission_id IS NOT NULL;
