-- One enduring personal dialogue per workspace member. No Computer or task run.
CREATE TABLE personal_threads (
    workspace_id uuid NOT NULL,
    user_id uuid NOT NULL,
    revision bigint NOT NULL DEFAULT 1,
    context_after bigint NOT NULL DEFAULT 0,
    auto_memory boolean NOT NULL DEFAULT true,
    PRIMARY KEY(workspace_id,user_id),
    FOREIGN KEY(workspace_id,user_id) REFERENCES workspace_members(workspace_id,user_id) ON DELETE CASCADE
);
CREATE TABLE personal_turns (
    id uuid PRIMARY KEY,
    workspace_id uuid NOT NULL,
    user_id uuid NOT NULL,
    seq bigint GENERATED ALWAYS AS IDENTITY,
    request_digest text NOT NULL,
    content text NOT NULL CHECK(length(content) BETWEEN 1 AND 12000),
    reply text NOT NULL DEFAULT '',
    memory_notice text NOT NULL DEFAULT '',
    status text NOT NULL CHECK(status IN ('pending','completed','failed','interrupted')),
    model_id uuid NOT NULL,
    project_id uuid,
    usage jsonb NOT NULL DEFAULT '[]',
    created_at timestamptz NOT NULL DEFAULT now(),
    FOREIGN KEY(workspace_id,user_id) REFERENCES personal_threads(workspace_id,user_id) ON DELETE CASCADE,
    FOREIGN KEY(workspace_id,project_id) REFERENCES projects(workspace_id,id),
    UNIQUE(workspace_id,user_id,id)
);
CREATE INDEX personal_turns_history ON personal_turns(workspace_id,user_id,seq DESC);
CREATE TABLE personal_memories (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id uuid NOT NULL,
    user_id uuid NOT NULL,
    project_id uuid,
    kind text NOT NULL CHECK(kind IN ('preference','project','work')),
    topic text NOT NULL CHECK(length(topic) BETWEEN 1 AND 80),
    content text NOT NULL CHECK(length(content)<=600),
    evidence text NOT NULL DEFAULT '',
    source_turn_id uuid,
    automatic boolean NOT NULL DEFAULT false,
    deleted boolean NOT NULL DEFAULT false,
    revision bigint NOT NULL DEFAULT 1,
    updated_at timestamptz NOT NULL DEFAULT now(),
    FOREIGN KEY(workspace_id,user_id) REFERENCES personal_threads(workspace_id,user_id) ON DELETE CASCADE,
    FOREIGN KEY(workspace_id,project_id) REFERENCES projects(workspace_id,id),
    FOREIGN KEY(workspace_id,user_id,source_turn_id) REFERENCES personal_turns(workspace_id,user_id,id),
    UNIQUE(workspace_id,user_id,id)
);
CREATE UNIQUE INDEX personal_memory_topic ON personal_memories(workspace_id,user_id,COALESCE(project_id,'00000000-0000-0000-0000-000000000000'::uuid),lower(topic));
CREATE INDEX personal_memory_lookup ON personal_memories(workspace_id,user_id,project_id,updated_at DESC) WHERE NOT deleted;
CREATE TABLE personal_memory_changes (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id uuid NOT NULL,
    user_id uuid NOT NULL,
    memory_id uuid NOT NULL,
    turn_id uuid,
    revision bigint NOT NULL,
    before_value jsonb,
    after_value jsonb NOT NULL,
    undone boolean NOT NULL DEFAULT false,
    created_at timestamptz NOT NULL DEFAULT now(),
    FOREIGN KEY(workspace_id,user_id,memory_id) REFERENCES personal_memories(workspace_id,user_id,id) ON DELETE CASCADE,
    FOREIGN KEY(workspace_id,user_id,turn_id) REFERENCES personal_turns(workspace_id,user_id,id) ON DELETE CASCADE
);
CREATE INDEX personal_memory_changes_turn ON personal_memory_changes(workspace_id,user_id,turn_id);
CREATE TABLE personal_delegations (
    turn_id uuid PRIMARY KEY REFERENCES personal_turns(id) ON DELETE CASCADE,
    workspace_id uuid NOT NULL,
    user_id uuid NOT NULL,
    conversation_id uuid NOT NULL,
    submission_id uuid NOT NULL UNIQUE,
    project_id uuid NOT NULL,
    action text NOT NULL CHECK(action IN ('create','continue')),
    title text NOT NULL,
    prompt text NOT NULL,
    status text NOT NULL CHECK(status IN ('prepared','dispatching','started','queued','failed','unknown')),
    run_id uuid,
    input_id uuid,
    created_at timestamptz NOT NULL DEFAULT now(),
    FOREIGN KEY(workspace_id,user_id,turn_id) REFERENCES personal_turns(workspace_id,user_id,id),
    FOREIGN KEY(workspace_id,project_id) REFERENCES projects(workspace_id,id)
);
CREATE INDEX personal_delegations_owner ON personal_delegations(workspace_id,user_id,created_at DESC);
CREATE INDEX conversations_personal_recent ON conversations(workspace_id,created_by,updated_at DESC,id DESC);
