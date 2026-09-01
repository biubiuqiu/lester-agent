package agenttool

import (
	"fmt"

	"github.com/biubiuqiu/lester-agent/backend/internal/sandbox"
)

const outputCharacterLimit = 30000

func truncateText(value string, limit int) (string, bool, int) {
	runes := []rune(value)
	if len(runes) <= limit {
		return value, false, 0
	}
	tail := min(4000, limit/4)
	head := limit - tail
	omitted := len(runes) - limit
	notice := fmt.Sprintf("\n\n[Output truncated: %d characters omitted. Showing the beginning and end.]\n\n", omitted)
	return string(runes[:head]) + notice + string(runes[len(runes)-tail:]), true, omitted
}

func limitCommandResult(result *sandbox.CommandResult) map[string]any {
	stdout, stdoutTruncated, stdoutOmitted := truncateText(result.Stdout, outputCharacterLimit)
	stderr, stderrTruncated, stderrOmitted := truncateText(result.Stderr, outputCharacterLimit)
	providerTruncated := result.StdoutTruncated || result.StderrTruncated
	limited := map[string]any{"exit_code": result.ExitCode, "stdout": stdout, "stderr": stderr, "duration_ms": result.DurationMS, "truncated": stdoutTruncated || stderrTruncated || providerTruncated}
	if stdoutTruncated || stderrTruncated || providerTruncated {
		limited["omitted_characters"] = stdoutOmitted + stderrOmitted
		limited["omitted_bytes_before_tool_limit"] = result.StdoutOmittedBytes + result.StderrOmittedBytes
		limited["notice"] = "Command output was truncated. Run a narrower command or redirect output to a file and read it in chunks."
	}
	return limited
}
