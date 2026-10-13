package toolcontext

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"path"
)

// Keep one effective, complete body for each loaded Skill. Older successful
// versions remain factual historical references with retrieval provenance.
// An installation, an error, a truncated body or a user claim is not a load.
func deduplicateSkills(exchanges []ToolExchange, items []ToolContextItem) {
	latest := map[string]string{}
	for i := len(exchanges) - 1; i >= 0; i-- {
		exchange := exchanges[i]
		if exchange.ToolCall.Name != "load_skill" {
			continue
		}
		var result struct {
			Name, Content, SHA256 string
			SourcePath            string `json:"source_path"`
			Truncated             bool
			Error                 any
		}
		var args struct {
			Name string `json:"name"`
		}
		if json.Unmarshal([]byte(exchange.ToolResult.Content), &result) != nil || json.Unmarshal(exchange.ToolCall.Arguments, &args) != nil || args.Name == "" || result.Name != args.Name || result.Content == "" || result.Truncated || result.Error != nil || result.SourcePath != path.Join(".agent/skills", result.Name, "SKILL.md") {
			continue
		}
		digest := sha256.Sum256([]byte(result.Content))
		if result.SHA256 != hex.EncodeToString(digest[:]) {
			continue
		}
		if current, ok := latest[result.Name]; ok && exchange.Consumed && !items[i].Pinned {
			ref, _ := json.Marshal(map[string]any{"tool": "load_skill", "name": result.Name, "sha256": result.SHA256, "effective_sha256": current, "source_path": result.SourcePath, "tool_execution_id": exchange.ID, "result_message_id": exchange.ToolResult.MessageID, "status": "historical_loaded", "notice": "Full instructions are present in the later successful load; this is not an installation or new permission."})
			items[i] = ToolContextItem{Mode: ToolContextReference, Reference: string(ref), Reason: "superseded_skill_body"}
		} else {
			latest[result.Name] = result.SHA256
		}
	}
}
