package personal

import (
	"context"
	"errors"
	"strings"

	"github.com/biubiuqiu/lester-agent/backend/internal/auth"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

func (s *Service) applyMemories(ctx context.Context, tx pgx.Tx, p auth.Principal, req Request, actions []memoryAction) (string, error) {
	notice := ""
	for _, a := range actions {
		var project *uuid.UUID
		if a.Scope == "project" {
			project = req.ProjectID
		}
		var before *Memory
		var m Memory
		var err error
		if a.ID != "" {
			id, _ := uuid.Parse(a.ID)
			m, err = scanMemory(tx.QueryRow(ctx, `SELECT `+memoryColumns+` FROM personal_memories WHERE workspace_id=$1 AND user_id=$2 AND id=$3 FOR UPDATE`, p.WorkspaceID, p.UserID, id))
		} else {
			m, err = scanMemory(tx.QueryRow(ctx, `SELECT `+memoryColumns+` FROM personal_memories WHERE workspace_id=$1 AND user_id=$2 AND project_id IS NOT DISTINCT FROM $3::uuid AND lower(topic)=lower($4) FOR UPDATE`, p.WorkspaceID, p.UserID, project, strings.TrimSpace(a.Topic)))
		}
		if err == nil {
			old := m
			before = &old
			if m.Deleted && a.Automatic {
				notice = "已忘记的记忆未被自动恢复。"
				continue
			}
		} else if !errors.Is(err, pgx.ErrNoRows) {
			return "", err
		}
		if before == nil {
			var count int
			if err = tx.QueryRow(ctx, `SELECT count(*) FROM personal_memories WHERE workspace_id=$1 AND user_id=$2 AND NOT deleted`, p.WorkspaceID, p.UserID).Scan(&count); err != nil {
				return "", err
			}
			if count >= 128 {
				notice = "记忆空间已满，部分信息未保存。可忘记不再需要的记忆后继续。"
				continue
			}
			m, err = scanMemory(tx.QueryRow(ctx, `INSERT INTO personal_memories(workspace_id,user_id,project_id,kind,topic,content,evidence,source_turn_id,automatic) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9) RETURNING `+memoryColumns, p.WorkspaceID, p.UserID, project, a.Kind, strings.TrimSpace(a.Topic), strings.TrimSpace(a.Content), a.Evidence, req.ID, a.Automatic))
		} else {
			if m.Deleted && a.Operation == "remember" {
				var count int
				if err = tx.QueryRow(ctx, `SELECT count(*) FROM personal_memories WHERE workspace_id=$1 AND user_id=$2 AND NOT deleted`, p.WorkspaceID, p.UserID).Scan(&count); err != nil {
					return "", err
				}
				if count >= 128 {
					notice = "记忆空间已满，部分信息未保存。可忘记不再需要的记忆后继续。"
					continue
				}
			}
			m.Content = strings.TrimSpace(a.Content)
			m.Evidence = a.Evidence
			m.SourceTurnID = &req.ID
			m.Automatic = a.Automatic
			m.Deleted = a.Operation == "forget"
			if m.Deleted {
				m.Content = ""
				m.Evidence = ""
			}
			m, err = writeMemory(ctx, tx, p, m)
		}
		if err != nil {
			return "", err
		}
		if _, err = recordChange(ctx, tx, p, before, m, &req.ID); err != nil {
			return "", err
		}
		if a.Operation == "forget" || before != nil {
			if err = barrier(ctx, tx, p); err != nil {
				return "", err
			}
		}
	}
	return notice, nil
}
