package agent

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/biubiuqiu/lester-agent/backend/internal/blob"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Agent struct {
	ID                    uuid.UUID  `json:"id"`
	Slug                  string     `json:"slug"`
	Name                  string     `json:"name"`
	Description           string     `json:"description"`
	Instructions          string     `json:"instructions"`
	SkillSlugs            []string   `json:"skill_slugs"`
	Version               int        `json:"version"`
	Builtin               bool       `json:"builtin"`
	UpdatedAt             time.Time  `json:"updated_at"`
	BuilderConversationID *uuid.UUID `json:"builder_conversation_id,omitempty"`
}

var Builtins = []Agent{
	{Slug: "lester", Name: "Lester", Description: "通用工作助手，冷静、务实地完成任务。", Builtin: true, SkillSlugs: []string{}},
	{Slug: "franklin", Name: "Franklin", Description: "直接、高效，专注推进。", Builtin: true, SkillSlugs: []string{}},
	{Slug: "michael", Name: "Michael", Description: "结构化、审慎，重视质量。", Builtin: true, SkillSlugs: []string{}},
	{Slug: "trevor", Name: "Trevor", Description: "大胆、主动，擅长探索。", Builtin: true, SkillSlugs: []string{}},
	{Slug: "agent-designer", Name: "智能体设计师", Description: "通过对话了解需求，创建并完善专属智能体。", Builtin: true, SkillSlugs: []string{}},
}

var ErrInvalid = errors.New("请填写名称和提示词，并检查长度及所选 Skill")

type Service struct {
	DB      *pgxpool.Pool
	Objects blob.Store
}

func (s *Service) validate(ctx context.Context, a Agent) error {
	if n := len([]rune(strings.TrimSpace(a.Name))); n == 0 || n > 80 {
		return ErrInvalid
	}
	if len([]rune(a.Description)) > 500 || len([]rune(strings.TrimSpace(a.Instructions))) == 0 || len([]rune(a.Instructions)) > 20000 || len(a.SkillSlugs) > 20 {
		return ErrInvalid
	}
	seen := map[string]bool{}
	for _, slug := range a.SkillSlugs {
		if slug == "" || seen[slug] {
			return ErrInvalid
		}
		seen[slug] = true
		var available bool
		if err := s.DB.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM skills WHERE slug=$1)`, slug).Scan(&available); err != nil {
			return err
		}
		if !available {
			return ErrInvalid
		}
	}
	return nil
}

func (s *Service) List(ctx context.Context, workspace uuid.UUID) ([]Agent, error) {
	items := append([]Agent{}, Builtins...)
	rows, err := s.DB.Query(ctx, `SELECT a.id,a.name,a.description,a.instructions,a.skill_slugs,a.version,a.updated_at,(SELECT c.id FROM conversations c WHERE c.created_agent_id=a.id AND c.workspace_id=$1 ORDER BY c.created_at LIMIT 1) FROM agents a WHERE a.workspace_id=$1 ORDER BY a.updated_at DESC,a.id`, workspace)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var a Agent
		if err = rows.Scan(&a.ID, &a.Name, &a.Description, &a.Instructions, &a.SkillSlugs, &a.Version, &a.UpdatedAt, &a.BuilderConversationID); err != nil {
			return nil, err
		}
		a.Slug = "custom-" + a.ID.String()
		items = append(items, a)
	}
	return items, rows.Err()
}

func (s *Service) Get(ctx context.Context, workspace, id uuid.UUID) (Agent, error) {
	var a Agent
	err := s.DB.QueryRow(ctx, `SELECT a.id,a.name,a.description,a.instructions,a.skill_slugs,a.version,a.updated_at,(SELECT c.id FROM conversations c WHERE c.created_agent_id=a.id AND c.workspace_id=$1 ORDER BY c.created_at LIMIT 1) FROM agents a WHERE a.workspace_id=$1 AND a.id=$2`, workspace, id).Scan(&a.ID, &a.Name, &a.Description, &a.Instructions, &a.SkillSlugs, &a.Version, &a.UpdatedAt, &a.BuilderConversationID)
	a.Slug = "custom-" + a.ID.String()
	return a, err
}

func (s *Service) Save(ctx context.Context, workspace, id uuid.UUID, a Agent) (Agent, error) {
	if err := s.validate(ctx, a); err != nil {
		return Agent{}, err
	}
	a.Name = strings.TrimSpace(a.Name)
	a.Instructions = strings.TrimSpace(a.Instructions)
	if a.SkillSlugs == nil {
		a.SkillSlugs = []string{}
	}
	var err error
	if id == uuid.Nil {
		err = s.DB.QueryRow(ctx, `INSERT INTO agents(workspace_id,name,description,instructions,skill_slugs) VALUES($1,$2,$3,$4,$5) RETURNING id,version,updated_at`, workspace, a.Name, a.Description, a.Instructions, a.SkillSlugs).Scan(&a.ID, &a.Version, &a.UpdatedAt)
	} else {
		err = s.DB.QueryRow(ctx, `UPDATE agents SET name=$3,description=$4,instructions=$5,skill_slugs=$6,version=version+1,updated_at=now() WHERE workspace_id=$1 AND id=$2 AND version=$7 RETURNING id,version,updated_at`, workspace, id, a.Name, a.Description, a.Instructions, a.SkillSlugs, a.Version).Scan(&a.ID, &a.Version, &a.UpdatedAt)
	}
	a.Slug = "custom-" + a.ID.String()
	return a, err
}

func (s *Service) Delete(ctx context.Context, workspace, id uuid.UUID, version int) error {
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var locked uuid.UUID
	if err = tx.QueryRow(ctx, `SELECT id FROM agents WHERE workspace_id=$1 AND id=$2 AND version=$3 FOR UPDATE`, workspace, id, version).Scan(&locked); err != nil {
		return err
	}
	rows, err := tx.Query(ctx, `SELECT object_key FROM agent_files WHERE agent_id=$1`, id)
	if err != nil {
		return err
	}
	var keys []string
	for rows.Next() {
		var key string
		if err = rows.Scan(&key); err != nil {
			rows.Close()
			return err
		}
		keys = append(keys, key)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `DELETE FROM agents WHERE workspace_id=$1 AND id=$2`, workspace, id); err != nil {
		return err
	}
	var unused []string
	for _, key := range keys {
		var used bool
		if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM conversation_agent_files WHERE object_key=$1)`, key).Scan(&used); err != nil {
			return err
		}
		if !used {
			unused = append(unused, key)
		}
	}
	if err = tx.Commit(ctx); err != nil {
		return err
	}
	if s.Objects != nil {
		for _, key := range unused {
			_ = s.Objects.Delete(ctx, key)
		}
	}
	return nil
}

func (s *Service) Resolve(ctx context.Context, workspace uuid.UUID, slug string) (Agent, error) {
	for _, a := range Builtins {
		if a.Slug == slug {
			return a, nil
		}
	}
	id, err := uuid.Parse(strings.TrimPrefix(slug, "custom-"))
	if err != nil || !strings.HasPrefix(slug, "custom-") {
		return Agent{}, pgx.ErrNoRows
	}
	return s.Get(ctx, workspace, id)
}
