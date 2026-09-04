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

The default conversation HTTP response filters internal messages for compatibility with the existing timeline. `?include_internal=true` exposes all stored records after the same workspace authorization. Model-history reconstruction loads the complete ordered transcript, except explicitly incomplete streamed responses; each model request then applies the tool-context projection described below.

`read` uses the Sandbox Service's streaming line-range endpoint rather than loading an entire file into API memory. It uses cat-n-style line prefixes and bounded contiguous pages, with an exact `next_offset`. Very long lines are individually marked truncated; their remainder requires a narrower inspection. File content/indentation is never rewritten by numbering. Raw file access is capped at 25 MiB, directory listings at 500 entries, and command stdout/stderr at 256 KiB per stream before the model-facing character limit is applied.

Docker-backed file operations run through `cmd/lester-toolbox`, a static Go helper bundled in the Sandbox Service image and copied into each new or existing user Computer. The helper owns resolved-path checks, bounded listing/read/write behavior, streaming line pages, atomic writes, exact-string edits, and content digests. `agenttool.Edit` calls `Provider.EditFile`, so file contents no longer make an API-side round trip. DockerProvider may use `docker exec` as transport, but it must not generate ad-hoc Python or shell programs for filesystem semantics. ACSProvider uses the official OpenKruise Go E2B SDK and its runtime file/process APIs behind the same contract.

`sandbox.Provider` owns lifecycle, command, file, and interactive terminal behavior. Its `provider_ref` is opaque: Docker uses the stable logical user ID while ACS returns a generated Sandbox ID. The API persists the returned reference before data-plane work. A per-user PostgreSQL advisory transaction lock fences create/recovery across API replicas; the process-local lock is only an optimization. Provider selection occurs once in `cmd/sandbox-service` through `SANDBOX_PROVIDER`, so future cloud adapters do not add branches to conversations or public HTTP handlers.

ACS supports both Native and Private routing. Native is the production default and requires wildcard DNS/TLS; Private uses the `/kruise` path layout for simpler internal/test deployments. ACS pause/resume backs idle suspension. The adapter reconnects before data-plane operations so paused sandboxes wake and fresh runtime access tokens are used; tokens are never stored in Lester's database. Snapshot/volume portability and automatic migration between Docker and ACS are not implemented.

`MODEL_DELTA` events are coalesced by time/size before PostgreSQL insertion. Each inserted event is published to both its conversation channel and authenticated Workspace channel. The browser keeps one Workspace SSE for all live conversation states and loads the selected conversation's durable event history through JSON. SSE subscribes to Redis before reading durable history, sends numeric event IDs, honors `Last-Event-ID`, deduplicates the subscribe/query overlap, and bounds every history replay to the most recent 1,200 events. PostgreSQL remains authoritative; Redis only carries events that have already been inserted.

Sandbox Service is an internal trust boundary. `/healthz` is public for probes, while every lifecycle, command, file, and terminal route requires the shared `SANDBOX_SERVICE_TOKEN`. The API adds this token to HTTP and WebSocket upstream requests. Deployment configuration must keep the service private even though the token provides defense in depth.

## Tool context projection

`internal/toolcontext` is a provider-independent, deterministic projection over the full runtime message history. The conversation loop retains the full history, calls `Build` before **every** `Stream`, and sends a separate projected copy. It never persists references over original messages. `runs.context` records the policy version/window, and `MODEL_STARTED` records mode counts and before/after content-plus-argument character counts (not token estimates).

The unit is a ToolExchange: one assistant call plus its matching result. Calls are counted in transcript/call order, matched inside their contiguous assistant batch, and results retain their original order. Reused IDs across runs are supported; local `Message.RunID` provenance gives references a `run_id:tool_call_id` execution identifier and is not sent as a provider wire field. Incomplete/mismatched/duplicate pairs fail before the provider call; interruption repair remains the transcript layer's responsibility.

The default policy protects the last 10 exchanges. The latest unobserved batch is entirely FULL even if larger than 10. After a later assistant response proves consumption, successful list_files and a small exact bash command allowlist may be EVICTED early. Older known tools become REFERENCE: original calls **and** results are removed together, and bounded, JSON-escaped historical metadata is added to the originating assistant text. This also removes large edit/write input bodies, while preserving unrelated prose and FULL siblings. Read ranges use actual result metadata, not requested limits; edit results currently expose replacement counts, not line ranges. Bash searches use command/exit-code references rather than guessing file hits from stdout. Background references preserve task/log handles. Unknown tools, load_skill and unrecognized results remain FULL.

Unresolved errors are PIN+FULL, including nonzero shell exit codes when tool execution itself returned no Go error. Only a later batch's successful matching operation unpins them: exact bash command/background mode, exact read path, or canonical full arguments for other tools. This is intentionally conservative, not semantic resolution; an unrelated success or launched background process is insufficient. Multiple failures and loaded skill instructions can therefore exceed the window. There is no conversation compaction, relevance model, token ceiling, or historical-result retrieval tool. References are historical observations, not current filesystem facts or permission to re-execute side effects.

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
