package conversation

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"

	"github.com/biubiuqiu/lester-agent/backend/internal/reference"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

var ErrSubmissionConflict = errors.New("submission ID already belongs to a different message; use a new submission ID")

type Submission struct {
	RunID     uuid.UUID `json:"run_id"`
	Status    string    `json:"status"`
	MessageID uuid.UUID `json:"message_id"`
	digest    string
}

// Submission looks up an accepted input, never executes or replays it. The
// submitting user and workspace are both part of its authority.
func (s *Service) Submission(ctx context.Context, workspaceID, userID, conversationID, submissionID uuid.UUID) (*Submission, error) {
	var found Submission
	err := s.db.QueryRow(ctx, `SELECT r.id,r.status,r.input_message_id,r.submission_sha256
		FROM runs r JOIN conversations c ON c.id=r.conversation_id
		WHERE c.workspace_id=$1 AND c.id=$2 AND r.submitted_by=$3 AND r.submission_id=$4`,
		workspaceID, conversationID, userID, submissionID).Scan(&found.RunID, &found.Status, &found.MessageID, &found.digest)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &found, nil
}

func submissionDigest(content string, attachments, contexts []uuid.UUID, refs []reference.Input, modes ...string) string {
	// Normalize nil/empty and duplicate legacy IDs to the effective input. Keep
	// selected reference order: it is part of the user's selected context.
	attachments = append([]uuid.UUID{}, uniqueUUIDs(attachments)...)
	contexts = append([]uuid.UUID{}, uniqueUUIDs(contexts)...)
	refs = append([]reference.Input{}, refs...)
	raw, _ := json.Marshal(struct {
		Content               string
		Attachments, Contexts []uuid.UUID
		References            []reference.Input
	}{content, attachments, contexts, refs})
	digest := sha256.Sum256(raw)
	// Preserve existing execution receipt hashes across the mode upgrade.
	if len(modes) > 0 && modes[0] == "plan" {
		digest = sha256.Sum256(append(digest[:], []byte("\x00plan")...))
	}
	return hex.EncodeToString(digest[:])
}

func (s *Service) acceptedSubmission(ctx context.Context, workspaceID, userID, conversationID, submissionID uuid.UUID, digest string) (uuid.UUID, bool, error) {
	if submissionID == uuid.Nil {
		return uuid.Nil, false, nil
	}
	found, err := s.Submission(ctx, workspaceID, userID, conversationID, submissionID)
	if err != nil || found == nil {
		return uuid.Nil, false, err
	}
	if found.digest != digest {
		return uuid.Nil, false, ErrSubmissionConflict
	}
	return found.RunID, true, nil
}
