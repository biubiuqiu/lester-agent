-- Explicit downgrade discards personal dialogue/memories, never task files/runs.
DROP INDEX conversations_personal_recent;
DROP TABLE personal_delegations;
DROP TABLE personal_memory_changes;
DROP TABLE personal_memories;
DROP TABLE personal_turns;
DROP TABLE personal_threads;
