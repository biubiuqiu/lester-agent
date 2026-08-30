package prompts

import (
	"strings"
	"testing"
)

func TestCompositionOrder(t *testing.T) {
	prompt, err := Compose("lester", "c", "w", "m", "running", []Skill{{Slug: "code-review", Name: "Code Review", Description: "Review code"}})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Index(prompt, "autonomous AI agent") > strings.Index(prompt, "Your name is Lester") || strings.Index(prompt, "Your name is Lester") > strings.Index(prompt, "The Computer") {
		t.Fatal("unexpected prompt order")
	}
	if !strings.Contains(prompt, "<available_skills>") || !strings.Contains(prompt, ".agent/skills/code-review/SKILL.md") {
		t.Fatal("installed skill was not added to the prompt")
	}
}
