CREATE TABLE skills (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    slug text NOT NULL UNIQUE CHECK (slug ~ '^[a-z0-9]+(?:-[a-z0-9]+)*$'),
    name text NOT NULL,
    description text NOT NULL,
    version text NOT NULL,
    object_key text NOT NULL UNIQUE,
    source text NOT NULL DEFAULT 'builtin' CHECK (source IN ('builtin', 'workspace')),
    size_bytes bigint NOT NULL DEFAULT 0 CHECK (size_bytes >= 0),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE conversation_skills (
    conversation_id uuid NOT NULL REFERENCES conversations(id) ON DELETE CASCADE,
    skill_id uuid NOT NULL REFERENCES skills(id) ON DELETE CASCADE,
    installed_by uuid NOT NULL REFERENCES users(id),
    installed_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (conversation_id, skill_id)
);

CREATE INDEX conversation_skills_skill_id_idx ON conversation_skills(skill_id);
CREATE INDEX conversation_skills_installed_by_idx ON conversation_skills(installed_by);

CREATE TABLE attachments (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    conversation_id uuid NOT NULL REFERENCES conversations(id) ON DELETE CASCADE,
    uploaded_by uuid NOT NULL REFERENCES users(id),
    original_name text NOT NULL,
    stored_path text NOT NULL,
    content_type text NOT NULL DEFAULT 'application/octet-stream',
    size_bytes bigint NOT NULL CHECK (size_bytes >= 0 AND size_bytes <= 26214400),
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (conversation_id, stored_path)
);

CREATE INDEX attachments_conversation_created_idx ON attachments(conversation_id, created_at);
CREATE INDEX attachments_uploaded_by_idx ON attachments(uploaded_by);
