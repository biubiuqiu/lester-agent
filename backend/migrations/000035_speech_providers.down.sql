-- The older API cannot interpret a native provider added in 035. Require an
-- explicit administrator choice first, preserving its credential and receipts.
DO $$ BEGIN
 IF EXISTS(SELECT 1 FROM speech_settings WHERE provider NOT IN ('openai','elevenlabs')) THEN
  RAISE EXCEPTION 'Select OpenAI-compatible or ElevenLabs settings before rolling back speech provider support';
 END IF;
END $$;
ALTER TABLE speech_settings DROP CONSTRAINT speech_settings_provider_check;
ALTER TABLE speech_settings ADD CONSTRAINT speech_settings_provider_check
 CHECK(provider IN ('openai','elevenlabs'));
