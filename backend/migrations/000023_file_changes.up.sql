CREATE TABLE file_changes (
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
 workspace_id uuid NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
 conversation_id uuid NOT NULL REFERENCES conversations(id) ON DELETE CASCADE,
 run_id uuid NOT NULL,
 tool_call_id text NOT NULL,
 path text NOT NULL,
 operation text NOT NULL CHECK(operation IN ('write','edit')),
 status text NOT NULL CHECK(status IN ('pending','confirmed','unknown')),
 before_exists boolean NOT NULL,
 before_sha256 text NOT NULL DEFAULT '',
 after_sha256 text NOT NULL,
 before_content bytea,
 after_content bytea,
 retained_bytes bigint NOT NULL DEFAULT 0 CHECK(retained_bytes>=0),
 unavailable_reason text NOT NULL DEFAULT '',
 rollback_status text NOT NULL DEFAULT 'none' CHECK(rollback_status IN ('none','pending','completed','conflict','unknown')),
 rollback_id uuid,
 rollback_actor uuid REFERENCES users(id) ON DELETE SET NULL,
 created_at timestamptz NOT NULL DEFAULT now(),
 updated_at timestamptz NOT NULL DEFAULT now(),
 FOREIGN KEY(run_id,conversation_id) REFERENCES runs(id,conversation_id),
 UNIQUE(run_id,tool_call_id)
);
CREATE INDEX file_changes_conversation ON file_changes(conversation_id,created_at,id);
CREATE TABLE file_rollbacks (
 id uuid PRIMARY KEY,
 file_change_id uuid NOT NULL REFERENCES file_changes(id) ON DELETE CASCADE,
 workspace_id uuid NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
 conversation_id uuid NOT NULL REFERENCES conversations(id) ON DELETE CASCADE,
 submitted_by uuid REFERENCES users(id) ON DELETE SET NULL,
 status text NOT NULL CHECK(status IN ('pending','completed','conflict','unknown')),
 created_at timestamptz NOT NULL DEFAULT now(),
 updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX file_rollbacks_one_restore ON file_rollbacks(file_change_id) WHERE status IN ('pending','completed','unknown');
