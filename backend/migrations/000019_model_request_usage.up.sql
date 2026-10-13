CREATE TABLE model_request_usage (
    run_id uuid NOT NULL REFERENCES runs(id) ON DELETE CASCADE,
    request_index integer NOT NULL CHECK(request_index > 0),
    purpose text NOT NULL DEFAULT 'execution' CHECK(purpose IN ('execution','compaction')),
    status text NOT NULL CHECK(status IN ('started','completed','failed','cancelled')),
    estimated_input_tokens integer NOT NULL CHECK(estimated_input_tokens > 0),
    budget jsonb NOT NULL,
    usage jsonb,
    error_kind text,
    created_at timestamptz NOT NULL DEFAULT now(),
    finished_at timestamptz,
    PRIMARY KEY(run_id,request_index)
);
