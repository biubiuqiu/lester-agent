-- Retain message content on rollback. The old API cannot interpret tool rows;
-- use a backup for a full application downgrade.
DROP TRIGGER messages_assign_seq ON messages;
DROP FUNCTION assign_message_seq();
ALTER TABLE runs DROP CONSTRAINT runs_input_message_conversation_fkey;
ALTER TABLE runs DROP COLUMN input_message_id;
ALTER TABLE runs DROP COLUMN context;
ALTER TABLE messages DROP CONSTRAINT messages_run_conversation_fkey;
DROP INDEX messages_run_tool_result_unique;
ALTER TABLE messages DROP CONSTRAINT messages_id_conversation_unique;
ALTER TABLE messages DROP CONSTRAINT messages_conversation_seq_unique;
ALTER TABLE messages DROP COLUMN seq;
ALTER TABLE messages DROP COLUMN run_id;
ALTER TABLE messages DROP COLUMN tool_calls;
ALTER TABLE messages DROP COLUMN tool_call_id;
ALTER TABLE messages DROP COLUMN tool_name;
ALTER TABLE runs DROP CONSTRAINT runs_id_conversation_unique;
ALTER TABLE conversations DROP COLUMN last_message_seq;
