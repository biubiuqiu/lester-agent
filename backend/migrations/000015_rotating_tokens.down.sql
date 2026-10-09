-- Pair this rollback with the compatible API. Never extend an access credential
-- into a legacy 30-day credential.
DELETE FROM sessions;
ALTER TABLE auth_oauth_flows DROP COLUMN return_to;
DROP TABLE auth_refresh_tokens;
DROP TABLE auth_access_tokens;
