-- Latest task per conversation; guarded recovery scans only live records.
CREATE INDEX runs_conversation_latest_idx ON runs(conversation_id,created_at DESC,id DESC);
CREATE INDEX runs_active_conversation_idx ON runs(conversation_id) WHERE status IN ('running','cancelling');
CREATE INDEX run_events_run_activity_idx ON run_events(run_id,created_at DESC);
CREATE INDEX run_interactions_pending_run_idx ON run_interactions(run_id,kind) WHERE status='pending';
CREATE INDEX command_tasks_workspace_active_idx ON command_tasks(workspace_id,conversation_id,status) WHERE status IN ('starting','running','stopping','unknown');
