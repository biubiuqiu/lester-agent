package prompts

import (
	"strings"
	"testing"
)

func TestCompositionOrder(t *testing.T) {
	prompt, err := Compose("lester", "c", "w", "m", "running")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Index(prompt, "autonomous AI agent") > strings.Index(prompt, "Your name is Lester") || strings.Index(prompt, "Your name is Lester") > strings.Index(prompt, "The Computer") {
		t.Fatal("unexpected prompt order")
	}
}
