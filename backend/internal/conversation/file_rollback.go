package conversation

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"github.com/biubiuqiu/lester-agent/backend/internal/eventlog"
	"github.com/biubiuqiu/lester-agent/backend/internal/sandbox"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"time"
)

type FileRollback struct {
	ID           uuid.UUID `json:"id"`
	FileChangeID uuid.UUID `json:"file_change_id"`
	Status       string    `json:"status"`
}

func (s *Service) FileRollback(ctx context.Context, workspaceID, userID, conversationID, changeID, id uuid.UUID) (FileRollback, error) {
	receipt := FileRollback{ID: id}
	err := s.db.QueryRow(ctx, `SELECT file_change_id,status FROM file_rollbacks WHERE id=$1 AND workspace_id=$2 AND submitted_by=$3 AND conversation_id=$4`, id, workspaceID, userID, conversationID).Scan(&receipt.FileChangeID, &receipt.Status)
	if errors.Is(err, pgx.ErrNoRows) {
		return receipt, ErrFileChangeNotFound
	}
	if err == nil && receipt.FileChangeID != changeID {
		return receipt, ErrFileRollbackConflict
	}
	return receipt, err
}
func byteDigest(data []byte) string { sum := sha256.Sum256(data); return hex.EncodeToString(sum[:]) }
func (s *Service) RollbackFileChange(ctx context.Context, workspaceID, userID, conversationID, changeID, id uuid.UUID) (FileRollback, error) {
	if id == uuid.Nil {
		return FileRollback{}, ErrFileRollbackConflict
	}
	if receipt, err := s.FileRollback(ctx, workspaceID, userID, conversationID, changeID, id); err == nil || !errors.Is(err, ErrFileChangeNotFound) {
		return receipt, err
	}
	guard, err := s.acquireRun(ctx, workspaceID, conversationID)
	if err != nil {
		return FileRollback{}, err
	}
	defer guard.Close()
	var allowedActor, running bool
	if err = s.db.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM workspace_members wm JOIN users u ON u.id=wm.user_id WHERE wm.workspace_id=$1 AND wm.user_id=$2 AND NOT u.disabled),EXISTS(SELECT 1 FROM runs WHERE conversation_id=$3 AND status IN ('running','cancelling'))`, workspaceID, userID, conversationID).Scan(&allowedActor, &running); err != nil {
		return FileRollback{}, err
	}
	if !allowedActor {
		return FileRollback{}, ErrFileChangeNotFound
	}
	if running {
		return FileRollback{}, ErrRunInProgress
	}
	if receipt, err := s.FileRollback(ctx, workspaceID, userID, conversationID, changeID, id); err == nil || !errors.Is(err, ErrFileChangeNotFound) {
		return receipt, err
	}
	change, err := s.FileChange(ctx, workspaceID, conversationID, changeID)
	if err != nil {
		return FileRollback{}, err
	}
	if change.Status != "confirmed" || !change.Retained || !change.BeforeExists || change.BeforeContent == nil || change.AfterContent == nil || change.RollbackStatus == "completed" || change.RollbackStatus == "pending" || change.RollbackStatus == "unknown" || byteDigest([]byte(*change.BeforeContent)) != change.BeforeSHA256 || byteDigest([]byte(*change.AfterContent)) != change.AfterSHA256 {
		return FileRollback{}, ErrFileRollbackConflict
	}
	writer := s.sandboxes
	if writer == nil {
		return FileRollback{}, ErrFileRollbackConflict
	}
	computer, err := s.ComputerForConversation(ctx, workspaceID, conversationID)
	if err != nil {
		return FileRollback{}, err
	}
	current, err := s.sandboxes.ReadFile(ctx, computer.SandboxID, computer.WorkDir, change.Path)
	if err != nil {
		return FileRollback{}, ErrFileRollbackConflict
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return FileRollback{}, err
	}
	defer tx.Rollback(ctx)
	var allowed bool
	if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM workspace_members wm JOIN users u ON u.id=wm.user_id WHERE wm.workspace_id=c.workspace_id AND wm.user_id=$3 AND NOT u.disabled) FROM conversations c WHERE c.workspace_id=$1 AND c.id=$2 FOR UPDATE`, workspaceID, conversationID, userID).Scan(&allowed); err != nil || !allowed {
		return FileRollback{}, ErrFileChangeNotFound
	}
	var active bool
	if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM runs WHERE conversation_id=$1 AND status IN ('running','cancelling'))`, conversationID).Scan(&active); err != nil {
		return FileRollback{}, err
	}
	if active {
		return FileRollback{}, ErrRunInProgress
	}
	receipt := FileRollback{ID: id, FileChangeID: changeID, Status: "pending"}
	if byteDigest(current) != change.AfterSHA256 {
		receipt.Status = "conflict"
	}
	if _, err = tx.Exec(ctx, `INSERT INTO file_rollbacks(id,file_change_id,workspace_id,conversation_id,submitted_by,status) VALUES($1,$2,$3,$4,$5,$6)`, id, changeID, workspaceID, conversationID, userID, receipt.Status); err != nil {
		return FileRollback{}, err
	}
	if _, err = tx.Exec(ctx, `UPDATE file_changes SET rollback_status=$2,rollback_id=$3,rollback_actor=$4,updated_at=now() WHERE id=$1`, changeID, receipt.Status, id, userID); err != nil {
		return FileRollback{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return FileRollback{}, err
	}
	if receipt.Status == "conflict" {
		return receipt, nil
	}
	result, writeErr := writer.WriteFileVersioned(ctx, computer.SandboxID, computer.WorkDir, change.Path, sandbox.FileWriteRequest{Content: []byte(*change.BeforeContent), ExpectedSHA256: change.AfterSHA256})
	receipt.Status = "unknown"
	if writeErr == nil && result != nil && result.OK && result.SHA256 == change.BeforeSHA256 {
		receipt.Status = "completed"
	}
	// A cancelled HTTP request cannot erase a real mutation receipt. A lost native
	// receipt stays unknown and never permits blind replay or inferred attribution.
	saveCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	save, err := s.db.Begin(saveCtx)
	if err != nil {
		return receipt, err
	}
	defer save.Rollback(saveCtx)
	if _, err = save.Exec(saveCtx, `SELECT id FROM conversations WHERE id=$1 FOR UPDATE`, conversationID); err != nil {
		return receipt, err
	}
	if _, err = save.Exec(saveCtx, `UPDATE file_rollbacks SET status=$2,updated_at=now() WHERE id=$1 AND status='pending'`, id, receipt.Status); err != nil {
		return receipt, err
	}
	if _, err = save.Exec(saveCtx, `UPDATE file_changes SET rollback_status=$2,updated_at=now() WHERE id=$1 AND rollback_id=$3`, changeID, receipt.Status, id); err != nil {
		return receipt, err
	}
	if receipt.Status == "completed" {
		err = eventlog.Append(saveCtx, save, change.RunID, conversationID, "FILE_UPDATED", map[string]any{"path": change.Path, "operation": "restore", "sha256": change.BeforeSHA256, "rollback_id": id})
	}
	if err == nil {
		err = eventlog.Append(saveCtx, save, change.RunID, conversationID, "FILE_ROLLBACK_UPDATED", map[string]any{"file_change_id": changeID, "rollback_id": id, "status": receipt.Status})
	}
	if err != nil {
		return receipt, err
	}
	return receipt, save.Commit(saveCtx)
}
