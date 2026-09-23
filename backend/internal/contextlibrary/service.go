package contextlibrary

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"strings"
	"time"
)

type Entry struct {
	ID          uuid.UUID `json:"id"`
	Title       string    `json:"title"`
	Description string    `json:"description"`
	Content     string    `json:"content"`
	Version     int       `json:"version"`
	UpdatedAt   time.Time `json:"updated_at"`
}
type Service struct{ DB *pgxpool.Pool }

func Validate(e Entry) error {
	if n := len([]rune(strings.TrimSpace(e.Title))); n < 1 || n > 80 {
		return errors.New("词条名称需为 1–80 个字符")
	}
	if len([]rune(e.Description)) > 240 {
		return errors.New("简介不能超过 240 个字符")
	}
	if n := len([]rune(strings.TrimSpace(e.Content))); n < 1 || n > 20000 {
		return errors.New("正文需为 1–20000 个字符")
	}
	return nil
}
func (s *Service) List(ctx context.Context, workspace uuid.UUID) ([]Entry, error) {
	rows, err := s.DB.Query(ctx, `SELECT id,title,description,version,updated_at FROM context_entries WHERE workspace_id=$1 ORDER BY title,id`, workspace)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []Entry{}
	for rows.Next() {
		var e Entry
		if err = rows.Scan(&e.ID, &e.Title, &e.Description, &e.Version, &e.UpdatedAt); err != nil {
			return nil, err
		}
		items = append(items, e)
	}
	return items, rows.Err()
}
func (s *Service) Get(ctx context.Context, workspace, id uuid.UUID) (Entry, error) {
	var e Entry
	err := s.DB.QueryRow(ctx, `SELECT id,title,description,content,version,updated_at FROM context_entries WHERE workspace_id=$1 AND id=$2`, workspace, id).Scan(&e.ID, &e.Title, &e.Description, &e.Content, &e.Version, &e.UpdatedAt)
	return e, err
}
func (s *Service) Save(ctx context.Context, workspace, id uuid.UUID, e Entry) (Entry, error) {
	if err := Validate(e); err != nil {
		return Entry{}, err
	}
	e.Title = strings.TrimSpace(e.Title)
	var err error
	if id == uuid.Nil {
		err = s.DB.QueryRow(ctx, `INSERT INTO context_entries(workspace_id,title,description,content) VALUES($1,$2,$3,$4) RETURNING id,version,updated_at`, workspace, e.Title, e.Description, e.Content).Scan(&e.ID, &e.Version, &e.UpdatedAt)
	} else {
		err = s.DB.QueryRow(ctx, `UPDATE context_entries SET title=$3,description=$4,content=$5,version=version+1,updated_at=now() WHERE workspace_id=$1 AND id=$2 AND version=$6 RETURNING id,version,updated_at`, workspace, id, e.Title, e.Description, e.Content, e.Version).Scan(&e.ID, &e.Version, &e.UpdatedAt)
	}
	return e, err
}
func (s *Service) Delete(ctx context.Context, workspace, id uuid.UUID, version int) error {
	tag, err := s.DB.Exec(ctx, `DELETE FROM context_entries WHERE workspace_id=$1 AND id=$2 AND version=$3`, workspace, id, version)
	if err == nil && tag.RowsAffected() != 1 {
		return pgx.ErrNoRows
	}
	return err
}

// Resolve takes one database snapshot, rejects inaccessible entries and preserves
// the requested order. The caller stores these values in the message transaction.
func Resolve(ctx context.Context, tx pgx.Tx, workspace uuid.UUID, ids []uuid.UUID) ([]Entry, error) {
	if len(ids) > 8 {
		return nil, errors.New("每条消息最多引用 8 个上下文词条")
	}
	result := []Entry{}
	if len(ids) == 0 {
		return result, nil
	}
	rows, err := tx.Query(ctx, `SELECT id,title,description,content,version,updated_at FROM context_entries WHERE workspace_id=$1 AND id=ANY($2)`, workspace, ids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	entries := map[uuid.UUID]Entry{}
	for rows.Next() {
		var e Entry
		if err = rows.Scan(&e.ID, &e.Title, &e.Description, &e.Content, &e.Version, &e.UpdatedAt); err != nil {
			return nil, err
		}
		entries[e.ID] = e
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	seen := map[uuid.UUID]bool{}
	total := 0
	for _, id := range ids {
		if seen[id] {
			continue
		}
		seen[id] = true
		e, ok := entries[id]
		if !ok {
			return nil, errors.New("引用的上下文已删除或不可访问，请移除后重新选择")
		}
		total += len([]rune(e.Content))
		if total > 40000 {
			return nil, errors.New("引用上下文的正文合计不能超过 40000 个字符")
		}
		result = append(result, e)
	}
	return result, nil
}
