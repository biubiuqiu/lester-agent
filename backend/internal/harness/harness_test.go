package harness

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	modelruntime "github.com/biubiuqiu/lester-agent/backend/internal/model/runtime"
)

type testClient struct {
	stream func(context.Context, modelruntime.Request) (<-chan modelruntime.Event, error)
}

func (c testClient) Stream(ctx context.Context, request modelruntime.Request) (<-chan modelruntime.Event, error) {
	return c.stream(ctx, request)
}
func (testClient) Generate(context.Context, modelruntime.Request) (*modelruntime.Response, error) {
	return nil, errors.New("Generate must not be used by the execution loop")
}
func (testClient) Capabilities(context.Context, string) (modelruntime.Capabilities, error) {
	return modelruntime.Capabilities{}, nil
}

func frames(events ...modelruntime.Event) <-chan modelruntime.Event {
	stream := make(chan modelruntime.Event, len(events))
	for _, event := range events {
		stream <- event
	}
	close(stream)
	return stream
}

func call(index int, id, name, args string) *modelruntime.ToolCall {
	return &modelruntime.ToolCall{Index: index, ID: id, Name: name, Arguments: json.RawMessage(args)}
}

func TestCloudAndLocalHostsShareTheFullLoopWithoutATurnLimit(t *testing.T) {
	// The core preserves all turns even if its host projects a much smaller
	// provider request. This covers the previous fixed twelve-turn regression.
	var modelCalls, tools int
	var roles []string
	client := testClient{stream: func(_ context.Context, request modelruntime.Request) (<-chan modelruntime.Event, error) {
		modelCalls++
		if len(request.Messages) > 3 {
			t.Fatal("host projection was not applied")
		}
		if modelCalls <= 15 {
			return frames(modelruntime.Event{ToolCalls: []*modelruntime.ToolCall{call(0, fmt.Sprint(modelCalls), "read", `{"path":"notes.md"}`)}}), nil
		}
		return frames(modelruntime.Event{Delta: "Complete"}), nil
	}}
	hooks := Hooks{
		PrepareTurn: func(_ context.Context, state *State) (modelruntime.Request, error) {
			request := state.Request
			if len(request.Messages) > 3 {
				request.Messages = request.Messages[len(request.Messages)-3:]
			}
			return request, nil
		},
		Assistant: func(_ context.Context, _ Result, message modelruntime.Message) (modelruntime.Message, error) {
			roles = append(roles, message.Role)
			message.Seq = int64(len(roles))
			message.MessageID = fmt.Sprint(message.Seq)
			return message, nil
		},
		ExecuteTool: func(context.Context, modelruntime.ToolCall) (any, error) {
			tools++
			return map[string]any{"content": "retained"}, nil
		},
		ToolResult: func(_ context.Context, call modelruntime.ToolCall, value any, err error) (modelruntime.Message, error) {
			roles = append(roles, "tool")
			message, err := ToolResultMessage(call, value, err)
			message.Seq = int64(len(roles))
			return message, err
		},
	}
	state, err := Run(context.Background(), client, modelruntime.Request{Messages: []modelruntime.Message{{Role: "user", Content: "goal"}}}, hooks)
	if err != nil {
		t.Fatal(err)
	}
	if modelCalls != 16 || tools != 15 || len(state.Request.Messages) != 32 {
		t.Fatalf("loop or canonical history lost: calls=%d tools=%d messages=%d", modelCalls, tools, len(state.Request.Messages))
	}
	if state.Request.Messages[0].Content != "goal" || state.Request.Messages[30].Role != "tool" || state.Request.Messages[31].Content != "Complete" {
		t.Fatal("canonical transcript was pruned or reordered")
	}
}

func TestMultiCallFramesAreAssembledAndPersistedBeforeEachTool(t *testing.T) {
	var order []string
	requests := 0
	client := testClient{stream: func(_ context.Context, request modelruntime.Request) (<-chan modelruntime.Event, error) {
		requests++
		if requests == 2 {
			if len(request.Messages) != 3 || request.Messages[0].Role != "assistant" || request.Messages[1].ToolCallID != "first" || request.Messages[2].ToolCallID != "second" {
				t.Fatal("next turn did not receive persisted batch in canonical order")
			}
			return frames(modelruntime.Event{Delta: "done"}), nil
		}
		return frames(
			modelruntime.Event{ToolCalls: []*modelruntime.ToolCall{call(2, "second", "write", `{"path":`), call(1, "first", "read", `{}`)}, ToolCall: call(2, "second", "write", `{"path":`)},
			modelruntime.Event{ToolCalls: []*modelruntime.ToolCall{call(2, "", "", `"notes.md"}`)}},
		), nil
	}}
	_, err := Run(context.Background(), client, modelruntime.Request{}, Hooks{
		Assistant: func(_ context.Context, _ Result, m modelruntime.Message) (modelruntime.Message, error) {
			order = append(order, "assistant")
			return m, nil
		},
		ExecuteTool: func(_ context.Context, c modelruntime.ToolCall) (any, error) {
			order = append(order, c.ID)
			if !json.Valid(c.Arguments) {
				t.Fatal("invalid tool arguments reached host")
			}
			return "observed", nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(order, []string{"assistant", "first", "second", "assistant"}) {
		t.Fatalf("wrong host order: %v", order)
	}
}

func TestInvalidToolBatchNeverExecutesItsValidPrefix(t *testing.T) {
	for _, invalid := range []*modelruntime.ToolCall{call(2, "first", "read", `{}`), call(2, "second", "", `{}`), call(2, "second", "read", `{"incomplete":`)} {
		t.Run(invalid.ID+invalid.Name, func(t *testing.T) {
			var execution, finish, partial int
			client := testClient{stream: func(context.Context, modelruntime.Request) (<-chan modelruntime.Event, error) {
				return frames(modelruntime.Event{ToolCalls: []*modelruntime.ToolCall{call(1, "first", "read", `{}`), invalid}}), nil
			}}
			_, err := Run(context.Background(), client, modelruntime.Request{}, Hooks{
				ExecuteTool: func(context.Context, modelruntime.ToolCall) (any, error) { execution++; return nil, nil },
				ModelFinished: func(_ context.Context, _ Result, err error) error {
					finish++
					if err == nil {
						t.Fatal("invalid response marked successful")
					}
					return nil
				},
				Partial: func(_ context.Context, r Result) {
					partial++
					if len(r.CallsByIndex) != 2 {
						t.Fatal("partial observations lost")
					}
				},
			})
			if err == nil || execution != 0 || finish != 1 || partial != 1 {
				t.Fatalf("unsafe batch handling err=%v exec=%d finish=%d partial=%d", err, execution, finish, partial)
			}
		})
	}
}

func TestModelRecoveryRequiresExplicitHostApprovalAndNoPartialOutput(t *testing.T) {
	for _, partial := range []bool{false, true} {
		t.Run(fmt.Sprint(partial), func(t *testing.T) {
			attempts, recoveries, finished := 0, 0, 0
			client := testClient{stream: func(context.Context, modelruntime.Request) (<-chan modelruntime.Event, error) {
				attempts++
				if attempts == 1 {
					failure := &modelruntime.ProviderError{Kind: modelruntime.ErrorContextExceeded}
					if partial {
						return frames(modelruntime.Event{Delta: "observed"}, modelruntime.Event{Err: failure}), nil
					}
					return nil, failure
				}
				return frames(modelruntime.Event{Delta: "done"}), nil
			}}
			_, err := Run(context.Background(), client, modelruntime.Request{}, Hooks{
				RecoverModel:  func(context.Context, Result, error) (bool, error) { recoveries++; return true, nil },
				ModelFinished: func(context.Context, Result, error) error { finished++; return nil },
			})
			if partial {
				if err == nil || attempts != 1 || recoveries != 0 || finished != 1 {
					t.Fatal("partial model output was replayed")
				}
			} else if err != nil || attempts != 2 || recoveries != 1 || finished != 2 {
				t.Fatalf("explicit recovery failed: %v %d %d %d", err, attempts, recoveries, finished)
			}
		})
	}
}

func TestPersistenceFailureStopsBeforeExecutingTools(t *testing.T) {
	for _, boundary := range []string{"usage", "assistant"} {
		t.Run(boundary, func(t *testing.T) {
			persistErr := errors.New("persistence unavailable")
			executed := 0
			client := testClient{stream: func(context.Context, modelruntime.Request) (<-chan modelruntime.Event, error) {
				return frames(modelruntime.Event{ToolCall: call(0, "call", "read", `{}`)}), nil
			}}
			hooks := Hooks{ExecuteTool: func(context.Context, modelruntime.ToolCall) (any, error) { executed++; return nil, nil }}
			if boundary == "usage" {
				hooks.ModelFinished = func(context.Context, Result, error) error { return persistErr }
			} else {
				hooks.Assistant = func(context.Context, Result, modelruntime.Message) (modelruntime.Message, error) {
					return modelruntime.Message{}, persistErr
				}
			}
			_, err := Run(context.Background(), client, modelruntime.Request{}, hooks)
			if !errors.Is(err, persistErr) || executed != 0 {
				t.Fatal("tool executed without a durable model receipt")
			}
		})
	}
}

func TestFailedModelReceiptMustPersistBeforeRecovery(t *testing.T) {
	for _, boundary := range []string{"open", "stream", "partial"} {
		t.Run(boundary, func(t *testing.T) {
			providerErr := &modelruntime.ProviderError{Kind: modelruntime.ErrorContextExceeded}
			persistErr := errors.New("usage receipt unavailable")
			attempts, recoveries, partials := 0, 0, 0
			client := testClient{stream: func(context.Context, modelruntime.Request) (<-chan modelruntime.Event, error) {
				attempts++
				if attempts > 1 {
					return frames(modelruntime.Event{Delta: "unexpected recovery"}), nil
				}
				if boundary == "open" {
					return nil, providerErr
				}
				if boundary == "partial" {
					return frames(modelruntime.Event{Delta: "observed"}, modelruntime.Event{Err: providerErr}), nil
				}
				return frames(modelruntime.Event{Err: providerErr}), nil
			}}
			_, err := Run(context.Background(), client, modelruntime.Request{}, Hooks{
				ModelFinished: func(context.Context, Result, error) error { return persistErr },
				RecoverModel: func(context.Context, Result, error) (bool, error) {
					recoveries++
					return true, nil
				},
				Partial: func(_ context.Context, result Result) {
					if result.Text != "" {
						partials++
						if result.Text != "observed" {
							t.Fatal("partial observation changed")
						}
					}
				},
			})
			if !errors.Is(err, persistErr) || !errors.Is(err, providerErr) || attempts != 1 || recoveries != 0 {
				t.Fatalf("recovered without a durable receipt: err=%v attempts=%d recoveries=%d", err, attempts, recoveries)
			}
			if (boundary == "partial") != (partials == 1) {
				t.Fatalf("partial response lost at persistence failure: %d", partials)
			}
		})
	}
}

func TestCancellationRetainsObservedToolResultWithoutReplayingTheBatch(t *testing.T) {
	ctx, cancel := context.WithCancelCause(context.Background())
	cause := errors.New("explicit task cancellation")
	executed, interrupted, stored := 0, 0, 0
	client := testClient{stream: func(context.Context, modelruntime.Request) (<-chan modelruntime.Event, error) {
		return frames(modelruntime.Event{ToolCalls: []*modelruntime.ToolCall{call(0, "one", "write", `{}`), call(1, "two", "write", `{}`)}}), nil
	}}
	state, err := Run(ctx, client, modelruntime.Request{}, Hooks{
		ExecuteTool: func(context.Context, modelruntime.ToolCall) (any, error) {
			executed++
			cancel(cause)
			return map[string]any{"status": "observed"}, nil
		},
		ToolInterrupted: func(_ context.Context, c modelruntime.ToolCall, value any) {
			interrupted++
			if c.ID != "one" || value == nil {
				t.Fatal("observed effect lost")
			}
		},
		ToolResult: func(context.Context, modelruntime.ToolCall, any, error) (modelruntime.Message, error) {
			stored++
			return modelruntime.Message{}, nil
		},
	})
	if !errors.Is(err, cause) || executed != 1 || interrupted != 1 || stored != 0 || len(state.Request.Messages) != 1 {
		t.Fatalf("cancelled batch mishandled: %v %d %d %d", err, executed, interrupted, stored)
	}
}

func TestStreamCancellationFlushesPartialTextWithIndependentCleanupContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	finished, partial := 0, 0
	text := strings.Repeat("x", 2100)
	client := testClient{stream: func(context.Context, modelruntime.Request) (<-chan modelruntime.Event, error) {
		return frames(modelruntime.Event{Delta: text}), nil
	}}
	_, err := Run(ctx, client, modelruntime.Request{}, Hooks{
		Delta: func(eventCtx context.Context, value string) {
			if value != text {
				t.Fatal("delta changed")
			}
			cancel()
		},
		ModelFinished: func(_ context.Context, _ Result, err error) error {
			finished++
			if !errors.Is(err, context.Canceled) {
				t.Fatal("cancelled stream marked successful")
			}
			return nil
		},
		Partial: func(_ context.Context, r Result) {
			partial++
			if r.Text != text {
				t.Fatal("partial text lost")
			}
		},
	})
	if !errors.Is(err, context.Canceled) || finished != 1 || partial != 1 {
		t.Fatalf("cancellation was not finalized once: %v %d %d", err, finished, partial)
	}
}

func TestPendingPartialFlushPreservesContextValuesAfterCancellation(t *testing.T) {
	type contextKey struct{}
	ctx, cancel := context.WithCancel(context.WithValue(context.Background(), contextKey{}, "run context"))
	defer cancel()
	flushed, partial := false, false
	providerErr := errors.New("stream failed")
	client := testClient{stream: func(context.Context, modelruntime.Request) (<-chan modelruntime.Event, error) {
		return frames(modelruntime.Event{Delta: "pending"}, modelruntime.Event{Err: providerErr}), nil
	}}
	_, err := Run(ctx, client, modelruntime.Request{}, Hooks{
		ModelFinished: func(context.Context, Result, error) error { cancel(); return nil },
		Delta: func(eventCtx context.Context, text string) {
			flushed = true
			if eventCtx.Err() != nil || eventCtx.Value(contextKey{}) != "run context" || text != "pending" {
				t.Fatal("cleanup lost the run context or pending delta")
			}
			if _, bounded := eventCtx.Deadline(); !bounded {
				t.Fatal("cleanup context has no deadline")
			}
		},
		Partial: func(_ context.Context, result Result) {
			partial = result.Text == "pending"
		},
	})
	if !errors.Is(err, context.Canceled) || !flushed || !partial {
		t.Fatalf("partial cleanup failed: err=%v flushed=%v partial=%v", err, flushed, partial)
	}
}

func TestFinalResponseBoundaryCanAcceptNewInput(t *testing.T) {
	requests, completions := 0, 0
	client := testClient{stream: func(_ context.Context, r modelruntime.Request) (<-chan modelruntime.Event, error) {
		requests++
		if requests == 2 && (len(r.Messages) != 3 || r.Messages[2].Content != "accepted adjustment") {
			t.Fatal("newly accepted input lost")
		}
		return frames(modelruntime.Event{Delta: "answer"}), nil
	}}
	state, err := Run(context.Background(), client, modelruntime.Request{Messages: []modelruntime.Message{{Role: "user", Content: "initial"}}}, Hooks{
		Complete: func(_ context.Context, state *State) (bool, error) {
			completions++
			if completions == 1 {
				state.Request.Messages = append(state.Request.Messages, modelruntime.Message{Role: "user", Content: "accepted adjustment"})
				return true, nil
			}
			return false, nil
		},
	})
	if err != nil || requests != 2 || len(state.Request.Messages) != 4 {
		t.Fatalf("final-response input race: %v %d", err, requests)
	}
}

func TestToolFailuresRemainModelVisibleAndUsageIsFinalSnapshot(t *testing.T) {
	requests, finished := 0, 0
	client := testClient{stream: func(_ context.Context, r modelruntime.Request) (<-chan modelruntime.Event, error) {
		requests++
		if requests == 1 {
			return frames(modelruntime.Event{NormalizedUsage: &modelruntime.NormalizedUsage{Source: modelruntime.UsageActual, InputTokens: 10}}, modelruntime.Event{NormalizedUsage: &modelruntime.NormalizedUsage{Source: modelruntime.UsageActual, InputTokens: 20}, ToolCall: call(0, "call", "read", `{}`)}), nil
		}
		if !strings.Contains(r.Messages[1].Content, "permission denied") {
			t.Fatal("tool failure was dropped instead of sent to model")
		}
		return frames(modelruntime.Event{Delta: "reported limitation"}), nil
	}}
	_, err := Run(context.Background(), client, modelruntime.Request{}, Hooks{
		ExecuteTool: func(context.Context, modelruntime.ToolCall) (any, error) {
			return map[string]any{"observed": true}, errors.New("permission denied")
		},
		ModelFinished: func(_ context.Context, result Result, _ error) error {
			finished++
			if finished == 1 && result.Usage.InputTokens != 20 {
				t.Fatal("usage snapshots were summed or stale")
			}
			return nil
		},
	})
	if err != nil || requests != 2 || finished != 2 {
		t.Fatalf("tool-error continuation failed: %v", err)
	}
}
