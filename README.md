# Lester

**Make it happen.**

An open-source, self-hostable AI agent workspace. Talk to one enduring Lester: it remembers preferences and project decisions, finds earlier work, and delegates concrete work to isolated task conversations. Task Agents use your personal Computer to work with files and commands; preview the results and keep refining them.

[简体中文](README.zh-CN.md) · [Documentation](docs/README.md) · [Quick start](#quick-start) · [Walkthrough](#watch-the-walkthrough) · [GitHub](https://github.com/biubiuqiu/lester-agent) · [Issues](https://github.com/biubiuqiu/lester-agent/issues)

## Features

- **A personal assistant:** a persistent dialogue with private, editable memory and task delegation.
- **Work in projects:** recent conversations, project context, task queues, questions and explicit permissions.
- **See the results:** file tabs, HTML/Markdown previews, source inspection and static-site publishing.
- **Choose your tools:** multiple model providers, custom Agents, @ references, Skills and attachments.
- **Work beyond one visit:** scheduled tasks, inbound webhooks and persistent task records.
- **Use your own deployment:** Web in six languages, plus iPhone/iPad and Android clients.

Lester focuses on conversation-driven work. It does not provide a Workflow/DAG editor or a multi-Agent orchestration UI. See the [user guide](docs/usage.md) for behavior and support boundaries.

## Watch the walkthrough

[![Lester walkthrough: materials, planning, results, cloud tasks and usage](docs/media/lester-walkthrough.gif)](docs/media/lester-walkthrough.mp4)

[Watch the 97-second video](docs/media/lester-walkthrough.mp4) · [Chapters and recording details](docs/walkthrough.md)

The recording uses controlled sample data, not a live model run or speed benchmark. It predates the personal-assistant panel and shows the task workspace; the former Cloud tasks entry is now **Recent conversations → Task status**.

## Quick start

You need Docker, Docker Compose v2 and model-provider credentials. Run commands from the repository root.

**For a fresh installation:**

```bash
cp deploy/.env.example deploy/.env
```

Set `POSTGRES_PASSWORD`, `MASTER_KEY_BASE64`, `SANDBOX_SERVICE_TOKEN` and `MINIO_ROOT_PASSWORD` in `deploy/.env`. Generate independent values:

```bash
openssl rand -hex 24       # POSTGRES_PASSWORD
openssl rand -base64 32    # MASTER_KEY_BASE64
openssl rand -hex 32       # SANDBOX_SERVICE_TOKEN
openssl rand -hex 24       # MINIO_ROOT_PASSWORD
```

```bash
docker compose --env-file deploy/.env -f deploy/docker-compose.yaml up -d --build
```

Open [http://localhost:13000](http://localhost:13000), enter the workspace, register and configure a model in **Settings → Models**. Send your first goal to start a task. The fixed **Lester** entry opens your separate personal assistant.

[Full setup, PowerShell and alternate ports](docs/getting-started.md). Saving model configuration does not verify provider access; the first task confirms the actual call.

**Existing installations:** preserve `.env`, encryption keys and volumes. Back up and apply every missing migration through **031** before running the matching services. Follow the [upgrade checklist](docs/deployment.md#upgrading-an-existing-installation); do not overwrite an existing configuration or delete volumes.

## Documentation

Start with the [documentation index](docs/README.md), or open a chapter:

| Chapter | Covers |
| --- | --- |
| [Getting started](docs/getting-started.md) | Installation, configuration and the first task |
| [User guide](docs/usage.md) | Personal assistant, permissions, queues, schedules and recaps |
| [Projects and files](docs/projects-and-files.md) | Deliverables, previews, file state and published sites |
| [Agents and context](docs/agents-and-context.md) | Custom Agents, @ references, Skills and attachments |
| [Architecture](docs/architecture.md) | Services, Computers, storage and context management |
| [Deployment](docs/deployment.md) | Upgrades, gateway, Docker/ACS and Kubernetes/Helm |
| [Accounts and security](docs/accounts-and-security.md) | OAuth, rotating tokens, avatars and administration |
| [Development and CI](docs/development.md) | Repository layout, local checks and GitHub Actions |
| [Mobile clients](docs/mobile.md) | iPhone, iPad and Android build/install guides |

## Architecture

Web connects through the gateway to the Go API, which owns authentication, task execution, personal memory and delegation. Sandbox Service manages per-user Computers; PostgreSQL stores durable state, Redis delivers live events, and S3-compatible storage holds packages and published files. A separate Artifact Host serves public snapshots.

See the [service diagrams and execution/storage boundaries](docs/architecture.md) and [backend extension guide](backend/ARCHITECTURE.md).

## Deployment

Docker Compose provides a local deployment. Kubernetes/Helm supports external PostgreSQL, Redis and S3-compatible storage, with Docker or ACS Computers. Public deployments require HTTPS and an independent artifact hostname.

Read [Deployment and upgrades](docs/deployment.md), [security configuration](docs/accounts-and-security.md#security-notes) and the [Helm reference](deploy/helm/lester/README.md) before operating a public service.
