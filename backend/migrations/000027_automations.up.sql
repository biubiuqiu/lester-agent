CREATE TABLE automations (
    id uuid PRIMARY KEY,
    workspace_id uuid NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    created_by uuid NOT NULL REFERENCES users(id),
    name text NOT NULL CHECK (length(name) BETWEEN 1 AND 120),
    kind text NOT NULL CHECK (kind IN ('schedule','webhook')),
    project_id uuid NOT NULL,
    agent_slug text NOT NULL,
    model_deployment_id uuid NOT NULL REFERENCES model_deployments(id),
    prompt text NOT NULL CHECK (octet_length(prompt) BETWEEN 1 AND 16000),
    permission_mode text NOT NULL DEFAULT 'standard' CHECK (permission_mode IN ('standard','auto','full')),
    enabled boolean NOT NULL DEFAULT false,
    cron text NOT NULL DEFAULT '',
    timezone text NOT NULL DEFAULT 'UTC',
    run_at timestamptz,
    next_run_at timestamptz,
    webhook_hash bytea,
    revision bigint NOT NULL DEFAULT 1,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    deleted_at timestamptz,
    UNIQUE (workspace_id,id),
    FOREIGN KEY (workspace_id,project_id) REFERENCES projects(workspace_id,id),
    CHECK ((kind='schedule' AND webhook_hash IS NULL AND ((cron<>'' AND run_at IS NULL) OR (cron='' AND run_at IS NOT NULL))) OR
           (kind='webhook' AND webhook_hash IS NOT NULL AND cron='' AND run_at IS NULL AND next_run_at IS NULL))
);
CREATE INDEX automations_workspace_idx ON automations(workspace_id,created_at DESC,id DESC) WHERE deleted_at IS NULL;
CREATE INDEX automations_due_idx ON automations(next_run_at,id) WHERE enabled AND deleted_at IS NULL AND kind='schedule';

CREATE TABLE automation_deliveries (
    id uuid PRIMARY KEY,
    accepted_order bigserial NOT NULL UNIQUE,
    workspace_id uuid NOT NULL,
    automation_id uuid NOT NULL,
    event_key text NOT NULL CHECK (length(event_key) BETWEEN 1 AND 160),
    request_digest text NOT NULL,
    source text NOT NULL CHECK (source IN ('schedule','webhook','manual')),
    snapshot jsonb NOT NULL,
    conversation_id uuid NOT NULL,
    run_id uuid REFERENCES runs(id) ON DELETE SET NULL,
    state text NOT NULL DEFAULT 'pending' CHECK (state IN ('pending','dispatching','accepted','failed','unknown','skipped','cancelled')),
    error_code text NOT NULL DEFAULT '',
    scheduled_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    FOREIGN KEY (workspace_id,automation_id) REFERENCES automations(workspace_id,id) ON DELETE CASCADE,
    UNIQUE (automation_id,event_key)
);
CREATE INDEX automation_deliveries_pending_idx ON automation_deliveries(accepted_order) WHERE state IN ('pending','dispatching','unknown');
CREATE INDEX automation_deliveries_queued_idx ON automation_deliveries(automation_id,accepted_order) WHERE state='pending';
CREATE INDEX automation_deliveries_history_idx ON automation_deliveries(workspace_id,automation_id,accepted_order DESC);
CREATE INDEX automation_deliveries_rate_idx ON automation_deliveries(workspace_id,automation_id,created_at DESC);
CREATE INDEX automation_deliveries_active_idx ON automation_deliveries(automation_id,state,run_id) WHERE state IN ('dispatching','accepted');
