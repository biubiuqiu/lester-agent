package personal

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"time"

	"github.com/biubiuqiu/lester-agent/backend/internal/auth"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// Inbox is a bounded page of conversations, not a count of individual messages.
type Inbox struct {
	Tasks      []Task `json:"tasks"`
	Total      int64  `json:"total"`
	NextCursor string `json:"next_cursor"`
}

type inboxCursor struct {
	At time.Time `json:"at"`
	ID uuid.UUID `json:"id"`
}

const publicReplyPredicate = `role='assistant' AND content ~ '[^[:space:]]' AND (tool_calls IS NULL OR tool_calls='[]'::jsonb) AND COALESCE(metadata->>'incomplete','false')!='true'`

const taskColumns = `c.id,c.project_id,p.name,c.title,
 CASE WHEN r.status='running' AND (
   EXISTS(SELECT 1 FROM run_permissions approval WHERE approval.workspace_id=c.workspace_id AND approval.conversation_id=c.id AND approval.run_id=r.id AND approval.status='pending')
   OR EXISTS(SELECT 1 FROM run_interactions interaction WHERE interaction.workspace_id=c.workspace_id AND interaction.conversation_id=c.id AND interaction.run_id=r.id AND interaction.status='pending')
 ) THEN 'awaiting_input' ELSE COALESCE(r.status,'idle') END,
 r.id,COALESCE(left(m.content,1800),''),c.updated_at,m.id,COALESCE(m.seq,0),m.created_at,COALESCE(m.seq>COALESCE(reads.last_read_seq,0),false)`

const taskJoins = `FROM conversations c JOIN projects p ON p.id=c.project_id AND p.workspace_id=c.workspace_id
 LEFT JOIN LATERAL(SELECT id,status FROM runs WHERE conversation_id=c.id ORDER BY created_at DESC,id DESC LIMIT 1)r ON true
 LEFT JOIN LATERAL(SELECT id,seq,content,created_at FROM messages WHERE conversation_id=c.id AND ` + publicReplyPredicate + ` ORDER BY seq DESC LIMIT 1)m ON true
 LEFT JOIN conversation_read_states reads ON reads.workspace_id=c.workspace_id AND reads.user_id=$2 AND reads.conversation_id=c.id`

func scanTask(row pgx.Row) (v Task, err error) {
	err = row.Scan(&v.ID, &v.ProjectID, &v.ProjectName, &v.Title, &v.Status, &v.RunID, &v.Reply, &v.UpdatedAt, &v.LatestReplyID, &v.LatestReplySeq, &v.ReplyAt, &v.Unread)
	return
}

// UnreadTasks reads original reply/read boundaries only. Looking at this list,
// or giving the model its metadata, never acknowledges a human having read it.
func (s *Service) UnreadTasks(ctx context.Context, p auth.Principal, project *uuid.UUID, cursor string) (Inbox, error) {
	result := Inbox{Tasks: []Task{}}
	var after inboxCursor
	if cursor != "" {
		if len(cursor) > 256 {
			return result, ErrInvalid
		}
		raw, err := base64.RawURLEncoding.DecodeString(cursor)
		if err != nil || json.Unmarshal(raw, &after) != nil || after.ID == uuid.Nil || after.At.IsZero() {
			return result, ErrInvalid
		}
	}
	tx, err := s.DB.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead})
	if err != nil {
		return result, err
	}
	defer tx.Rollback(ctx)
	if err = authorize(ctx, tx, p); err != nil {
		return result, err
	}
	// The total is independent of the page cursor and of the thirty recent
	// tasks. Older conversations with fresh replies remain discoverable.
	where := ` WHERE c.workspace_id=$1 AND c.created_by=$2 AND ($3::uuid IS NULL OR c.project_id=$3) AND m.seq>COALESCE(reads.last_read_seq,0)`
	if err = tx.QueryRow(ctx, `SELECT count(*) `+taskJoins+where, p.WorkspaceID, p.UserID, project).Scan(&result.Total); err != nil {
		return result, err
	}
	rows, err := tx.Query(ctx, `SELECT `+taskColumns+` `+taskJoins+where+` AND ($4::timestamptz IS NULL OR (m.created_at,c.id)<($4,$5::uuid)) ORDER BY m.created_at DESC,c.id DESC LIMIT 31`, p.WorkspaceID, p.UserID, project, nullableCursorTime(after.At), after.ID)
	if err != nil {
		return result, err
	}
	for rows.Next() {
		item, e := scanTask(rows)
		if e != nil {
			rows.Close()
			return result, e
		}
		result.Tasks = append(result.Tasks, item)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return result, err
	}
	if len(result.Tasks) > 30 {
		result.Tasks = result.Tasks[:30]
		last := result.Tasks[len(result.Tasks)-1]
		raw, _ := json.Marshal(inboxCursor{At: *last.ReplyAt, ID: last.ID})
		result.NextCursor = base64.RawURLEncoding.EncodeToString(raw)
	}
	if err = tx.Commit(ctx); err != nil {
		return Inbox{}, err
	}
	return result, nil
}

func nullableCursorTime(at time.Time) any {
	if at.IsZero() {
		return nil
	}
	return at
}

// MarkRead advances to one actually rendered, durable public reply. It cannot
// acknowledge a future sequence, a tool result, or a newer racing reply.
func (s *Service) MarkRead(ctx context.Context, p auth.Principal, conversationID, messageID uuid.UUID) (int64, error) {
	if conversationID == uuid.Nil || messageID == uuid.Nil {
		return 0, ErrInvalid
	}
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx)
	if err = authorize(ctx, tx, p); err != nil {
		return 0, err
	}
	var seq int64
	err = tx.QueryRow(ctx, `SELECT m.seq FROM conversations c JOIN messages m ON m.conversation_id=c.id WHERE c.id=$1 AND c.workspace_id=$2 AND c.created_by=$3 AND m.id=$4 AND `+publicReplyPredicate, conversationID, p.WorkspaceID, p.UserID, messageID).Scan(&seq)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, ErrNotFound
	}
	if err != nil {
		return 0, err
	}
	if err = tx.QueryRow(ctx, `INSERT INTO conversation_read_states(workspace_id,user_id,conversation_id,last_read_seq) VALUES($1,$2,$3,$4)
 ON CONFLICT(workspace_id,user_id,conversation_id) DO UPDATE SET last_read_seq=GREATEST(conversation_read_states.last_read_seq,EXCLUDED.last_read_seq),updated_at=now()
 RETURNING last_read_seq`, p.WorkspaceID, p.UserID, conversationID, seq).Scan(&seq); err != nil {
		return 0, err
	}
	if err = tx.Commit(ctx); err != nil {
		return 0, err
	}
	return seq, nil
}
