-- Acknowledgements belong to the human reader, independently of task execution
-- and personal-assistant retrieval. Never infer historical reading activity.
CREATE TABLE conversation_read_states (
    workspace_id uuid NOT NULL,
    user_id uuid NOT NULL,
    conversation_id uuid NOT NULL,
    last_read_seq bigint NOT NULL DEFAULT 0 CHECK(last_read_seq >= 0),
    updated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY(workspace_id,user_id,conversation_id),
    FOREIGN KEY(workspace_id,user_id) REFERENCES workspace_members(workspace_id,user_id) ON DELETE CASCADE,
    FOREIGN KEY(workspace_id,conversation_id) REFERENCES conversations(workspace_id,id) ON DELETE CASCADE
);

CREATE INDEX messages_public_replies_idx ON messages(conversation_id,seq DESC)
 WHERE role='assistant' AND content ~ '[^[:space:]]'
   AND (tool_calls IS NULL OR tool_calls='[]'::jsonb)
   AND COALESCE(metadata->>'incomplete','false')!='true';

-- Tracking starts at this upgrade. Prior replies have no trustworthy read
-- history, so establish a baseline instead of declaring all history unread.
INSERT INTO conversation_read_states(workspace_id,user_id,conversation_id,last_read_seq)
SELECT c.workspace_id,c.created_by,c.id,COALESCE(m.seq,0)
FROM conversations c
JOIN workspace_members member ON member.workspace_id=c.workspace_id AND member.user_id=c.created_by
LEFT JOIN LATERAL (
    SELECT seq FROM messages WHERE conversation_id=c.id
      AND role='assistant' AND content ~ '[^[:space:]]'
      AND (tool_calls IS NULL OR tool_calls='[]'::jsonb)
      AND COALESCE(metadata->>'incomplete','false')!='true'
    ORDER BY seq DESC LIMIT 1
) m ON true;
