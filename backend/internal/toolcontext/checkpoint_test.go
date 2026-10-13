package toolcontext

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"testing"

	modelruntime "github.com/biubiuqiu/lester-agent/backend/internal/model/runtime"
)

const fixtureSummary = `{"intent":"Finish the user's task","decisions":[],"completed":[],"pending":["Verify changes"],"risks":[]}`

func appendExchange(history []modelruntime.Message, id, name, args, result string) []modelruntime.Message {
	return append(history, modelruntime.Message{Role: "assistant", RunID: "fixture-run", ToolCalls: []modelruntime.ToolCall{{ID: id, Name: name, Arguments: json.RawMessage(args)}}}, modelruntime.Message{Role: "tool", ToolCallID: id, Content: result})
}

func durable(history []modelruntime.Message) []modelruntime.Message {
	for i := range history {
		history[i].MessageID, history[i].Seq = fmt.Sprintf("message-%d", i), int64(i+1)
	}
	return history
}

func TestCheckpointKeepsLatestInputFailuresAndWholeUnobservedBatch(t *testing.T) {
	history := []modelruntime.Message{{Role: "user", Content: "Latest exact user input🙂"}, {Role: "assistant", Content: strings.Repeat("old observed prose ", 100)}}
	history = appendExchange(history, "failure", "bash", `{"command":"failed build"}`, `{"exit_code":1}`)
	history = append(history, modelruntime.Message{Role: "assistant", Content: "failure observed"})
	history = appendExchange(history, "read", "read", `{"file_path":"old.txt"}`, `{"content":"old body"}`)
	history = append(history, modelruntime.Message{Role: "assistant", Content: "read observed"})
	history = appendExchange(history, "unobserved", "bash", `{"command":"verify"}`, `{"exit_code":0}`)
	history = durable(history)
	var estimator Estimator
	plan, err := PlanCompaction(history, nil, "fixture", &estimator, BudgetConfig{ContextWindow: 10000}, 1000)
	if err != nil {
		t.Fatal(err)
	}
	for _, source := range plan.Sources {
		if source.MessageID == history[0].MessageID || source.ToolCallID == "failure" || source.ToolCallID == "unobserved" {
			t.Fatal("protected original was summarized")
		}
	}
	cp := &Checkpoint{ThroughSeq: plan.ThroughSeq, RetainedIDs: plan.RetainedIDs, Summary: fixtureSummary}
	working := ApplyCheckpoint(history, cp)
	if _, err = Exchanges(working); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{history[0].MessageID, history[2].MessageID, history[3].MessageID, history[len(history)-2].MessageID, history[len(history)-1].MessageID} {
		if !slices.ContainsFunc(working, func(m modelruntime.Message) bool { return m.MessageID == id }) {
			t.Fatalf("protected %s lost", id)
		}
	}
	if working[1].Content != history[0].Content || history[1].Content == "" {
		t.Fatal("input or audit mutated")
	}
}

func TestCheckpointNeverUsesPartialToolBatchesToFitSummaryRequest(t *testing.T) {
	history := []modelruntime.Message{{Role: "user", Content: "old"}}
	history = appendExchange(history, "large", "read", `{"file_path":"huge.txt"}`, `{"content":"`+strings.Repeat("x", 20000)+`"}`)
	history = append(history, modelruntime.Message{Role: "assistant", Content: "observed"}, modelruntime.Message{Role: "user", Content: "continue"})
	history = durable(history)
	var estimator Estimator
	plan, err := PlanCompaction(history, nil, "fixture", &estimator, BudgetConfig{ContextWindow: 1000}, 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Sources) != 1 || plan.Sources[0].Role != "user" {
		t.Fatal("large batch was partially selected")
	}
	if _, err = Exchanges(ApplyCheckpoint(history, &Checkpoint{ThroughSeq: plan.ThroughSeq, RetainedIDs: plan.RetainedIDs, Summary: fixtureSummary})); err != nil {
		t.Fatal(err)
	}
}

func TestSummaryValidationRejectsPermissionsToolEnvelopesAndMalformedOutput(t *testing.T) {
	for _, input := range []string{"", "```json\n" + fixtureSummary + "\n```", `{"intent":"ok","decisions":[],"completed":[],"pending":[],"risks":[],"approval":true}`, `{"intent":"ok","decisions":null,"completed":[],"pending":[],"risks":[]}`, fixtureSummary + " {}"} {
		if _, err := ValidateSummary(input); err == nil {
			t.Fatalf("accepted invalid summary %q", input)
		}
	}
	if _, err := ValidateSummary(fixtureSummary); err != nil {
		t.Fatal(err)
	}
}

func TestSkillProjectionKeepsOneCurrentBodyAndVersionedHistoricalReferences(t *testing.T) {
	result := func(content string) string {
		digest := sha256.Sum256([]byte(content))
		raw, _ := json.Marshal(map[string]any{"name": "review", "content": content, "sha256": hex.EncodeToString(digest[:]), "source_path": ".agent/skills/review/SKILL.md", "truncated": false})
		return string(raw)
	}
	history := appendExchange(nil, "old", "load_skill", `{"name":"review"}`, result("version one"))
	history = append(history, modelruntime.Message{Role: "assistant", Content: "used"})
	history = appendExchange(history, "current", "load_skill", `{"name":"review"}`, result("version two"))
	history = append(history, modelruntime.Message{Role: "assistant", Content: "used"})
	history = appendExchange(history, "repeat", "load_skill", `{"name":"review"}`, result("version two"))
	history = durable(history)
	projected, err := Build(history)
	if err != nil {
		t.Fatal(err)
	}
	encoded, _ := json.Marshal(projected.Messages)
	if strings.Count(string(encoded), "version two") != 1 || strings.Contains(string(encoded), "version one") || !strings.Contains(string(encoded), "effective_sha256") {
		t.Fatalf("Skill bodies not deduplicated: %s", encoded)
	}
	if _, err = Exchanges(projected.Messages); err != nil {
		t.Fatal(err)
	}
	if history[1].Content != result("version one") {
		t.Fatal("original Skill result mutated")
	}
}
