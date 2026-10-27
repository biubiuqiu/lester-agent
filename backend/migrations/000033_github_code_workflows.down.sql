-- Never discard uncertain external writes or a still-consumable approval.
DO $$ BEGIN
 IF EXISTS(SELECT 1 FROM github_publish_proposals WHERE status IN ('publishing','unknown') OR (stage='pr_intent' AND pr_url='')) THEN
  RAISE EXCEPTION 'Resolve GitHub publishing/unknown receipts before rollback';
 END IF;
 IF EXISTS(SELECT 1 FROM run_permissions WHERE tool='github_publish' AND (status IN ('pending','reviewing') OR (status='approved' AND consumed_at IS NULL))) THEN
  RAISE EXCEPTION 'Resolve pending GitHub publication approvals before rollback';
 END IF;
END $$;
-- This removes local receipts only; it cannot undo GitHub commits or PRs.
DROP TABLE github_publish_proposals;
DROP TABLE github_checkouts;
ALTER TABLE app_connection_oauth_flows DROP COLUMN github_code_enabled;
ALTER TABLE app_connections DROP COLUMN github_code_enabled;
