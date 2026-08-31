-- Preserve legacy text history in its previous (created_at,id) order.
-- Missing historical tool results cannot be reconstructed.
ALTER TABLE conversations ADD COLUMN last_message_seq bigint NOT NULL DEFAULT 0;
ALTER TABLE runs ADD COLUMN input_message_id uuid;
ALTER TABLE runs ADD COLUMN context jsonb NOT NULL DEFAULT '{}';
ALTER TABLE runs ADD CONSTRAINT runs_id_conversation_unique UNIQUE (id, conversation_id);
ALTER TABLE messages ADD COLUMN seq bigint;
ALTER TABLE messages ADD COLUMN run_id uuid;
ALTER TABLE messages ADD COLUMN tool_calls jsonb NOT NULL DEFAULT '[]'
    CHECK (jsonb_typeof(tool_calls) = 'array');
ALTER TABLE messages ADD COLUMN tool_call_id text;
ALTER TABLE messages ADD COLUMN tool_name text;
ALTER TABLE messages ADD CONSTRAINT messages_run_conversation_fkey
    FOREIGN KEY (run_id, conversation_id) REFERENCES runs(id, conversation_id);
WITH ordered AS (
    SELECT id, row_number() OVER (PARTITION BY conversation_id ORDER BY created_at,id) AS seq
    FROM messages
)
UPDATE messages m SET seq = ordered.seq FROM ordered WHERE m.id = ordered.id;
UPDATE conversations c SET last_message_seq = totals.seq
FROM (SELECT conversation_id, max(seq) AS seq FROM messages GROUP BY conversation_id) totals
WHERE c.id = totals.conversation_id;
ALTER TABLE messages ALTER COLUMN seq SET NOT NULL;
ALTER TABLE messages ADD CONSTRAINT messages_conversation_seq_unique UNIQUE (conversation_id, seq);
ALTER TABLE messages ADD CONSTRAINT messages_id_conversation_unique UNIQUE (id, conversation_id);
ALTER TABLE runs ADD CONSTRAINT runs_input_message_conversation_fkey
    FOREIGN KEY (input_message_id, conversation_id) REFERENCES messages(id, conversation_id);
CREATE UNIQUE INDEX messages_run_tool_result_unique ON messages(run_id,tool_call_id)
    WHERE role = 'tool';
-- The conversation row lock serializes allocation and commit within a conversation.
CREATE FUNCTION assign_message_seq() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    UPDATE conversations SET last_message_seq = last_message_seq + 1
    WHERE id = NEW.conversation_id RETURNING last_message_seq INTO NEW.seq;
    RETURN NEW;
END;
$$;
CREATE TRIGGER messages_assign_seq BEFORE INSERT ON messages
    FOR EACH ROW EXECUTE FUNCTION assign_message_seq();
