ALTER TABLE runs ADD COLUMN permission_mode text NOT NULL DEFAULT 'standard' CHECK(permission_mode IN ('standard','auto','full'));
ALTER TABLE run_inputs ADD COLUMN permission_mode text NOT NULL DEFAULT 'standard' CHECK(permission_mode IN ('standard','auto','full'));
CREATE TABLE run_permissions (
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
 workspace_id uuid NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
 conversation_id uuid NOT NULL REFERENCES conversations(id) ON DELETE CASCADE,
 run_id uuid NOT NULL,
 tool_call_id text NOT NULL,
 tool text NOT NULL,
 request_digest text NOT NULL CHECK(request_digest ~ '^[0-9a-f]{64}$'),
 summary jsonb NOT NULL,
 allow_run boolean NOT NULL DEFAULT false,
 status text NOT NULL DEFAULT 'pending' CHECK(status IN ('pending','approved','rejected','cancelled')),
 decision text CHECK(decision IN ('once','run','reject')),
 consumed_at timestamptz,
 responded_by uuid REFERENCES users(id) ON DELETE SET NULL,
 created_at timestamptz NOT NULL DEFAULT now(),
 UNIQUE(run_id,tool_call_id),
 FOREIGN KEY(run_id,conversation_id) REFERENCES runs(id,conversation_id)
);
CREATE INDEX run_permissions_conversation ON run_permissions(conversation_id,created_at DESC);
