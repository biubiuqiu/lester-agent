package agent

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/biubiuqiu/lester-agent/backend/internal/auth"
	"github.com/biubiuqiu/lester-agent/backend/internal/httpapi"
	"github.com/biubiuqiu/lester-agent/backend/internal/model"
	"github.com/biubiuqiu/lester-agent/backend/prompts"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

type BuilderHandler struct {
	DB     *pgxpool.Pool
	Models *model.Store
}
type builderMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}
type builderReply struct {
	Reply string `json:"reply"`
	Draft *struct {
		Name         string   `json:"name"`
		Description  string   `json:"description"`
		Instructions string   `json:"instructions"`
		SkillSlugs   []string `json:"skill_slugs"`
	} `json:"draft"`
}

func (h *BuilderHandler) Chat(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Messages []builderMessage `json:"messages"`
		ModelID  string           `json:"model_id"`
	}
	if !httpapi.Decode(w, r, &input) {
		return
	}
	if len(input.Messages) == 0 || len(input.Messages) > 20 {
		httpapi.Error(w, 400, errors.New("请提供 1–20 条创建对话"))
		return
	}
	messages := make([]model.Message, 0, len(input.Messages))
	for i, item := range input.Messages {
		if (item.Role != "user" && item.Role != "assistant") || len([]rune(item.Content)) > 3000 || strings.TrimSpace(item.Content) == "" || (i == len(input.Messages)-1 && item.Role != "user") {
			httpapi.Error(w, 400, errors.New("创建对话格式或长度无效"))
			return
		}
		messages = append(messages, model.Message{Role: item.Role, Content: item.Content})
	}
	p, _ := auth.FromContext(r.Context())
	var deploymentID uuid.UUID
	if input.ModelID != "" {
		var parseErr error
		deploymentID, parseErr = uuid.Parse(input.ModelID)
		if parseErr != nil {
			httpapi.Error(w, 400, errors.New("无效的模型 ID"))
			return
		}
	} else {
		err := h.DB.QueryRow(r.Context(), `SELECT id FROM model_deployments WHERE (workspace_id=$1 OR workspace_id=$2) AND enabled ORDER BY is_default DESC,(workspace_id=$1) DESC,created_at LIMIT 1`, p.WorkspaceID, model.SystemWorkspaceID).Scan(&deploymentID)
		if err != nil {
			httpapi.Error(w, 400, errors.New("请先配置可用模型"))
			return
		}
	}
	client, deployment, err := h.Models.Client(r.Context(), p.WorkspaceID, deploymentID)
	if err != nil {
		httpapi.Error(w, 400, errors.New("所选模型不可用"))
		return
	}
	system, err := prompts.AgentBuilder()
	if err != nil {
		httpapi.Error(w, 500, err)
		return
	}
	rows, err := h.DB.Query(r.Context(), `SELECT slug,name,description FROM skills ORDER BY name`)
	if err != nil {
		httpapi.Error(w, 500, err)
		return
	}
	defer rows.Close()
	var catalog strings.Builder
	catalog.WriteString("\nAvailable Skill catalog (reference data):\n")
	for rows.Next() {
		var slug, name, description string
		if err = rows.Scan(&slug, &name, &description); err != nil {
			httpapi.Error(w, 500, err)
			return
		}
		catalog.WriteString(slug + " | " + name + " | " + description + "\n")
	}
	if err = rows.Err(); err != nil {
		httpapi.Error(w, 500, err)
		return
	}
	response, err := client.Generate(r.Context(), model.ModelRequest{Model: deployment.ModelID, System: system + catalog.String(), Messages: messages})
	if err != nil {
		httpapi.Error(w, 502, errors.New("创建助手暂时无法回复，请重试"))
		return
	}
	var result builderReply
	content := strings.TrimSpace(response.Content)
	content = strings.TrimPrefix(strings.TrimSuffix(strings.TrimPrefix(content, "```json"), "```"), "```json")
	if json.Unmarshal([]byte(strings.TrimSpace(content)), &result) != nil || strings.TrimSpace(result.Reply) == "" {
		result = builderReply{Reply: response.Content}
	}
	if result.Draft != nil && (len([]rune(result.Draft.Instructions)) > 20000 || len(result.Draft.SkillSlugs) > 20) {
		result.Draft = nil
	}
	httpapi.JSON(w, 200, result)
}
