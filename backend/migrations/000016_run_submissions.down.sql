DROP INDEX runs_submission_identity_unique;
ALTER TABLE runs DROP COLUMN model_deployment_id;
ALTER TABLE runs DROP COLUMN submission_sha256;
ALTER TABLE runs DROP COLUMN submitted_by;
ALTER TABLE runs DROP COLUMN submission_id;
