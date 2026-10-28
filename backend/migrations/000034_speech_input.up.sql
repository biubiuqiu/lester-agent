-- Deployment-wide speech transcription is an explicit administrator choice.
CREATE TABLE speech_settings (
 singleton boolean PRIMARY KEY DEFAULT true CHECK(singleton),
 enabled boolean NOT NULL DEFAULT false,
 provider text NOT NULL DEFAULT 'openai' CHECK(provider IN ('openai','elevenlabs')),
 endpoint text NOT NULL DEFAULT 'https://api.openai.com/v1',
 model_id text NOT NULL DEFAULT 'gpt-4o-transcribe',
 credential_ciphertext bytea,
 credential_nonce bytea,
 revision bigint NOT NULL DEFAULT 0,
 updated_at timestamptz NOT NULL DEFAULT now(),
 CHECK((credential_ciphertext IS NULL)=(credential_nonce IS NULL)),
 CHECK(NOT enabled OR credential_ciphertext IS NOT NULL)
);
INSERT INTO speech_settings(singleton) VALUES(true);

-- No audio is retained. Short-lived text receipts prevent a lost HTTP response
-- from silently charging a second transcription request.
CREATE TABLE speech_transcriptions (
 id uuid PRIMARY KEY,
 workspace_id uuid NOT NULL,
 user_id uuid NOT NULL,
 request_digest text NOT NULL,
 settings_revision bigint NOT NULL,
 status text NOT NULL CHECK(status IN ('processing','completed','failed','unknown')),
 text_content text NOT NULL DEFAULT '',
 error_message text NOT NULL DEFAULT '',
 created_at timestamptz NOT NULL DEFAULT now(),
 expires_at timestamptz NOT NULL DEFAULT now()+interval '5 minutes',
 FOREIGN KEY(workspace_id,user_id) REFERENCES workspace_members(workspace_id,user_id) ON DELETE CASCADE
);
CREATE INDEX speech_transcriptions_actor_time_idx ON speech_transcriptions(user_id,created_at DESC);
CREATE INDEX speech_transcriptions_expiry_idx ON speech_transcriptions(expires_at);
