CREATE TABLE user_guides (
    user_id uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    topic text NOT NULL CHECK (topic IN ('welcome','models','projects','files','agents','contexts','computer','skills','profile','publishing')),
    step smallint NOT NULL DEFAULT 0 CHECK (step BETWEEN 0 AND 19),
    status text NOT NULL CHECK (status IN ('in_progress','skipped','completed')),
    updated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (user_id, topic)
);

-- Existing accounts can opt in without being interrupted after an upgrade.
INSERT INTO user_guides (user_id, topic, status)
SELECT id, 'welcome', 'skipped' FROM users;
