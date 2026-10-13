CREATE TABLE run_inputs (
    id uuid PRIMARY KEY,
    workspace_id uuid NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    conversation_id uuid NOT NULL REFERENCES conversations(id) ON DELETE CASCADE,
    after_run_id uuid NOT NULL,
    submitted_by uuid REFERENCES users(id) ON DELETE SET NULL,
    kind text NOT NULL CHECK (kind IN ('adjust','next')),
    request jsonb NOT NULL CHECK (jsonb_typeof(request)='object'),
    request_digest text NOT NULL CHECK (request_digest ~ '^[0-9a-f]{64}$'),
    content text NOT NULL,
    metadata jsonb NOT NULL DEFAULT '{}',
    model_deployment_id uuid,
    execution_mode text NOT NULL DEFAULT 'execute' CHECK (execution_mode IN ('execute','plan')),
    status text NOT NULL CHECK (status IN ('pending','queued','applied','held','started','cancelled')),
    accepted_order bigint GENERATED ALWAYS AS IDENTITY UNIQUE,
    revision bigint NOT NULL DEFAULT 1,
    message_id uuid,
    started_run_id uuid,
    resume_authorized boolean NOT NULL DEFAULT false,
    held_reason text,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    FOREIGN KEY(after_run_id,conversation_id) REFERENCES runs(id,conversation_id),
    FOREIGN KEY(started_run_id,conversation_id) REFERENCES runs(id,conversation_id),
    FOREIGN KEY(message_id,conversation_id) REFERENCES messages(id,conversation_id)
);
CREATE INDEX run_inputs_conversation_order ON run_inputs(conversation_id,accepted_order);
CREATE INDEX run_inputs_pending ON run_inputs(after_run_id,accepted_order) WHERE status='pending';
CREATE INDEX run_inputs_queue ON run_inputs(conversation_id,accepted_order) WHERE status IN ('queued','held');
