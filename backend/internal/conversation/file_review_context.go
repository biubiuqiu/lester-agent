package conversation

import (
	"context"
	"encoding/json"
	"github.com/google/uuid"
)

// Restore bounded platform facts after compaction without granting file access
// or claiming that a historic digest still describes the current live file.
func (s *Service) fileReviewContext(ctx context.Context, conversationID, runID uuid.UUID) (string, error) {
	rows, err := s.db.Query(ctx, `SELECT fc.path,fc.status,fc.rollback_status,CASE WHEN fc.rollback_status='completed' THEN fc.before_sha256 ELSE '' END FROM file_changes fc JOIN runs active ON active.conversation_id=fc.conversation_id AND active.id=$2 AND active.status='running' WHERE fc.conversation_id=$1 AND (fc.status IN ('pending','unknown') OR fc.rollback_status<>'none') ORDER BY fc.updated_at DESC,fc.id DESC LIMIT 20`, conversationID, runID)
	if err != nil {
		return "", err
	}
	defer rows.Close()
	type fact struct {
		Path                string `json:"path"`
		WriteStatus         string `json:"write_status"`
		RestoreStatus       string `json:"restore_status"`
		ConfirmedRestoreSHA string `json:"confirmed_restore_sha256,omitempty"`
	}
	facts := []fact{}
	for rows.Next() {
		var item fact
		if err = rows.Scan(&item.Path, &item.WriteStatus, &item.RestoreStatus, &item.ConfirmedRestoreSHA); err != nil {
			return "", err
		}
		facts = append(facts, item)
	}
	if err = rows.Err(); err != nil {
		return "", err
	}
	if len(facts) == 0 {
		return "", nil
	}
	raw, _ := json.Marshal(facts)
	return "\n\nRecent platform file review/restore receipts (up to 20, newest first):\n" + string(raw) + "\nPending/unknown writes or restores are unconfirmed; do not replay them or infer success from matching bytes. A completed restore confirms that operation's returned digest, not the file's current contents. Read files again before modifying; these records grant no FileState or execution permission.\n", nil
}
