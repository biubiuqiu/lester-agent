-- Preserve authored facts, their changes, and accepted task snapshots.
-- Availability cannot be preserved by the older schema: require the operator
-- to resume each paused memory explicitly before removing its pause setting.
DO $$
BEGIN
    IF EXISTS(SELECT 1 FROM personal_memories WHERE paused AND NOT deleted) THEN
        RAISE EXCEPTION 'Resume paused memories before rolling back memory management';
    END IF;
END $$;
DROP INDEX personal_memory_changes_history;
ALTER TABLE personal_memory_changes
    DROP CONSTRAINT personal_memory_changes_undo_owner,
    DROP CONSTRAINT personal_memory_changes_owner_id,
    DROP COLUMN undo_of;
ALTER TABLE personal_memories DROP COLUMN paused;
