-- A pause keeps the authored fact and provenance while excluding future recall.
-- The false default also matches snapshots written before this migration.
ALTER TABLE personal_memories ADD COLUMN paused boolean NOT NULL DEFAULT false;

-- New undo operations retain their resulting revision as an ordinary change.
-- Historical revision gaps are left intact; no past activity is fabricated.
ALTER TABLE personal_memory_changes
    ADD COLUMN undo_of uuid,
    ADD CONSTRAINT personal_memory_changes_owner_id UNIQUE(workspace_id,user_id,id),
    ADD CONSTRAINT personal_memory_changes_undo_owner
        FOREIGN KEY(workspace_id,user_id,undo_of)
        REFERENCES personal_memory_changes(workspace_id,user_id,id);
CREATE INDEX personal_memory_changes_history
    ON personal_memory_changes(workspace_id,user_id,memory_id,revision DESC);
