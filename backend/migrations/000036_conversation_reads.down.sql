-- Drop reading metadata only; task messages, personal dialogue and files stay.
DROP INDEX messages_public_replies_idx;
DROP TABLE conversation_read_states;
