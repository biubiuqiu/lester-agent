# Development and CI

[Documentation](README.md) · [Project home](../README.md) · [简体中文](zh-CN/development.md)

Repository layout, local validation and GitHub Actions.

## In this chapter

- [Repository layout](#repository-layout)
- [Development checks](#development-checks)
- [Continuous integration](#continuous-integration)

## Repository layout

```text
lester-agent/
├── frontend/                     Next.js application
│   ├── src/
│   └── Dockerfile
├── backend/                      Go application
│   ├── cmd/api/                  API entry point
│   ├── cmd/sandbox-service/      Sandbox Service entry point
│   ├── cmd/lester-toolbox/       Static Computer filesystem helper
│   ├── internal/
│   │   ├── agenttool/            Tool registry and independent handlers
│   │   ├── toolboxfs/            Safe filesystem operations and protocol
│   │   └── model/                Model storage, contracts, and providers
│   ├── prompts/                  Embedded system prompts
│   ├── migrations/               PostgreSQL migrations
│   ├── Dockerfile.api
│   ├── Dockerfile.sandbox-runtime
│   └── Dockerfile.sandbox-service
├── deploy/                       Compose, environment templates, Helm
├── docs/                         Chapter-based documentation and technical notes
│   ├── README.md                 Documentation index
│   ├── zh-CN/                    Chinese chapters
│   └── media/                    Walkthrough video, animation, and poster
├── AGENTS.md                     Coding-agent development agreement
├── Makefile
├── README.zh-CN.md               Chinese project overview
└── README.md
```

Frontend and backend have separate dependencies, build contexts, and Dockerfiles. API and Sandbox Service share the Go module but compile into separate executables and containers. Tools extend a registry through independent schemas/handlers; model providers register through `internal/model/integration.Provider`. Stores and conversation runtime do not contain provider-specific branches. See [backend architecture](../backend/ARCHITECTURE.md) for extension boundaries.

The runtime imposes no global model output-token cap. OpenAI-compatible providers omit `max_tokens` when unset. Protocols that require a limit, including Anthropic, Vertex Anthropic, and Bedrock Anthropic, use adapter-specific fallbacks.

## Development checks

Backend (Go 1.26.9):

```bash
cd backend
go mod tidy
go vet ./...
go test ./...
go build ./cmd/...
```

Commit the changes made by `go mod tidy`, including direct/indirect dependency classifications. Backend CI reruns tidy and requires the committed module files to stay unchanged; passing local tests alone does not satisfy this check.


Frontend (pnpm 10.17.1):

```bash
cd frontend
pnpm install --frozen-lockfile
pnpm test # includes six-language catalogue validation
pnpm lint
pnpm build
```

Or from the repository root:

```bash
make test
make web-check
```

After frontend changes, rebuild the running Web service and reload the browser:

```bash
docker compose --env-file deploy/.env -f deploy/docker-compose.yaml up -d --no-deps --build web
```

For Helm changes:

```bash
helm lint deploy/helm/lester --set secrets.existingSecret=lester-test-secrets
helm template lester deploy/helm/lester --set secrets.existingSecret=lester-test-secrets
```

## Continuous integration

GitHub Actions runs on pushes to `main`, pull requests, and manual dispatch. New commits cancel older runs for the same event/branch or PR. Documentation-only changes skip unrelated builds; unknown paths or an unavailable Git comparison run all checks. CI configuration changes also validate workflows with actionlint and run the full suite.

| Check | When it runs | What it verifies |
| --- | --- | --- |
| Backend | Backend changes | Committed module files, gofmt, go vet, uncached unit/PostgreSQL integration tests, all `cmd` builds |
| Frontend | Frontend changes | Frozen dependencies, tests, ESLint, production build/type checking, desktop/mobile Chromium regressions |
| Gateway / Compose | Gateway, Compose or deployment environment template changes | Config resolution, routing/cookies, preview isolation, uploads, SSE and WebSocket fixtures |
| Helm | Chart changes | Chart lint and rendering with artifact ingress |
| Go dependency security | Backend changes and weekly | Reachable vulnerable symbols via govulncheck |
| Sandbox runtime image | Runtime/toolbox/provider changes | Build, dependency audit, offline non-root browser/toolchain checks, persistent volume and terminal integration |
| Frontend dependency security | Package/lockfile/pnpm configuration changes and weekly | High/critical production advisories via pnpm audit |

Browser regressions use the production standalone bundle and fixed REST/SSE fixtures; no live models, OAuth providers or email delivery are involved. After building, run `pnpm exec playwright install chromium` then `pnpm test:e2e` in `frontend/`. Desktop and mobile Chromium cover login errors, explicit first-task creation, preserved drafts, failed sends, live file tabs and isolated HTML previews. They also check all six languages across the homepage and every help chapter, translated server HTML without JavaScript, onboarding, and language switching with retained drafts and attachments. Tests do not retry failures; CI retains failure screenshots/traces for seven days.

Security scans have their own job names and remain failures when vulnerabilities or scan errors occur. Monday's scheduled run (03:23 UTC) scans dependencies even when code has not changed; **Run workflow** runs everything. Jobs use Ubuntu 24.04, Node 22, the Go version in `backend/go.mod`, and pnpm from `frontend/package.json`. Integration tests use a disposable PostgreSQL service, never production data or live model-provider credentials. CI does not deploy the application.

Use **CI result** as the required branch check if enabling branch protection: it accepts intentionally skipped jobs and rejects failed/cancelled checks or a failed selection job. Existing required check names may need updating to match this workflow. Local workflow validation and selection tests:

```bash
python3 -m unittest discover -s .github/scripts -p 'test_*.py' -v
go run github.com/rhysd/actionlint/cmd/actionlint@v1.7.12
```

---

[← Accounts, administration and security](accounts-and-security.md) · [Mobile clients →](mobile.md)
