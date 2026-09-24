package agent

import (
	"context"
	"errors"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

var ErrNotDesigner = errors.New("only an Agent Designer conversation can save an Agent")

// SaveFromDesigner binds the generated Agent to its durable design conversation.
func (s *Service) SaveFromDesigner(ctx context.Context, conversationID uuid.UUID, a Agent) (Agent, error) {
	if err := s.validate(ctx, a); err != nil {
		return Agent{}, err
	}
	a.Name = strings.TrimSpace(a.Name)
	a.Instructions = strings.TrimSpace(a.Instructions)
	if a.SkillSlugs == nil {
		a.SkillSlugs = []string{}
	}
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return Agent{}, err
	}
	defer tx.Rollback(ctx)
	var workspace uuid.UUID
	var existing *uuid.UUID
	var slug string
	if err = tx.QueryRow(ctx, `SELECT workspace_id,agent_slug,created_agent_id FROM conversations WHERE id=$1 FOR UPDATE`, conversationID).Scan(&workspace, &slug, &existing); err != nil {
		return Agent{}, err
	}
	if slug != "agent-designer" {
		return Agent{}, ErrNotDesigner
	}
	if existing == nil {
		err = tx.QueryRow(ctx, `INSERT INTO agents(workspace_id,name,description,instructions,skill_slugs) VALUES($1,$2,$3,$4,$5) RETURNING id,version,updated_at`, workspace, a.Name, a.Description, a.Instructions, a.SkillSlugs).Scan(&a.ID, &a.Version, &a.UpdatedAt)
		if err != nil {
			return Agent{}, err
		}
		if _, err = tx.Exec(ctx, `UPDATE conversations SET created_agent_id=$2 WHERE id=$1`, conversationID, a.ID); err != nil {
			return Agent{}, err
		}
	} else {
		err = tx.QueryRow(ctx, `UPDATE agents SET name=$3,description=$4,instructions=$5,skill_slugs=$6,version=version+1,updated_at=now() WHERE workspace_id=$1 AND id=$2 RETURNING id,version,updated_at`, workspace, *existing, a.Name, a.Description, a.Instructions, a.SkillSlugs).Scan(&a.ID, &a.Version, &a.UpdatedAt)
		if err == pgx.ErrNoRows {
			return Agent{}, errors.New("the Agent was deleted; start a new design conversation")
		}
		if err != nil {
			return Agent{}, err
		}
	}
	if err = tx.Commit(ctx); err != nil {
		return Agent{}, err
	}
	a.Slug = "custom-" + a.ID.String()
	a.BuilderConversationID = &conversationID
	return a, nil
}
