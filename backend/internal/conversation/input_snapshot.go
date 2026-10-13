package conversation

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"github.com/biubiuqiu/lester-agent/backend/internal/contextlibrary"
	"github.com/biubiuqiu/lester-agent/backend/internal/reference"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// Snapshot explicit inputs under the caller's conversation transaction. Both
// immediate sends and accepted queued tasks use these same authority checks.
func prepareRunInput(ctx context.Context, tx pgx.Tx, workspaceID, userID, id uuid.UUID, content string, attachmentIDs, contextIDs []uuid.UUID, selected []reference.Input) (string, []byte, error) {
	if len(contextIDs)+len(selected) > reference.MaxReferences {
		return "", nil, errors.New("每条消息最多引用 8 个对象")
	}
	attachmentIDs = uniqueUUIDs(attachmentIDs)
	if strings.TrimSpace(content) == "" && len(attachmentIDs) == 0 {
		return "", nil, errors.New("message is required")
	}
	extra := []reference.Input{}
	for _, input := range selected {
		if input.ID == uuid.Nil {
			return "", nil, errors.New("无效的引用 ID")
		}
		if input.Type == "context" {
			contextIDs = append(contextIDs, input.ID)
		} else {
			extra = append(extra, input)
		}
	}
	attachments := []Attachment{}
	if len(attachmentIDs) > 0 {
		rows, queryErr := tx.Query(ctx, `SELECT id,conversation_id,original_name,stored_path,content_type,size_bytes,created_at FROM attachments WHERE conversation_id=$1 AND uploaded_by=$2 AND id=ANY($3) ORDER BY created_at,id`, id, userID, attachmentIDs)
		if queryErr != nil {
			return "", nil, queryErr
		}
		for rows.Next() {
			var attachment Attachment
			if queryErr = rows.Scan(&attachment.ID, &attachment.ConversationID, &attachment.OriginalName, &attachment.StoredPath, &attachment.ContentType, &attachment.SizeBytes, &attachment.CreatedAt); queryErr != nil {
				rows.Close()
				return "", nil, queryErr
			}
			attachments = append(attachments, attachment)
		}
		queryErr = rows.Err()
		rows.Close()
		if queryErr != nil {
			return "", nil, queryErr
		}
		if len(attachments) != len(attachmentIDs) {
			return "", nil, errors.New("one or more attachments were not found")
		}
	}
	if strings.TrimSpace(content) == "" {
		content = "已上传附件：" + attachmentNames(attachments)
	}
	contexts, err := contextlibrary.Resolve(ctx, tx, workspaceID, contextIDs)
	if err != nil {
		return "", nil, err
	}
	conversations, selectedSkills, err := reference.Resolve(ctx, tx, workspaceID, id, extra)
	if err != nil {
		return "", nil, err
	}
	metadata, marshalErr := json.Marshal(map[string]any{"attachments": attachments, "contexts": contexts, "conversations": conversations, "skills": selectedSkills})
	if marshalErr != nil {
		return "", nil, marshalErr
	}
	return content, metadata, nil
}
