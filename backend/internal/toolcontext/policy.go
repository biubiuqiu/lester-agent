package toolcontext

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
)

type DefaultPolicy struct{}

var _ ToolContextPolicy = DefaultPolicy{}

type observation struct {
	name, status, key string
	args, result      map[string]any
}

func (DefaultPolicy) Resolve(exchanges []ToolExchange) []ToolContextItem {
	observations := make([]observation, len(exchanges))
	for i, exchange := range exchanges {
		observations[i] = observe(exchange)
	}
	items := make([]ToolContextItem, len(exchanges))
	laterSuccess := map[string]int{}
	for i := len(exchanges) - 1; i >= 0; i-- {
		exchange, observation := exchanges[i], observations[i]
		// Siblings in the same parallel-call batch cannot prove error resolution.
		successAt, succeeded := laterSuccess[observation.key]
		unresolved := observation.status == "error" && !(succeeded && successAt > exchange.AssistantIndex)
		item := ToolContextItem{Mode: ToolContextFull, Reason: "recent"}
		switch {
		case unresolved:
			item.Pinned, item.Reason = true, "unresolved_error"
		case !exchange.Consumed:
			item.Reason = "unobserved_result"
		case observation.status == "unknown" || !knownTool(observation.name):
			// Unknown/custom tools and load_skill may carry durable instructions.
			item.Reason = "conservative_full"
		case observation.status == "success" && lowValue(observation):
			item.Mode, item.Reason = ToolContextEvicted, "consumed_low_value"
		case i >= len(exchanges)-RecentFullToolExchanges:
			// The window counts exchanges, not messages, batches or only survivors.
		default:
			item.Mode, item.Reason = ToolContextReference, "outside_recent_window"
			item.Reference = reference(exchange, observation, observation.status == "error")
		}
		items[i] = item
		if observation.status == "success" {
			if previous, ok := laterSuccess[observation.key]; !ok || previous < exchange.AssistantIndex {
				laterSuccess[observation.key] = exchange.AssistantIndex
			}
		}
	}
	return items
}

func observe(exchange ToolExchange) observation {
	o := observation{name: canonicalName(exchange.ToolCall.Name), status: "unknown"}
	decoder := json.NewDecoder(bytes.NewReader(exchange.ToolCall.Arguments))
	decoder.UseNumber() // Retry identity must not collapse large integer arguments.
	argsErr := decoder.Decode(&o.args)
	_ = json.Unmarshal([]byte(exchange.ToolResult.Content), &o.result)
	if hasError(o.result) {
		o.status = "error"
	} else if argsErr == nil && o.args != nil {
		switch o.name {
		case "bash":
			if code, ok := number(o.result, "exit_code"); ok && code == 0 {
				o.status = "success"
			} else if field(o.result, "status") == "running" {
				o.status = "running"
			}
		case "read":
			if _, ok := o.result["content"].(string); ok {
				o.status = "success"
			}
		case "edit", "write":
			if o.result["ok"] == true {
				o.status = "success"
			}
		case "list_files":
			var files []json.RawMessage
			if json.Unmarshal([]byte(exchange.ToolResult.Content), &files) == nil && files != nil {
				o.status = "success"
			}
		}
	}
	// Only exact, verifiable retries unpin errors. No semantic error matching.
	keyArgs := o.args
	if o.name == "bash" && field(o.args, "command") != "" {
		keyArgs = map[string]any{"command": field(o.args, "command"), "run_in_background": o.args["run_in_background"] == true}
	} else if o.name == "read" && filePath(o.args) != "" {
		keyArgs = map[string]any{"file_path": filePath(o.args)}
	}
	canonical, _ := json.Marshal(keyArgs)
	if argsErr != nil || keyArgs == nil {
		canonical = exchange.ToolCall.Arguments
	}
	o.key = o.name + ":" + string(canonical)
	return o
}

func hasError(result map[string]any) bool {
	if value, ok := result["error"]; ok && value != nil && value != "" && value != false {
		return true
	}
	if result["is_error"] == true || result["ok"] == false {
		return true
	}
	if code, ok := number(result, "exit_code"); ok && code != 0 {
		return true
	}
	switch field(result, "status") {
	case "error", "failed", "interrupted":
		return true
	}
	return false
}

func canonicalName(name string) string {
	switch name {
	case "computer_exec":
		return "bash"
	case "computer_read_file":
		return "read"
	case "computer_write_file":
		return "write"
	case "computer_list_files":
		return "list_files"
	}
	return name
}

func knownTool(name string) bool {
	switch name {
	case "bash", "read", "write", "edit", "list_files":
		return true
	}
	return false
}

func lowValue(o observation) bool {
	if o.name == "list_files" {
		return true
	}
	if o.name != "bash" || o.args["run_in_background"] == true {
		return false
	}
	// Exact allowlist only: never classify compound commands, redirects, scripts,
	// grep output, or commands with file arguments by a lossy shell prefix match.
	switch strings.TrimSpace(field(o.args, "command")) {
	case "pwd", "ls", "ls -a", "ls -l", "ls -la", "ls -al", "git status", "git status --short", "git status --porcelain":
		return true
	}
	return false
}

func reference(exchange ToolExchange, o observation, resolved bool) string {
	ref := map[string]any{"tool": o.name, "tool_execution_id": exchange.ID, "status": o.status}
	if resolved {
		ref["resolved_by_later_success"] = true
	}
	switch o.name {
	case "read":
		ref["file"] = shortened(filePath(o.args), 240)
		if start, ok := number(o.result, "start_line"); ok {
			if count, ok := number(o.result, "line_count"); ok {
				ref["start_line"], ref["line_count"] = start, count
				if count > 0 {
					ref["end_line"] = start + count - 1
				}
			}
		}
		if value, ok := number(o.result, "next_offset"); ok {
			ref["next_offset"] = value
		}
		if value, ok := o.result["truncated"].(bool); ok {
			ref["truncated"] = value
		}
	case "bash":
		ref["command"] = shortened(field(o.args, "command"), 400)
		if code, ok := number(o.result, "exit_code"); ok {
			ref["exit_code"] = code
		}
		for _, key := range []string{"task_id", "log_path", "pid"} {
			if value := field(o.result, key); value != "" {
				ref[key] = shortened(value, 240)
			}
		}
	case "edit", "write":
		ref["file"] = shortened(filePath(o.args), 240)
		if count, ok := number(o.result, "replacements"); ok {
			ref["replacements"] = count
		}
		// Existing edit results do not contain line ranges; never invent them.
	}
	raw, _ := json.Marshal(ref)
	return string(raw)
}

func field(value map[string]any, key string) string {
	text, _ := value[key].(string)
	return text
}

func number(value map[string]any, key string) (float64, bool) {
	n, ok := value[key].(float64)
	return n, ok
}

func filePath(args map[string]any) string {
	if value := field(args, "file_path"); value != "" {
		return value
	}
	return field(args, "path")
}

func shortened(value string, limit int) string {
	runes := []rune(value)
	if len(runes) > limit {
		return string(runes[:limit]) + fmt.Sprintf("…[%d chars omitted]", len(runes)-limit)
	}
	return value
}
