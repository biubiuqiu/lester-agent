package agenttool

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestReadConversationCannotAcceptCallerAuthority(t *testing.T) {
	_, err := (ReadConversation{}).Execute(context.Background(), Environment{}, json.RawMessage(`{"conversation_id":"11111111-1111-4111-8111-111111111111","workspace_id":"22222222-2222-4222-8222-222222222222"}`))
	if err == nil || !strings.Contains(err.Error(), "unknown field") {
		t.Fatal("caller-supplied workspace was accepted")
	}
	if _, err = (ReadConversation{}).Execute(context.Background(), Environment{}, json.RawMessage(`{}`)); err == nil {
		t.Fatal("missing conversation ID accepted")
	}
}
