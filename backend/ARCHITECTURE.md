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
