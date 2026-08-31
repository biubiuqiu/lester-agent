package agenttool

import (
	"fmt"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestNumberedRead(t *testing.T) {
	tests := []struct {
		name, input   string
		offset, limit int
		want          string
		total, count  int
		truncated     bool
	}{
		{"empty", "", 1, 20, "", 0, 0, false},
		{"terminal newline", "one\ntwo\n", 1, 20, "     1\tone\n     2\ttwo", 2, 2, false},
		{"blank lines", "\n\n", 1, 20, "     1\t\n     2\t", 2, 2, false},
		{"tabs and unicode", "\t中文\n  x\tvalue", 1, 20, "     1\t\t中文\n     2\t  x\tvalue", 2, 2, false},
		{"range", "one\ntwo\nthree\nfour", 2, 2, "     2\ttwo\n     3\tthree", 4, 2, true},
		{"EOF", "one\n", 2, 20, "", 1, 0, false},
		{"past EOF", "one", 9, 20, "", 1, 0, false},
		{"CRLF preserved", "one\r\ntwo\r\n", 1, 20, "     1\tone\r\n     2\ttwo\r", 2, 2, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := numberedReadResult(tt.input, tt.offset, tt.limit)
			if got["content"] != tt.want || got["total_lines"] != tt.total || got["line_count"] != tt.count || got["truncated"] != tt.truncated {
				t.Fatalf("result = %#v", got)
			}
		})
	}
}

func TestNumberedReadPaginationHasNoGaps(t *testing.T) {
	lines := make([]string, 150)
	for i := range lines {
		lines[i] = fmt.Sprintf("line%d ", i+1) + strings.Repeat("文", 600)
	}
	input := strings.Join(lines, "\n")
	offset := 1
	for offset <= len(lines) {
		got := numberedReadResult(input, offset, 2000)
		body := got["content"].(string)
		if utf8.RuneCountInString(body) > outputCharacterLimit {
			t.Fatal("output exceeds cap")
		}
		count := got["line_count"].(int)
		if count == 0 {
			t.Fatal("pagination made no progress")
		}
		for index, line := range strings.Split(body, "\n") {
			want := fmt.Sprintf("%6d\t%s", offset+index, lines[offset+index-1])
			if line != want {
				t.Fatalf("line mismatch at %d", offset+index)
			}
		}
		offset += count
		if offset <= len(lines) && got["next_offset"] != offset {
			t.Fatalf("next_offset = %v, want %d", got["next_offset"], offset)
		}
	}
}

func TestNumberedReadLongLine(t *testing.T) {
	got := numberedReadResult(strings.Repeat("文", 3001)+"\nnext", 1, 20)
	if got["truncated"] != true || got["omitted_characters"] != 1001 || got["line_count"] != 2 {
		t.Fatalf("result = %#v", got)
	}
	if !strings.HasSuffix(got["content"].(string), "\n     2\tnext") {
		t.Fatal("long line damaged next line numbering")
	}
	if _, ok := got["next_offset"]; ok {
		t.Fatal("long-line truncation must not suggest reading beyond EOF")
	}
}
