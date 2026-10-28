-- Extend supported wire protocols without rewriting existing configuration,
-- credentials or short-lived transcription receipts.
ALTER TABLE speech_settings DROP CONSTRAINT speech_settings_provider_check;
ALTER TABLE speech_settings ADD CONSTRAINT speech_settings_provider_check
 CHECK(provider IN ('openai','elevenlabs','deepgram','mistral'));
