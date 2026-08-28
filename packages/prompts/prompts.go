package prompts

import (
	"embed"
	"fmt"
	"strings"
)

//go:embed base.txt platform_rules.txt personas/*.txt
var files embed.FS

func Compose(persona, conversationID, workspaceID, model, computerStatus string) (string, error) {
	parts := make([]string, 0, 4)
	for _, name := range []string{"base.txt", "personas/" + strings.ToLower(persona) + ".txt", "platform_rules.txt"} {
		body, err := files.ReadFile(name)
		if err != nil {
			return "", err
		}
		parts = append(parts, strings.TrimSpace(string(body)))
	}
	runtime := fmt.Sprintf("<runtime>\nconversation_id: %s\nworkspace_id: %s\nagent: %s\nmodel: %s\ncomputer:\n  status: %s\n  workspace_path: /workspace\n</runtime>", conversationID, workspaceID, persona, model, computerStatus)
	parts = append(parts, runtime)
	return strings.Join(parts, "\n\n"), nil
}
