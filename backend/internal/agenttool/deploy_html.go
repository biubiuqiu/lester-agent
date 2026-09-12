package agenttool

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/biubiuqiu/lester-agent/backend/internal/artifact"
	"github.com/biubiuqiu/lester-agent/backend/internal/model"
	"github.com/google/uuid"
)

type DeployHTML struct{ Service *artifact.Service }

func (DeployHTML) Definition() model.Tool {
	return model.Tool{Name: "deploy_html", Description: "Publish a static HTML site and its local images, video, CSS and JS from this conversation to durable hosting. Returns a public URL and a stable artifact_id. Save that artifact_id and pass it on later calls to update the same deployment and keep its URL. Use only when the user asks to deploy, publish, share, or obtain an external link; creating a file alone is not a publication request. A single HTML file publishes its static dependency closure; a directory publishes its static files with index.html by default. Never include secrets. Files max 25 MiB each, site max 100 MiB / 256 files. Explicit references outside the selected site directory are also collected when they remain inside this conversation, including .agent/upload attachments. Dynamic resource paths should use a complete directory and relative URLs. A loopback URL is reachable on the host computer, not from inside the sandbox; do not invent or change the hosting origin.", InputSchema: map[string]any{"type": "object", "properties": map[string]any{
		"source_path": map[string]any{"type": "string", "description": "HTML file or static site directory relative to this conversation. Use . for the conversation root only when its static files are intended for publication."},
		"entry":       map[string]any{"type": "string", "description": "For directory deployment, HTML entry relative to that directory; defaults to index.html."},
		"name":        map[string]any{"type": "string", "description": "Human-readable site name."},
		"artifact_id": map[string]any{"type": "string", "description": "Optional existing artifact UUID from a previous deployment in this conversation."},
	}, "required": []string{"source_path", "name"}, "additionalProperties": false}}
}
func (d DeployHTML) Execute(ctx context.Context, e Environment, raw json.RawMessage) (any, error) {
	var in artifact.PublishInput
	if err := decodeArguments(raw, &in); err != nil {
		return nil, err
	}
	var workspace uuid.UUID
	if err := d.Service.DB.QueryRow(ctx, `SELECT workspace_id FROM conversations WHERE id=$1`, e.ConversationID).Scan(&workspace); err != nil {
		return nil, errors.New("conversation not found")
	}
	ctx, cancel := context.WithTimeout(ctx, 3*time.Minute)
	defer cancel()
	result, err := d.Service.Publish(ctx, workspace, e.ConversationID, in)
	if err == nil && e.Emit != nil {
		e.Emit("ARTIFACT_PUBLISHED", map[string]any{"artifact_id": result.ID, "name": result.Name, "url": result.URL, "file_count": result.FileCount})
	}
	return result, err
}
