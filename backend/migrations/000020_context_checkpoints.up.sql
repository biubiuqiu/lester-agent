CREATE TABLE conversation_context_checkpoints (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id uuid NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    conversation_id uuid NOT NULL REFERENCES conversations(id) ON DELETE CASCADE,
    run_id uuid NOT NULL,
    previous_id uuid REFERENCES conversation_context_checkpoints(id),
    through_seq bigint NOT NULL CHECK (through_seq > 0),
    retained_message_ids uuid[] NOT NULL DEFAULT '{}',
    source_message_ids uuid[] NOT NULL CHECK (cardinality(source_message_ids) > 0),
    source_digest text NOT NULL CHECK (source_digest ~ '^[0-9a-f]{64}$'),
    source_count integer NOT NULL CHECK (source_count > 0),
    model text NOT NULL,
    policy_version text NOT NULL,
    summary jsonb NOT NULL CHECK (jsonb_typeof(summary) = 'object'),
    estimated_before integer NOT NULL CHECK (estimated_before > 0),
    estimated_after integer NOT NULL CHECK (estimated_after > 0 AND estimated_after < estimated_before),
    created_at timestamptz NOT NULL DEFAULT now(),
    FOREIGN KEY (run_id, conversation_id) REFERENCES runs(id, conversation_id)
);
CREATE INDEX conversation_context_checkpoints_latest ON conversation_context_checkpoints(conversation_id, source_count DESC);
