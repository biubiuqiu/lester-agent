package agenttool

import (
	"encoding/json"
	"errors"
	"path"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/biubiuqiu/lester-agent/backend/internal/model"
)

// ReadReceipt records what was actually returned, not merely that a file was
// touched. A partial page is useful for a precise edit but never grants a full
// overwrite. Digests come from the provider's same-open-file read.
type ReadReceipt struct {
	Path            string `json:"path"`
	SHA256          string `json:"sha256"`
	StartLine       int    `json:"start_line"`
	LineCount       int    `json:"line_count"`
	TotalLines      int    `json:"total_lines"`
	Complete        bool   `json:"complete"`
	EndsWithNewline bool   `json:"ends_with_newline"`
}

type fileObservation struct {
	sha             string
	complete        bool
	text            string
	lines           map[int]string
	total           int
	endsWithNewline bool
}

// FileState is scoped to one execution. Rebuild only from model-visible,
// durable tool messages; user text and model-supplied digests are not receipts.
type FileState struct {
	mu    sync.Mutex
	files map[string]fileObservation
}

func NewFileState(history []model.Message) *FileState {
	s := &FileState{files: make(map[string]fileObservation)}
	calls := make(map[string]model.ToolCall)
	for _, message := range history {
		if message.Role == "assistant" {
			for _, call := range message.ToolCalls {
				calls[call.ID] = call
			}
		}
		if message.Role != "tool" {
			continue
		}
		call, exists := calls[message.ToolCallID]
		if !exists {
			continue
		}
		var result map[string]any
		if json.Unmarshal([]byte(message.Content), &result) != nil || result["error"] != nil {
			continue
		}
		var args map[string]any
		if json.Unmarshal(call.Arguments, &args) != nil {
			continue
		}
		name := call.Name
		switch name {
		case "read", "computer_read_file":
			s.ObserveRead(result)
		case "write", "computer_write_file":
			if result["ok"] == true {
				filePath, _ := args["file_path"].(string)
				if filePath == "" {
					filePath, _ = args["path"].(string)
				}
				content, exists := args["content"].(string)
				sha, _ := result["sha256"].(string)
				if exists {
					s.RecordWrite(filePath, content, sha)
				}
			}
		case "edit":
			if result["ok"] == true {
				filePath, _ := args["file_path"].(string)
				old, _ := args["old_string"].(string)
				updated, _ := args["new_string"].(string)
				all, _ := args["replace_all"].(bool)
				sha, _ := result["sha256"].(string)
				s.RecordEdit(filePath, old, updated, all, sha)
			}
		}
	}
	return s
}

func validSHA(sha string) bool {
	if len(sha) != 64 {
		return false
	}
	for _, value := range sha {
		if !strings.ContainsRune("0123456789abcdef", value) {
			return false
		}
	}
	return true
}

func (s *FileState) ObserveRead(result map[string]any) {
	if s == nil {
		return
	}
	raw, err := json.Marshal(result["read_receipt"])
	if err != nil {
		return
	}
	var receipt ReadReceipt
	if json.Unmarshal(raw, &receipt) != nil || receipt.Path == "" || !validSHA(receipt.SHA256) {
		return
	}
	content, _ := result["content"].(string)
	text := ""
	observed := make(map[int]string)
	var truncated []int
	truncatedRaw, _ := json.Marshal(result["truncated_lines"])
	_ = json.Unmarshal(truncatedRaw, &truncated)
	isTruncated := map[int]bool{}
	for _, number := range truncated {
		isTruncated[number] = true
	}
	// Image receipts retain their version but never pretend their descriptive
	// text was the complete editable file.
	if result["images"] == nil {
		var lines []string
		for index, line := range strings.Split(content, "\n") {
			if content == "" {
				break
			}
			prefix, original, exists := strings.Cut(line, "\t")
			number, parseErr := strconv.Atoi(strings.TrimSpace(prefix))
			if !exists || parseErr != nil || number != receipt.StartLine+index {
				return
			}
			lines = append(lines, original)
			if !isTruncated[number] {
				observed[number] = original
			}
		}
		if len(lines) != receipt.LineCount {
			return
		}
		text = strings.Join(lines, "\n")
		if receipt.Complete && receipt.EndsWithNewline {
			text += "\n"
		}
	}
	complete := receipt.Complete && receipt.StartLine == 1 && receipt.LineCount == receipt.TotalLines && result["truncated"] != true && result["images"] == nil
	s.mu.Lock()
	defer s.mu.Unlock()
	key := path.Clean(receipt.Path)
	previous, exists := s.files[key]
	if exists && previous.sha == receipt.SHA256 && previous.complete {
		return
	}
	if !exists || previous.sha != receipt.SHA256 || previous.total != receipt.TotalLines {
		previous = fileObservation{sha: receipt.SHA256, lines: make(map[int]string), total: receipt.TotalLines, endsWithNewline: receipt.EndsWithNewline}
	}
	if complete {
		previous.complete, previous.text = true, text
	} else {
		for number, value := range observed {
			previous.lines[number] = value
		}
		indices := make([]int, 0, len(previous.lines))
		for number := range previous.lines {
			indices = append(indices, number)
		}
		sort.Ints(indices)
		var body strings.Builder
		last := 0
		for _, number := range indices {
			if last > 0 {
				if number == last+1 {
					body.WriteByte('\n')
				} else {
					body.WriteString("\x00")
				}
			}
			body.WriteString(previous.lines[number])
			last = number
		}
		previous.text = body.String()
		previous.complete = result["images"] == nil && len(indices) == receipt.TotalLines && (receipt.TotalLines == 0 || indices[0] == 1 && indices[len(indices)-1] == receipt.TotalLines)
		if previous.complete && previous.endsWithNewline {
			previous.text += "\n"
		}
	}
	s.files[key] = previous
}

func (s *FileState) RecordWrite(filePath, content, sha string) {
	if s == nil || !validSHA(sha) {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.files[path.Clean(filePath)] = fileObservation{sha: sha, complete: true, text: content}
}

func (s *FileState) RecordEdit(filePath, old, updated string, all bool, sha string) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	key := path.Clean(filePath)
	previous, exists := s.files[key]
	if !exists || !validSHA(sha) || !previous.complete {
		delete(s.files, key) // Re-read after a partial edit; do not invent unseen text.
		return
	}
	count := 1
	if all {
		count = -1
	}
	previous.text = strings.Replace(previous.text, old, updated, count)
	previous.sha = sha
	s.files[key] = previous
}

func (s *FileState) ExpectedWrite(filePath string) (string, bool, error) {
	if s == nil {
		return "", true, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	observation, exists := s.files[path.Clean(filePath)]
	if !exists {
		return "", true, nil
	}
	if !observation.complete {
		return "", false, errors.New("read the complete current file before overwriting it; a partial page is not a complete read")
	}
	return observation.sha, false, nil
}

func (s *FileState) ExpectedEdit(filePath, old string, all bool) (string, error) {
	if s == nil {
		return "", errors.New("read the file before editing it")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	observation, exists := s.files[path.Clean(filePath)]
	if !exists || !strings.Contains(observation.text, old) {
		return "", errors.New("read the current file portion containing old_string before editing it")
	}
	if strings.ContainsRune(old, '\x00') {
		return "", errors.New("old_string must be text without NUL characters")
	}
	if !observation.complete && all {
		return "", errors.New("read an untruncated complete file before replace_all")
	}
	return observation.sha, nil
}
