package deliverable

import (
	"strings"
	"testing"
)

func TestRejectUnsafeDeliverableEntries(t *testing.T) {
	for _, entry := range []string{"../outside.md", "report/../../outside.md", "/outside.html", ".agent/upload/private.md", "agent-resources/prompt.md", "node_modules/package/index.html", "a\\b.html", "a\x00.md", "a//b.md", "script.js"} {
		if _, _, err := Validate(Input{EntryPath: entry, Title: "Report"}); err == nil {
			t.Errorf("accepted unsafe entry %q", entry)
		}
	}
	for _, entry := range []string{"site/index.html", "report.md", "PAGE.HTM"} {
		if _, _, err := Validate(Input{EntryPath: entry, Title: "Valid deliverable"}); err != nil {
			t.Errorf("rejected %q: %v", entry, err)
		}
	}
	if _, _, err := Validate(Input{EntryPath: "report.md", Title: strings.Repeat("中", 121)}); err == nil {
		t.Fatal("oversized title accepted")
	}
}
