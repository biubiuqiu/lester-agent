package conversation

import (
	"context"
	"errors"
	"github.com/biubiuqiu/lester-agent/backend/internal/reference"
	"github.com/google/uuid"
)

func (s *Service) installSelectedSkills(ctx context.Context, workspaceID uuid.UUID, conversation Conversation, runID uuid.UUID, messages []Message, computer *Computer) error {
	// Only this run's explicit selections activate Skills; history is data.
	var inputID uuid.UUID
	var err error
	if err = s.db.QueryRow(ctx, `SELECT r.input_message_id FROM runs r JOIN conversations c ON c.id=r.conversation_id WHERE r.id=$1 AND c.id=$2 AND c.workspace_id=$3`, runID, conversation.ID, workspaceID).Scan(&inputID); err != nil {
		return err
	}
	for _, message := range messages {
		if message.ID != inputID {
			continue
		}
		selected, decodeErr := reference.DecodeSkills(message.Metadata["skills"])
		if decodeErr != nil {
			return decodeErr
		}
		for _, item := range selected {
			var installed bool
			if err = s.db.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM conversation_skills WHERE conversation_id=$1 AND skill_id=$2)`, conversation.ID, item.ID).Scan(&installed); err != nil {
				return err
			}
			if installed {
				continue
			}
			if s.installAgentSkill == nil {
				return errors.New("selected Skill installer unavailable")
			}
			if err = s.installAgentSkill(ctx, workspaceID, conversation.CreatedBy, conversation.ID, computer.SandboxID, computer.WorkDir, item.Slug); err != nil {
				return err
			}
			s.event(ctx, runID, conversation.ID, "SKILL_INSTALLED", map[string]any{"slug": item.Slug, "name": item.Name})
		}
		break
	}

	return nil
}
