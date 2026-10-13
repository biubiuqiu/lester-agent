CREATE TABLE command_tasks (
    id uuid PRIMARY KEY,
    workspace_id uuid NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    conversation_id uuid NOT NULL REFERENCES conversations(id) ON DELETE CASCADE,
    run_id uuid NOT NULL REFERENCES runs(id) ON DELETE CASCADE,
    sandbox_ref text NOT NULL,
    handle jsonb NOT NULL,
    snapshot jsonb NOT NULL,
    status text NOT NULL CHECK (status IN ('starting','running','stopping','exited','timed_out','cancelled','unknown')),
    revision bigint NOT NULL DEFAULT 1,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX command_tasks_conversation ON command_tasks(workspace_id,conversation_id,created_at DESC);
CREATE INDEX command_tasks_active ON command_tasks(sandbox_ref) WHERE status IN ('starting','running','stopping','unknown');
