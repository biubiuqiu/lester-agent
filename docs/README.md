# Lester documentation

[Project home](../README.md) · [简体中文](zh-CN/README.md)

Start with installation, then choose a chapter for the work you want to do. Commands run from the repository root unless a code block changes directories. These repository chapters cover operation and development; `/docs` on a running deployment remains the six-language in-app help.

## Chapters

| Chapter | Contents |
| --- | --- |
| [Getting started](getting-started.md) | Install Lester and run your first task. |
| [User guide](usage.md) | Personal assistant, task controls, permissions, schedules and recaps. |
| [Projects, files and publishing](projects-and-files.md) | Project workspaces, deliverables, previews and published sites. |
| [Agents, context and Skills](agents-and-context.md) | Custom Agents, @ references, Skill packages and attachments. |
| [Architecture and Computers](architecture.md) | Services, sandboxes, storage, events and model context. |
| [Deployment and upgrades](deployment.md) | Upgrade checklist, gateway, Docker/ACS and Kubernetes/Helm. |
| [Accounts, administration and security](accounts-and-security.md) | Registration, OAuth, tokens, avatars, admin access and security. |
| [Development and CI](development.md) | Repository layout, local validation and GitHub Actions. |
| [Mobile clients](mobile.md) | iPhone, iPad and Android setup and support boundaries. |
| [Walkthrough](walkthrough.md) | The video, timestamps, sample-data limits and recording source. |

## Reading paths

- **First use:** [Getting started](getting-started.md) → [User guide](usage.md) → [Projects and files](projects-and-files.md).
- **Operate a deployment:** [Upgrade checklist](deployment.md#upgrading-an-existing-installation) → [Accounts and security](accounts-and-security.md) → [Helm reference](../deploy/helm/lester/README.md).
- **Contribute:** [Architecture](architecture.md) → [Development and CI](development.md) → [Backend extension guide](../backend/ARCHITECTURE.md).

## Technical notes and research

These existing notes retain their own scope and language. They complement the chapters above.

- [Personal assistant memory and delegation](personal-assistant.md)
- [Automations, webhooks and structured questions](automations-and-questions.md)
- [Claude Code alignment](claude-code-alignment.md)
- [Cloud Agent priorities](cloud-agent-priorities.md)
- [Security audit, 2026-10-10](security-audit-2026-10-10.md)
- [Context management research: from abstraction to implementation](research/agent-context-management.html)
- [Walkthrough recording and reproduction](media/README.md)
- [iPhone/iPad build guide](../ios/README.md) · [Android build guide](../android/README.md)

## Maintaining these docs

Keep the root READMEs concise. Update the chapter that owns a behavior or configuration, both language indexes and the matching language chapter when applicable. Update the authored in-app help alongside product changes. Keep relative links, media paths and heading anchors valid; do not put complete feature specifications or historical migration blocks back in the project overview.
