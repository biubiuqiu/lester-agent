ALTER TABLE sandboxes RENAME TO conversation_sandboxes_legacy;

CREATE TABLE sandboxes (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id uuid NOT NULL,
    user_id uuid NOT NULL UNIQUE,
    provider text NOT NULL DEFAULT 'docker',
    provider_ref text NOT NULL UNIQUE,
    status text NOT NULL DEFAULT 'not_created'
        CHECK (status IN ('not_created','creating','running','suspended','stopped','unhealthy','missing','error')),
    last_error text,
    last_checked_at timestamptz,
    last_active_at timestamptz NOT NULL DEFAULT now(),
    created_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT sandboxes_workspace_member_fkey
        FOREIGN KEY (workspace_id, user_id)
        REFERENCES workspace_members(workspace_id, user_id)
        ON DELETE CASCADE
);

INSERT INTO sandboxes(workspace_id,user_id,provider_ref,status,last_active_at,created_at)
SELECT DISTINCT ON (c.created_by)
       c.workspace_id,
       c.created_by,
       c.created_by::text,
       'not_created',
       legacy.last_active_at,
       legacy.created_at
FROM conversation_sandboxes_legacy legacy
JOIN conversations c ON c.id=legacy.conversation_id
ORDER BY c.created_by,legacy.last_active_at DESC;

CREATE INDEX sandboxes_running_last_active_idx
    ON sandboxes(last_active_at)
    WHERE status='running';
