-- Stop API writes and active tasks before rollback. Preserve execution usage.
UPDATE run_permissions SET status='cancelled' WHERE status='reviewing';
UPDATE run_permissions SET decision='once' WHERE decision='full';
ALTER TABLE run_permissions DROP CONSTRAINT run_permissions_status_check;
ALTER TABLE run_permissions ADD CONSTRAINT run_permissions_status_check CHECK(status IN ('pending','approved','rejected','cancelled'));
ALTER TABLE run_permissions DROP CONSTRAINT run_permissions_decision_check;
ALTER TABLE run_permissions ADD CONSTRAINT run_permissions_decision_check CHECK(decision IN ('once','run','reject'));
-- Old APIs do not understand the new purpose; removing only those usage records
-- does not undo operations, change existing conversations or delete user files.
DELETE FROM model_request_usage WHERE purpose='permission_review';
ALTER TABLE model_request_usage DROP CONSTRAINT model_request_usage_purpose_check;
ALTER TABLE model_request_usage ADD CONSTRAINT model_request_usage_purpose_check CHECK(purpose IN ('execution','compaction'));
ALTER TABLE run_permissions DROP COLUMN reviewer_model_deployment_id, DROP COLUMN review_reason, DROP COLUMN review_decision, DROP COLUMN decision_source, DROP COLUMN permission_revision, DROP COLUMN review_token;
ALTER TABLE runs DROP COLUMN permission_revision;
