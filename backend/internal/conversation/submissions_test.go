package conversation

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/biubiuqiu/lester-agent/backend/internal/reference"
	"github.com/google/uuid"
)

func TestSubmissionDigestBindsEffectiveInput(t *testing.T) {
	id := uuid.New()
	base := submissionDigest("keep the file", []uuid.UUID{id}, nil, nil)
	if base != submissionDigest("keep the file", []uuid.UUID{id, id}, []uuid.UUID{}, []reference.Input{}) {
		t.Fatal("empty/duplicate normalization changed identity")
	}
	if base != submissionDigest("keep the file", []uuid.UUID{id}, nil, nil, "execute") {
		t.Fatal("mode upgrade changed existing execution receipt hashes")
	}
	if base == submissionDigest("keep the file", []uuid.UUID{id}, nil, nil, "plan") {
		t.Fatal("Plan mode reused an execution receipt")
	}
	for _, changed := range []string{
		submissionDigest("delete the file", []uuid.UUID{id}, nil, nil),
		submissionDigest("keep the file", []uuid.UUID{uuid.New()}, nil, nil),
		submissionDigest("keep the file", []uuid.UUID{id}, []uuid.UUID{uuid.New()}, nil),
		submissionDigest("keep the file", []uuid.UUID{id}, nil, []reference.Input{{Type: "conversation", ID: uuid.New()}}),
	} {
		if changed == base {
			t.Fatal("different request reused an identity")
		}
	}
}

func TestAcceptedSubmissionRetryDoesNotCreateOrReplayRun(t *testing.T) {
	f := newTranscriptFixture(t, false)
	// An unavailable executor must still preserve acceptance identity. It marks
	// this run failed after commit, without making a model/tool call.
	f.service.executor.stopping = true
	ctx := context.Background()
	id := uuid.New()
	first, err := f.service.SendSubmission(ctx, f.workspaceID, f.userID, f.conversationID, id, "do once", nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	for n := 0; n < 3; n++ {
		again, err := f.service.SendSubmission(ctx, f.workspaceID, f.userID, f.conversationID, id, "do once", nil, nil, nil)
		if err != nil || again != first {
			t.Fatalf("retry = %s %v", again, err)
		}
	}
	if _, err := f.service.SendSubmission(ctx, f.workspaceID, f.userID, f.conversationID, id, "different action", nil, nil, nil); !errors.Is(err, ErrSubmissionConflict) {
		t.Fatalf("changed action accepted: %v", err)
	}
	var runs, messages int
	if err = f.service.db.QueryRow(ctx, `SELECT count(*) FROM runs WHERE conversation_id=$1`, f.conversationID).Scan(&runs); err != nil {
		t.Fatal(err)
	}
	if err = f.service.db.QueryRow(ctx, `SELECT count(*) FROM messages WHERE conversation_id=$1 AND role='user'`, f.conversationID).Scan(&messages); err != nil {
		t.Fatal(err)
	}
	if runs != 1 || messages != 1 {
		t.Fatalf("duplicate acceptance: runs=%d messages=%d", runs, messages)
	}
	found, err := f.service.Submission(ctx, f.workspaceID, f.userID, f.conversationID, id)
	if err != nil || found == nil || found.RunID != first || found.MessageID == uuid.Nil {
		t.Fatalf("receipt=%#v %v", found, err)
	}
	for _, scope := range [][3]uuid.UUID{{uuid.New(), f.userID, f.conversationID}, {f.workspaceID, uuid.New(), f.conversationID}, {f.workspaceID, f.userID, uuid.New()}} {
		found, err := f.service.Submission(ctx, scope[0], scope[1], scope[2], id)
		if err != nil || found != nil {
			t.Fatalf("receipt leaked across scope: %#v %v", found, err)
		}
	}
}

func TestConcurrentSubmissionOnlyAcceptsOnce(t *testing.T) {
	f := newTranscriptFixture(t, false)
	f.service.executor.stopping = true
	ctx := context.Background()
	submission := uuid.New()
	var group sync.WaitGroup
	ids := make(chan uuid.UUID, 2)
	errs := make(chan error, 2)
	for n := 0; n < 2; n++ {
		group.Add(1)
		go func() {
			defer group.Done()
			id, err := f.service.SendSubmission(ctx, f.workspaceID, f.userID, f.conversationID, submission, "exactly one acceptance", nil, nil, nil)
			ids <- id
			errs <- err
		}()
	}
	group.Wait()
	close(ids)
	close(errs)
	for err := range errs {
		if err != nil && !errors.Is(err, ErrRunInProgress) {
			t.Fatal(err)
		}
	}
	var count int
	if err := f.service.db.QueryRow(ctx, `SELECT count(*) FROM runs WHERE conversation_id=$1 AND submission_id=$2`, f.conversationID, submission).Scan(&count); err != nil || count != 1 {
		t.Fatalf("count=%d err=%v", count, err)
	}
	first := uuid.Nil
	for id := range ids {
		if id == uuid.Nil {
			continue
		}
		if first != uuid.Nil && id != first {
			t.Fatal("concurrent responses disagree")
		}
		first = id
	}
	again, err := f.service.SendSubmission(ctx, f.workspaceID, f.userID, f.conversationID, submission, "exactly one acceptance", nil, nil, nil)
	if err != nil || again != first {
		t.Fatalf("resolved receipt=%s %v", again, err)
	}
}

func TestAcceptedModelSnapshotAndActiveSwitchBoundary(t *testing.T) {
	f := newTranscriptFixture(t, false)
	f.service.executor.stopping = true
	ctx := context.Background()
	connection, a, b := uuid.New(), uuid.New(), uuid.New()
	if _, err := f.service.db.Exec(ctx, `INSERT INTO model_connections(id,workspace_id,name,provider,protocol) VALUES($1,$2,'fixture','openai','openai');`, connection, f.workspaceID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.service.db.Exec(ctx, `INSERT INTO model_deployments(id,workspace_id,connection_id,name,model_id) VALUES($1,$3,$4,'A','model-a'),($2,$3,$4,'B','model-b')`, a, b, f.workspaceID, connection); err != nil {
		t.Fatal(err)
	}
	if err := f.service.UpdateModel(ctx, f.workspaceID, f.conversationID, a); err != nil {
		t.Fatal(err)
	}
	run, err := f.service.SendSubmission(ctx, f.workspaceID, f.userID, f.conversationID, uuid.New(), "capture model", nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err = f.service.UpdateModel(ctx, f.workspaceID, f.conversationID, b); err != nil {
		t.Fatal(err)
	}
	var pinned uuid.UUID
	if err = f.service.db.QueryRow(ctx, `SELECT model_deployment_id FROM runs WHERE id=$1`, run).Scan(&pinned); err != nil || pinned != a {
		t.Fatalf("changed snapshot=%s %v", pinned, err)
	}
	f.startRun(t, "active")
	if err = f.service.UpdateModel(ctx, f.workspaceID, f.conversationID, a); !errors.Is(err, ErrRunInProgress) {
		t.Fatalf("active model changed: %v", err)
	}
}
