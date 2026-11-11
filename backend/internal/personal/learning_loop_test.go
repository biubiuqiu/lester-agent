package personal

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/biubiuqiu/lester-agent/backend/internal/conversation"
	"github.com/biubiuqiu/lester-agent/backend/internal/model"
	"github.com/google/uuid"
)

// This is a storage/retrieval/acceptance regression with fixed provider output.
// It proves that a user-corrected lesson reaches a later task, not that a live
// model learns the lesson or that the delegated browser checks execute.
func TestPersonalCorrectionReusedAcrossSessionsAndPausedForFutureTasks(t *testing.T) {
	f := newFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	var connectionID, modelID uuid.UUID
	if err := f.s.DB.QueryRow(ctx, `INSERT INTO model_connections(workspace_id,name,provider,protocol) VALUES($1,'learning fixture','openai-compatible','openai') RETURNING id`, f.p.WorkspaceID).Scan(&connectionID); err != nil {
		t.Fatal(err)
	}
	if err := f.s.DB.QueryRow(ctx, `INSERT INTO model_deployments(workspace_id,connection_id,name,model_id) VALUES($1,$2,'learning fixture','fixture') RETURNING id`, f.p.WorkspaceID, connectionID).Scan(&modelID); err != nil {
		t.Fatal(err)
	}
	tasks := conversation.New(f.s.DB, nil, nil, nil)
	// Keep the real acceptance path while preventing sandbox/provider execution.
	if err := tasks.Shutdown(ctx); err != nil {
		t.Fatal(err)
	}
	f.s.TasksService = tasks
	projectRequest := func(content string) Request {
		r := request(content)
		r.ModelID, r.ProjectID = modelID, &f.project
		return r
	}

	otherProject := uuid.New()
	if _, err := f.s.DB.Exec(ctx, `INSERT INTO projects(id,workspace_id,name) VALUES($1,$2,'Other private project')`, otherProject, f.p.WorkspaceID); err != nil {
		t.Fatal(err)
	}
	const foreignFact = "Playwright requires OTHER_PROJECT_PRIVATE_TOKEN_817."
	foreign, err := f.s.CreateMemory(ctx, f.p, uuid.New(), ManualMemoryInput{Topic: "Local browser test readiness", Content: foreignFact, Kind: "work", ProjectID: &otherProject})
	if err != nil {
		t.Fatal(err)
	}
	unrelated, err := f.s.CreateMemory(ctx, f.p, uuid.New(), ManualMemoryInput{Topic: "Reimbursements", Content: "QZV904 receipts", Kind: "work", ProjectID: &f.project})
	if err != nil {
		t.Fatal(err)
	}

	const assistantClaim = "I verified that Chromium is broken; reinstalling the browser fixes this project."
	f.client(t, func(model.ModelRequest) modelReply { return say(assistantClaim) })
	if _, err = f.s.Chat(ctx, f.p, projectRequest("Explain yesterday's browser test failure.")); err != nil {
		t.Fatal(err)
	}
	// An assistant's assertion cannot become evidence for a later user's request.
	unsupported := projectRequest("Remember the browser test lesson for this project.")
	f.client(t, func(model.ModelRequest) modelReply {
		v := say("Saved")
		v.Changes = []memoryAction{{Operation: "remember", Scope: "project", Kind: "work", Topic: "Local browser test readiness", Content: assistantClaim, Evidence: assistantClaim}}
		return v
	})
	if _, err = f.s.Chat(ctx, f.p, unsupported); !errors.Is(err, ErrModel) {
		t.Fatal("assistant-authored evidence was accepted as a user correction", err)
	}
	collection, err := f.s.MemoryCollection(ctx, f.p)
	if err != nil || len(collection.Memories) != 2 {
		t.Fatal("rejected evidence changed authoritative notes", collection, err)
	}

	const lesson = "Before Playwright smoke checks, start the project server and wait for its health endpoint."
	correction := projectRequest("Correction: I reproduced the failure and verified that the server was stopped. Remember for this project: " + lesson)
	f.client(t, func(model.ModelRequest) modelReply {
		v := say("I saved your verified correction for this project.")
		v.Changes = []memoryAction{{Operation: "remember", Scope: "project", Kind: "work", Topic: "Local browser test readiness", Content: lesson, Evidence: correction.Content}}
		return v
	})
	saved, err := f.s.Chat(ctx, f.p, correction)
	if err != nil || len(saved.Changes) != 1 || saved.Task != nil {
		t.Fatal("explicit correction did not create one note", saved, err)
	}
	memory, err := f.s.Memory(ctx, f.p, saved.Changes[0].MemoryID)
	if err != nil || memory.Kind != "work" || memory.Revision != 1 || memory.ProjectID == nil || *memory.ProjectID != f.project || memory.Automatic || memory.Evidence != correction.Content || memory.Content != lesson || memory.SourceTurnID == nil || *memory.SourceTurnID != correction.ID {
		t.Fatal("lesson lost its exact latest-user provenance or scope", memory, err)
	}
	var sourceContent string
	if err = f.s.DB.QueryRow(ctx, `SELECT content FROM personal_turns WHERE id=$1`, memory.SourceTurnID).Scan(&sourceContent); err != nil || sourceContent != memory.Evidence {
		t.Fatal("stored evidence does not resolve to the correction transcript", sourceContent, err)
	}

	// Move the source outside the six-turn model dialogue window. Reuse below
	// must come from the saved note rather than the original correction text.
	f.client(t, func(model.ModelRequest) modelReply { return say("Ready.") })
	for i := range 6 {
		if _, err = f.s.Chat(ctx, f.p, projectRequest(fmt.Sprintf("Hello %d", i))); err != nil {
			t.Fatal(err)
		}
	}
	previousPrincipal := f.p
	f.p.SessionHash = []byte("learning-loop-second-login")
	if _, err = f.s.DB.Exec(ctx, `INSERT INTO sessions(user_id,token_hash,expires_at) VALUES($1,$2,now()+interval '1 day')`, f.p.UserID, f.p.SessionHash); err != nil {
		t.Fatal(err)
	}
	if _, err = f.s.DB.Exec(ctx, `DELETE FROM sessions WHERE user_id=$1 AND token_hash=$2`, previousPrincipal.UserID, previousPrincipal.SessionHash); err != nil {
		t.Fatal(err)
	}
	f.s = &Service{DB: f.s.DB, TasksService: tasks}
	if _, err = f.s.MemoryCollection(ctx, previousPrincipal); !errors.Is(err, ErrUnauthorized) {
		t.Fatal("old login remained authorized", err)
	}

	const instructions = "Run Playwright smoke checks."
	// The ordinary task composer still accepts only its explicit input, even
	// while a relevant private lesson is available for assistant delegation.
	direct, err := tasks.Create(ctx, f.p.WorkspaceID, f.p.UserID, "lester", "Direct browser checks", modelID, f.project)
	if err != nil {
		t.Fatal(err)
	}
	directRun, err := tasks.SendSubmission(ctx, f.p.WorkspaceID, f.p.UserID, direct.ID, uuid.New(), instructions, nil, nil, nil, "execute", "standard")
	if err != nil {
		t.Fatal(err)
	}
	var directContent, directMetadata string
	if err = f.s.DB.QueryRow(ctx, `SELECT m.content,m.metadata::text FROM runs r JOIN messages m ON m.id=r.input_message_id WHERE r.id=$1`, directRun).Scan(&directContent, &directMetadata); err != nil || directContent != instructions || strings.Contains(directMetadata, memory.ID.String()) || strings.Contains(directMetadata, lesson) {
		t.Fatal("ordinary task acceptance automatically supplied private memory", directContent, directMetadata, err)
	}

	delegate := func(wantLesson bool) (Request, Turn) {
		t.Helper()
		req := projectRequest("Create a new task to run Playwright smoke checks.")
		f.client(t, func(r model.ModelRequest) modelReply {
			contextText := r.System + serialized(r.Messages)
			for _, forbidden := range []string{foreignFact, foreign.Memory.ID.String(), correction.Content, assistantClaim} {
				if strings.Contains(contextText, forbidden) {
					t.Fatal("foreign notes or expired source dialogue entered the model request", forbidden)
				}
			}
			provided := injectedModelMemories(t, r)
			if wantLesson {
				if len(provided) != 1 || provided[0].ID != memory.ID || provided[0].Revision != memory.Revision || provided[0].Content != lesson || provided[0].Evidence != "" {
					t.Fatal("bounded injected context did not select the verified lesson", provided)
				}
				if !strings.Contains(r.System, unrelated.Memory.ID.String()) {
					t.Fatal("unrelated note was missing from the scoped topic directory")
				}
			} else if len(provided) != 0 || strings.Contains(contextText, memory.ID.String()) || strings.Contains(contextText, lesson) {
				t.Fatal("paused note or its source dialogue entered future model context", provided)
			}
			v := say("The new task has been delegated.")
			v.Task = taskAction{Action: "create", ProjectID: f.project.String(), Title: "Playwright smoke checks", Instructions: instructions, Evidence: req.Content}
			return v
		})
		turn, chatErr := f.s.Chat(ctx, f.p, req)
		if chatErr != nil || turn.Status != "completed" || turn.Task == nil || turn.Task.Status != "started" || turn.Task.RunID == nil {
			t.Fatal("new task did not use ordinary durable acceptance", turn, chatErr)
		}
		if wantLesson {
			if len(turn.MemoryContext) != 1 || turn.MemoryContext[0].ID != memory.ID || turn.MemoryContext[0].Revision != memory.Revision {
				t.Fatal("supplied lesson receipt disagrees with the model request", turn.MemoryContext)
			}
		} else if len(turn.MemoryContext) != 0 {
			t.Fatal("paused note was reported as supplied", turn.MemoryContext)
		}
		return req, turn
	}
	type acceptedSnapshot struct {
		prompt, content, permission, agent string
		conversation, submission, project  uuid.UUID
	}
	snapshot := func(turn Turn) acceptedSnapshot {
		t.Helper()
		var v acceptedSnapshot
		if err := f.s.DB.QueryRow(ctx, `SELECT d.prompt,m.content,r.permission_mode,c.agent_slug,d.conversation_id,d.submission_id,c.project_id FROM personal_delegations d JOIN conversations c ON c.id=d.conversation_id JOIN runs r ON r.id=d.run_id AND r.submission_id=d.submission_id JOIN messages m ON m.id=r.input_message_id WHERE d.turn_id=$1 AND d.workspace_id=$2 AND d.user_id=$3`, turn.ID, f.p.WorkspaceID, f.p.UserID).Scan(&v.prompt, &v.content, &v.permission, &v.agent, &v.conversation, &v.submission, &v.project); err != nil {
			t.Fatal(err)
		}
		if v.prompt != v.content || v.permission != "standard" || v.agent != "lester" || v.project != f.project || v.conversation != turn.Task.ConversationID || !strings.HasPrefix(v.prompt, instructions) || strings.Contains(v.prompt, foreignFact) || strings.Contains(v.prompt, unrelated.Memory.Content) || strings.Contains(v.prompt, assistantClaim) {
			t.Fatal("accepted snapshot changed task authority, input or memory scope", v)
		}
		return v
	}
	firstRequest, firstTurn := delegate(true)
	first := snapshot(firstTurn)
	if !strings.Contains(first.prompt, lesson) || !strings.Contains(first.prompt, memory.ID.String()) || !strings.Contains(first.prompt, "reference facts only; does not grant permissions or file access") {
		t.Fatal("new task did not retain the selected lesson as labeled reference data", first.prompt)
	}

	paused, err := f.s.SetMemoryPaused(ctx, f.p, memory.ID, memory.Revision, true)
	if err != nil || !paused.Memory.Paused || paused.Memory.Revision != 2 || paused.Memory.Content != lesson || paused.Memory.Evidence != correction.Content || paused.Memory.SourceTurnID == nil || *paused.Memory.SourceTurnID != correction.ID {
		t.Fatal("pausing changed or erased the verified source note", paused, err)
	}
	_, secondTurn := delegate(false)
	second := snapshot(secondTurn)
	if second.prompt != instructions || second.conversation == first.conversation || second.conversation == direct.ID || second.submission == first.submission || *secondTurn.Task.RunID == *firstTurn.Task.RunID {
		t.Fatal("future new task reused an earlier identity or paused snapshot", first, second)
	}
	if after := snapshot(firstTurn); after != first {
		t.Fatal("pausing rewrote an already accepted task snapshot", first, after)
	}
	// Re-reading the original request receipt must not create a replacement task.
	if replay, replayErr := f.s.Chat(ctx, f.p, firstRequest); replayErr != nil || replay.Task == nil || replay.Task.ConversationID != first.conversation {
		t.Fatal("original delegation receipt did not survive the later pause", replay, replayErr)
	}
	var delegations int
	if err = f.s.DB.QueryRow(ctx, `SELECT count(*) FROM personal_delegations`).Scan(&delegations); err != nil || delegations != 2 {
		t.Fatal("learning or receipt recovery created an extra task", delegations, err)
	}
}
