# Backend Architecture

The API composition root is `cmd/api/main.go`. It creates infrastructure dependencies, registers Agent tools and model providers, and passes those abstractions into application services. Domain services must not select concrete tools or providers with `switch` statements.

## Agent tools

Agent tools live in `internal/agenttool/`.

- `Handler` is the extension contract: a tool provides a model-facing `Definition` and an `Execute` implementation.
- `Registry` owns stable ordering, name lookup, and compatibility aliases.
- `Environment` contains only per-call runtime state and the narrow sandbox interface.
- Each tool keeps its input type, validation, schema, execution, and focused helpers in its own file.
- Hidden aliases support old in-flight tool names without exposing them to new model requests.

To add a tool:

1. Add one file implementing `agenttool.Handler`.
2. Use a typed input struct and reject unknown JSON fields.
3. Register it in `NewDefaultRegistry`; add aliases only when compatibility requires them.
4. Add focused handler tests. Do not edit the conversation execution loop.

The conversation service remains responsible for run state, model turns, durable events, and connecting a tool result to the correct model call ID. It delegates all tool-specific behavior to the registry.

## Durable transcript

`conversation/transcript.go` owns durable messages, model-history reconstruction, run ownership, and interruption recovery. `messages` stores user input, intermediate/final assistant text, assistant `tool_calls`, and tool results with `tool_call_id` and `tool_name`. `seq` is allocated per conversation by a PostgreSQL trigger, and `(conversation_id,seq)` is unique. New rows link to `runs`; legacy text rows keep nullable run links because prior tool transcripts cannot be reconstructed honestly.

The runtime persists assistant calls before executing them and persists each tool result before the next model request. Results are the complete model-visible tool response, including bounded-output notices, not an unbounded copy of underlying files/stdout. Generic tool completion/failure events now include the result and call ID as well, but are UI/audit projections, not the history source of truth. Existing COMMAND/FILE events remain compatible.

Runs record the input message and a credential-free snapshot of system text, tool definitions, model ID, output settings, and `history_through_seq`. Subsequent messages reproduce the within-run transcript. This is not a provider wire-payload recorder or an automatic retry/checkpoint engine.

One dedicated PostgreSQL session per active run holds a conversation advisory lock; it does not hold a pool slot or transaction. Overlapping sends return 409 without storing input. The database must be direct or session-pooled. A watchdog cancels execution if the session is lost. When a new owner acquires the guard, orphaned runs are failed and missing tool results receive explicit interrupted/unknown records before accepting new input. Fenced transcript writes reject terminal runs. Tools may already have produced side effects during an interruption: do not claim exactly-once execution or automatically replay them.

The default conversation HTTP response filters internal messages for compatibility with the existing timeline. `?include_internal=true` exposes all stored records after the same workspace authorization. Model reconstruction always uses the complete ordered transcript, except explicitly incomplete streamed responses.

`read` uses cat-n-style line prefixes and bounded contiguous pages, with an exact `next_offset`. Very long lines are individually marked truncated; their remainder requires a narrower inspection. File content/indentation is never rewritten by numbering.

## Model integrations

Model code is split into three layers:

- `internal/model/runtime/` contains provider-neutral request, event, response, capability, and client contracts.
- `internal/model/integration/` contains the provider registry, shared streaming protocol adapters, and provider-specific builders.
- `internal/model/` persists connections/deployments and resolves credentials, then delegates client construction to the integration registry.

A model provider implements three small concerns through `integration.Provider`:

1. stable provider name and protocol;
2. default endpoint construction;
3. creation of a provider-neutral runtime client.

OpenAI-compatible and Anthropic-compatible transports reuse shared streaming clients. Azure OpenAI, Vertex Anthropic, Foundry Anthropic, and Bedrock keep their authentication and endpoint behavior inside their own integrations. Adding a provider must not add provider branches to `model.Store` or the conversation service.

## Composition and testing

Registries are assembled explicitly in `cmd/api/main.go`, so available capabilities are visible at startup. Registry metadata, typed argument validation, handler composition, endpoint construction, and both OpenAI/Anthropic streaming adapters have focused tests. Full verification remains `go test ./...` from `backend/`.

Set `LESTER_TEST_DATABASE_URL` to a disposable PostgreSQL database to run transcript integration tests. Each test creates and cleans up its own uniquely named schema. These tests cover migration/backfill/rollback, read/edit persistence and replay, event results, run guards, sequence allocation, failed tools, partial streams, and interrupted tool recovery. PostgreSQL integration tests are currently opt-in; CI does not provision a database.
