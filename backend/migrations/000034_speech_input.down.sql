-- Pending short-lived speech receipts are invalidated; task messages and files
-- are unaffected. No audio/provider effects can be undone by this rollback.
DROP TABLE speech_transcriptions;
DROP TABLE speech_settings;
