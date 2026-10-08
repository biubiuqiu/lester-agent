CREATE TABLE deliverables (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id uuid NOT NULL,
    conversation_id uuid NOT NULL,
    run_id uuid NOT NULL,
    title text NOT NULL CHECK (length(btrim(title)) BETWEEN 1 AND 120),
    summary text NOT NULL DEFAULT '' CHECK (length(summary) <= 2000),
    entry_path text NOT NULL CHECK (length(entry_path) BETWEEN 1 AND 1000),
    kind text NOT NULL CHECK (kind IN ('html', 'markdown')),
    content_sha256 text NOT NULL CHECK (content_sha256 ~ '^[a-f0-9]{64}$'),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    FOREIGN KEY (workspace_id, conversation_id) REFERENCES conversations(workspace_id, id) ON DELETE CASCADE,
    FOREIGN KEY (run_id, conversation_id) REFERENCES runs(id, conversation_id) ON DELETE CASCADE,
    UNIQUE (conversation_id, entry_path)
);
CREATE INDEX deliverables_workspace_conversation_idx ON deliverables(workspace_id, conversation_id, updated_at DESC, id);
CREATE INDEX deliverables_run_idx ON deliverables(run_id, conversation_id);

CREATE TABLE run_event_outbox (
    event_id bigint PRIMARY KEY REFERENCES run_events(id) ON DELETE CASCADE,
    created_at timestamptz NOT NULL DEFAULT now()
);
