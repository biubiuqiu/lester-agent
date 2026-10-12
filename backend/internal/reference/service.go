// Package reference owns explicit, bounded references selected by a user.
// Conversation references grant read access through the active run's input
// message, never through model-supplied workspace IDs or arbitrary titles.
package reference

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const MaxReferences = 8

type Input struct {
	Type string    `json:"type"`
	ID   uuid.UUID `json:"id"`
}

type Item struct {
	Type        string    `json:"type"`
	ID          uuid.UUID `json:"id"`
	Title       string    `json:"title"`
	Description string    `json:"description"`
	Slug        string    `json:"slug,omitempty"`
	Version     string    `json:"version,omitempty"`
	Project     string    `json:"project,omitempty"`
	Source      string    `json:"source,omitempty"`
}

type Conversation struct {
	ID        uuid.UUID `json:"id"`
	Title     string    `json:"title"`
	Project   string    `json:"project"`
	MaxSeq    int64     `json:"max_seq"`
	UpdatedAt time.Time `json:"updated_at"`
}

type Skill struct {
	ID          uuid.UUID `json:"id"`
	Slug        string    `json:"slug"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	Version     string    `json:"version"`
}

// Resolve runs inside the message transaction. Labels and sequence cutoffs
// come from the database; the caller cannot forge metadata or expand authority.
func Resolve(ctx context.Context, tx pgx.Tx, workspace, current uuid.UUID, inputs []Input) ([]Conversation, []Skill, error) {
	conversations, skills := []Conversation{}, []Skill{}
	if len(inputs) > MaxReferences {
		return nil, nil, errors.New("每条消息最多引用 8 个对象")
	}
	seen := map[Input]bool{}
	for _, input := range inputs {
		if input.ID == uuid.Nil {
			return nil, nil, errors.New("无效的引用 ID")
		}
		if seen[input] {
			continue
		}
		seen[input] = true
		switch input.Type {
		case "conversation":
			if input.ID == current {
				return nil, nil, errors.New("不能引用当前会话")
			}
			var item Conversation
			err := tx.QueryRow(ctx, `SELECT c.id,left(c.title,160),left(COALESCE(p.name,''),80),c.last_message_seq,c.updated_at FROM conversations c LEFT JOIN projects p ON p.id=c.project_id AND p.workspace_id=c.workspace_id WHERE c.workspace_id=$1 AND c.id=$2`, workspace, input.ID).Scan(&item.ID, &item.Title, &item.Project, &item.MaxSeq, &item.UpdatedAt)
			if err != nil {
				return nil, nil, errors.New("引用的会话已删除或不可访问，请移除后重新选择")
			}
			conversations = append(conversations, item)
		case "skill":
			var item Skill
			if err := tx.QueryRow(ctx, `SELECT id,slug,left(name,120),left(description,240),left(version,80) FROM skills WHERE id=$1`, input.ID).Scan(&item.ID, &item.Slug, &item.Name, &item.Description, &item.Version); err != nil {
				return nil, nil, errors.New("引用的 Skill 已不可用，请移除后重新选择")
			}
			skills = append(skills, item)
		default:
			return nil, nil, errors.New("不支持的引用类型")
		}
	}
	return conversations, skills, nil
}

type Service struct{ DB *pgxpool.Pool }

func (s Service) Search(ctx context.Context, workspace, exclude uuid.UUID, kind, query string) ([]Item, error) {
	if kind != "all" && kind != "context" && kind != "conversation" && kind != "skill" {
		return nil, errors.New("不支持的引用类型")
	}
	if len([]rune(query)) > 80 {
		return nil, errors.New("引用搜索不能超过 80 个字符")
	}
	// Literal matching: % and _ in names do not become SQL wildcards.
	query = "%" + strings.NewReplacer(`\`, `\\`, "%", `\%`, "_", `\_`).Replace(query) + "%"
	rows, err := s.DB.Query(ctx, `WITH items AS (
		SELECT 'context'::text AS type,id,title,description,''::text AS slug,version::text AS version,''::text AS project,title||' '||description AS search,''::text AS source FROM context_entries WHERE workspace_id=$1
		UNION ALL
		SELECT 'conversation',c.id,c.title,COALESCE(p.name,''),'','',COALESCE(p.name,''),c.title||' '||COALESCE(p.name,''),'' FROM conversations c LEFT JOIN projects p ON p.id=c.project_id AND p.workspace_id=c.workspace_id WHERE c.workspace_id=$1 AND c.id<>$2
		UNION ALL
		SELECT 'skill',id,name,description,slug,version,'',name||' '||slug||' '||description,source FROM skills
	), matched AS (SELECT *,row_number() OVER (PARTITION BY type ORDER BY title,id) AS rank FROM items WHERE ($3='all' OR type=$3) AND search ILIKE $4) SELECT type,id,left(title,160),left(description,240),slug,left(version,80),left(project,80),source FROM matched WHERE ($3<>'all' OR rank<=8) ORDER BY CASE type WHEN 'context' THEN 0 WHEN 'conversation' THEN 1 ELSE 2 END,title,id LIMIT 24`, workspace, exclude, kind, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []Item{}
	for rows.Next() {
		var item Item
		if err = rows.Scan(&item.Type, &item.ID, &item.Title, &item.Description, &item.Slug, &item.Version, &item.Project, &item.Source); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s Service) Skill(ctx context.Context, id uuid.UUID) (Item, error) {
	item := Item{Type: "skill"}
	err := s.DB.QueryRow(ctx, `SELECT id,left(name,120),left(description,240),slug,left(version,80),source FROM skills WHERE id=$1`, id).Scan(&item.ID, &item.Title, &item.Description, &item.Slug, &item.Version, &item.Source)
	return item, err
}

func DecodeConversations(value any) ([]Conversation, error) {
	if value == nil {
		return []Conversation{}, nil
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	var result []Conversation
	if err = json.Unmarshal(raw, &result); err != nil || len(result) > MaxReferences {
		return nil, errors.New("invalid stored conversation references")
	}
	return result, nil
}

func DecodeSkills(value any) ([]Skill, error) {
	if value == nil {
		return []Skill{}, nil
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	var result []Skill
	if err = json.Unmarshal(raw, &result); err != nil || len(result) > MaxReferences {
		return nil, errors.New("invalid stored Skill references")
	}
	return result, nil
}
