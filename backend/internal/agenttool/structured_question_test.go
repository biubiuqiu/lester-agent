package agenttool

import (
	"context"
	"encoding/json"
	"testing"
)

func TestQuestionToolCarriesOptionalMultiSelectHeaderAndRecommendation(t *testing.T) {
	var observed InteractionRequest
	env := Environment{Interact: func(_ context.Context, request InteractionRequest) (map[string]any, error) {
		observed = request
		return map[string]any{"status": "answered"}, nil
	}}
	raw := json.RawMessage(`{"questions":[{"id":"scope","header":"Scope","prompt":"Choose details","multi_select":true,"options":[{"id":"a","label":"First","recommended":true},{"id":"b","label":"Second"}]}]}`)
	if _, err := (AskUserQuestion{}).Execute(context.Background(), env, raw); err != nil {
		t.Fatal(err)
	}
	if len(observed.Questions) != 1 || !observed.Questions[0].MultiSelect || observed.Questions[0].Header != "Scope" || !observed.Questions[0].Options[0].Recommended {
		t.Fatal("question metadata lost")
	}
	observed.Questions[0].Header = "This header is much too long"
	if err := observed.Validate(); err == nil {
		t.Fatal("unbounded tab label accepted")
	}
	observed.Questions[0].Header = "Scope"
	observed.Questions[0].Options[1].Recommended = true
	if err := observed.Validate(); err == nil {
		t.Fatal("ambiguous recommendations accepted")
	}
}
