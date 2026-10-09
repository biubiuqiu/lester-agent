ALTER TABLE users
  ALTER COLUMN password_hash DROP NOT NULL,
  ADD COLUMN email_verified boolean NOT NULL DEFAULT false,
  ADD COLUMN email_verification_required boolean NOT NULL DEFAULT false,
  ADD COLUMN avatar_object_key text;

CREATE TABLE auth_identities (
  user_id uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  provider text NOT NULL CHECK (provider IN ('google', 'github')),
  subject text NOT NULL CHECK (length(subject) BETWEEN 1 AND 255),
  email text NOT NULL,
  avatar_url text NOT NULL DEFAULT '',
  created_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY(provider, subject),
  UNIQUE(user_id, provider)
);

CREATE TABLE auth_oauth_flows (
  token_hash bytea PRIMARY KEY,
  browser_hash bytea NOT NULL,
  provider text NOT NULL CHECK (provider IN ('google', 'github')),
  purpose text NOT NULL CHECK (purpose IN ('login', 'link')),
  user_id uuid REFERENCES users(id) ON DELETE CASCADE,
  session_hash bytea,
  verifier text NOT NULL,
  expires_at timestamptz NOT NULL,
  CHECK ((purpose = 'login' AND user_id IS NULL AND session_hash IS NULL)
      OR (purpose = 'link' AND user_id IS NOT NULL AND session_hash IS NOT NULL))
);
CREATE INDEX auth_oauth_flows_expiry_idx ON auth_oauth_flows(expires_at);
CREATE INDEX auth_oauth_flows_user_idx ON auth_oauth_flows(user_id) WHERE user_id IS NOT NULL;

CREATE TABLE auth_email_tokens (
  token_hash bytea PRIMARY KEY,
  user_id uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  purpose text NOT NULL CHECK (purpose IN ('verify', 'reset')),
  expires_at timestamptz NOT NULL,
  UNIQUE(user_id, purpose)
);
CREATE INDEX auth_email_tokens_expiry_idx ON auth_email_tokens(expires_at);
