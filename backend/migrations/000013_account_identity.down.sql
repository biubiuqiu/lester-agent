-- Removing identity-only accounts would lose user data; require an explicit
-- account recovery/migration plan before rolling back their login mechanism.
DO $$ BEGIN
  IF EXISTS (SELECT 1 FROM users WHERE password_hash IS NULL) THEN
    RAISE EXCEPTION 'Cannot roll back while identity-only accounts exist; set passwords or migrate them first';
  END IF;
END $$;
DROP TABLE auth_email_tokens;
DROP TABLE auth_oauth_flows;
DROP TABLE auth_identities;
ALTER TABLE users
  DROP COLUMN avatar_object_key,
  DROP COLUMN email_verification_required,
  DROP COLUMN email_verified,
  ALTER COLUMN password_hash SET NOT NULL;
