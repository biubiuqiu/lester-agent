CREATE TABLE native_signin_handoffs (
  token_hash BYTEA PRIMARY KEY CHECK (octet_length(token_hash)=32),
  user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  session_id UUID NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
  challenge TEXT NOT NULL CHECK (length(challenge)=43),
  expires_at TIMESTAMPTZ NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX native_signin_handoffs_user_idx ON native_signin_handoffs(user_id);
