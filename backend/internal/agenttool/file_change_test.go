package agenttool

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
)

func TestPrivateReviewCaptureDoesNotGrantUnseenEditAccess(t *testing.T) {
	env, computer := fileTestEnvironment(map[string][]byte{"note": []byte("first\nsecond\nthird\n")})
	readForTest(t, env, `{"file_path":"note","offset":2,"limit":1}`)
	var snapshots []FileChange
	env.RecordFileChange = func(_ context.Context, change *FileChange) error { snapshots = append(snapshots, *change); return nil }
	if _, err := (Edit{}).Execute(context.Background(), env, json.RawMessage(`{"file_path":"note","old_string":"second","new_string":"updated"}`)); err != nil {
		t.Fatal(err)
	}
	if len(snapshots) != 2 || snapshots[0].Status != "pending" || snapshots[1].Status != "confirmed" || string(snapshots[1].BeforeContent) != "first\nsecond\nthird\n" || string(snapshots[1].AfterContent) != string(computer.files["note"]) {
		t.Fatal("review did not capture actual bytes")
	}
	if _, err := env.Files.ExpectedEdit("note", "third", false); err == nil {
		t.Fatal("private full capture granted an unseen edit")
	}
}
func TestReviewIntentFailurePreventsWriteAndConfirmationFailureDoesNotInviteReplay(t *testing.T) {
	env, computer := fileTestEnvironment(map[string][]byte{})
	env.RecordFileChange = func(context.Context, *FileChange) error { return errors.New("fixture database unavailable") }
	if _, err := (Write{}).Execute(context.Background(), env, json.RawMessage(`{"file_path":"new","content":"real"}`)); err == nil {
		t.Fatal("write started without review intent")
	}
	if _, exists := computer.files["new"]; exists {
		t.Fatal("intent failure changed file")
	}
	env.RecordFileChange = func(_ context.Context, change *FileChange) error {
		if change.Status == "confirmed" {
			return errors.New("receipt lost")
		}
		return nil
	}
	result, err := (Write{}).Execute(context.Background(), env, json.RawMessage(`{"file_path":"new","content":"real"}`))
	if err != nil {
		t.Fatal("successful file write was reported as retryable failure", err)
	}
	value := result.(map[string]any)
	if value["ok"] != true || value["review_saved"] != false || string(computer.files["new"]) != "real" {
		t.Fatal("actual mutation result lost")
	}
}
