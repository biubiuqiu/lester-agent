package agent

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"
	"unicode"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

const MaxFileBytes = 10 << 20
const MaxAgentFiles = 20
const MaxAgentFileTotal = 50 << 20

var ErrInvalidFile = errors.New("文件名无效，或文件大小不在 1 字节至 10 MiB 之间")
var ErrFileLimit = errors.New("每个 Agent 最多 20 个文件，总大小最多 50 MiB")

type File struct {
	ID          uuid.UUID `json:"id"`
	Name        string    `json:"name"`
	ContentType string    `json:"content_type"`
	SizeBytes   int64     `json:"size_bytes"`
	CreatedAt   time.Time `json:"created_at"`
	ObjectKey   string    `json:"-"`
}

func validFileName(name string) bool {
	if name == "" || name == "." || name == ".." || len([]rune(name)) > 180 || strings.ContainsAny(name, "/\\") {
		return false
	}
	for _, r := range name {
		if unicode.IsControl(r) {
			return false
		}
	}
	return true
}
func ValidFileName(name string) bool { return validFileName(name) }

func (s *Service) ListFiles(ctx context.Context, workspace, agentID uuid.UUID) ([]File, error) {
	var exists bool
	if err := s.DB.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agents WHERE workspace_id=$1 AND id=$2)`, workspace, agentID).Scan(&exists); err != nil {
		return nil, err
	}
	if !exists {
		return nil, pgx.ErrNoRows
	}
	rows, err := s.DB.Query(ctx, `SELECT id,name,content_type,size_bytes,created_at FROM agent_files WHERE workspace_id=$1 AND agent_id=$2 ORDER BY name,id`, workspace, agentID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []File{}
	for rows.Next() {
		var item File
		if err = rows.Scan(&item.ID, &item.Name, &item.ContentType, &item.SizeBytes, &item.CreatedAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Service) UploadFile(ctx context.Context, workspace, agentID uuid.UUID, name, contentType string, data []byte) (File, error) {
	if !validFileName(name) || len(data) == 0 || len(data) > MaxFileBytes {
		return File{}, ErrInvalidFile
	}
	if s.Objects == nil {
		return File{}, errors.New("对象存储不可用")
	}
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return File{}, err
	}
	defer tx.Rollback(ctx)
	var locked uuid.UUID
	if err = tx.QueryRow(ctx, `SELECT id FROM agents WHERE workspace_id=$1 AND id=$2 FOR UPDATE`, workspace, agentID).Scan(&locked); err != nil {
		return File{}, err
	}
	var count, total int64
	if err = tx.QueryRow(ctx, `SELECT count(*),COALESCE(sum(size_bytes),0) FROM agent_files WHERE agent_id=$1`, agentID).Scan(&count, &total); err != nil {
		return File{}, err
	}
	if count >= MaxAgentFiles || total+int64(len(data)) > MaxAgentFileTotal {
		return File{}, ErrFileLimit
	}
	file := File{ID: uuid.New(), Name: name, ContentType: contentType, SizeBytes: int64(len(data))}
	file.ObjectKey = fmt.Sprintf("agents/%s/%s/%s", workspace, agentID, file.ID)
	if err = s.Objects.Put(ctx, file.ObjectKey, bytes.NewReader(data), int64(len(data)), contentType); err != nil {
		return File{}, err
	}
	err = tx.QueryRow(ctx, `INSERT INTO agent_files(id,agent_id,workspace_id,name,content_type,size_bytes,object_key) VALUES($1,$2,$3,$4,$5,$6,$7) RETURNING created_at`, file.ID, agentID, workspace, name, contentType, len(data), file.ObjectKey).Scan(&file.CreatedAt)
	if err != nil {
		_ = s.Objects.Delete(ctx, file.ObjectKey)
		return File{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		_ = s.Objects.Delete(ctx, file.ObjectKey)
		return File{}, err
	}
	return file, nil
}

func (s *Service) GetFile(ctx context.Context, workspace, agentID, fileID uuid.UUID) (File, error) {
	var f File
	err := s.DB.QueryRow(ctx, `SELECT id,name,content_type,size_bytes,created_at,object_key FROM agent_files WHERE workspace_id=$1 AND agent_id=$2 AND id=$3`, workspace, agentID, fileID).Scan(&f.ID, &f.Name, &f.ContentType, &f.SizeBytes, &f.CreatedAt, &f.ObjectKey)
	return f, err
}

func (s *Service) OpenFile(ctx context.Context, workspace, agentID, fileID uuid.UUID) (File, io.ReadCloser, error) {
	f, err := s.GetFile(ctx, workspace, agentID, fileID)
	if err != nil {
		return f, nil, err
	}
	reader, err := s.Objects.Get(ctx, f.ObjectKey)
	return f, reader, err
}

func (s *Service) DeleteFile(ctx context.Context, workspace, agentID, fileID uuid.UUID) error {
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var locked uuid.UUID
	if err = tx.QueryRow(ctx, `SELECT id FROM agents WHERE workspace_id=$1 AND id=$2 FOR UPDATE`, workspace, agentID).Scan(&locked); err != nil {
		return err
	}
	var key string
	if err = tx.QueryRow(ctx, `DELETE FROM agent_files WHERE workspace_id=$1 AND agent_id=$2 AND id=$3 RETURNING object_key`, workspace, agentID, fileID).Scan(&key); err != nil {
		return err
	}
	var used bool
	if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM conversation_agent_files WHERE object_key=$1)`, key).Scan(&used); err != nil {
		return err
	}
	if err = tx.Commit(ctx); err != nil {
		return err
	}
	if !used {
		return s.Objects.Delete(ctx, key)
	}
	return nil
}
