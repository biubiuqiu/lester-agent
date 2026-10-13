ALTER TABLE runs ADD COLUMN execution_mode text NOT NULL DEFAULT 'execute' CHECK (execution_mode IN ('execute','plan'));
ALTER TABLE runs ADD COLUMN approved_plan_digest text CHECK (approved_plan_digest ~ '^[0-9a-f]{64}$');
CREATE TABLE run_interactions (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id uuid NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    conversation_id uuid NOT NULL REFERENCES conversations(id) ON DELETE CASCADE,
    run_id uuid NOT NULL,
    tool_call_id text NOT NULL,
    kind text NOT NULL CHECK (kind IN ('question','plan')),
    request jsonb NOT NULL CHECK (jsonb_typeof(request)='object'),
    request_digest text NOT NULL CHECK (request_digest ~ '^[0-9a-f]{64}$'),
    status text NOT NULL DEFAULT 'pending' CHECK (status IN ('pending','answered','approved','rejected','changes_requested','cancelled')),
    response jsonb CHECK (jsonb_typeof(response)='object'),
    responded_by uuid REFERENCES users(id) ON DELETE SET NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    responded_at timestamptz,
    UNIQUE(run_id,tool_call_id),
    FOREIGN KEY(run_id,conversation_id) REFERENCES runs(id,conversation_id)
);
CREATE UNIQUE INDEX run_interactions_one_pending ON run_interactions(run_id) WHERE status='pending';
CREATE INDEX run_interactions_conversation ON run_interactions(conversation_id,created_at DESC);
