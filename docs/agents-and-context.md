# Agents, context and Skills

[Documentation](README.md) · [Project home](../README.md) · [简体中文](zh-CN/agents-and-context.md)

Custom Agents, @ references, Skill packages and attachments.

> Commands run from the repository root unless stated otherwise. For existing databases, follow the [complete upgrade checklist](deployment.md#upgrading-an-existing-installation); feature-specific migration examples are partial steps.

## In this chapter

- [Context library and @ references](#context-library-and--references)
- [Custom Agents](#custom-agents)
- [Skills and attachments](#skills-and-attachments)

## Context library and @ references

Open **Context library** from the account menu, or choose **Manage entries** in the chat context picker. Maintain personal dictionary-style entries with a name, short description, and plain-text or Markdown body. Entries are private to the current workspace, searchable by name and description, and can be edited or deleted. Version checks prevent stale edits from silently overwriting newer changes.

In either a new or existing conversation, type `@` or click **@ Add reference**. Search names and descriptions across **Context**, **Conversation** and **Skill**, or narrow the category with the filters or `@context`, `@conversation` / `@会话`, and `@skill`. Names may contain spaces. Arrow keys and Enter select a result; Escape dismisses the picker without sending. Click a selected chip to inspect it, or use Delete/Backspace to remove a focused chip. Selections survive navigation and refresh with the draft. A message supports eight references across all three types. Only an explicit send starts a task.

- **Context:** The API resolves IDs within the authenticated workspace and saves the latest entry bodies and versions as immutable message metadata. Historical messages and model history use those snapshots after edits or deletion. Each entry supports an 80-character name, a 240-character description and 20,000 characters of body; selected context bodies together may contain 40,000 characters. Names are workspace-unique.
- **Conversation:** The API stores a workspace-scoped source ID, title, project and message sequence cutoff. The `read_conversation` tool reads only sources explicitly referenced by the current run's input message. It returns visible user/assistant text in pages of at most 20 messages and 12,000 Unicode characters, with sequence/character cursors for long messages. Later messages, incomplete responses, internal tool exchanges and source files are excluded. Old references and sources mentioned inside a referenced conversation do not authorize new reads. Deleted, inaccessible and current-conversation sources are rejected.
- **Skill:** The API stores authoritative catalog metadata and installs missing selected packages into the current conversation before model execution. Existing installed copies are preserved; the Agent can then use `load_skill`. Historical selections do not cause installation in a new task. Selecting or previewing a Skill does not install it or call a model. The built-in **Conversation Reader** Skill guides reading and organizing referenced conversations into background, confirmed decisions and next actions, with source message citations.

Invalid references reject the send and preserve the draft. Historical messages display saved reference names. These references do not add automatic retrieval, embeddings, Memory, file copying, automatic publication or replay of earlier actions. No database migration is required for conversation/Skill references; rebuild API and Web to update existing installations. API startup seeds the Conversation Reader Skill alongside the other built-in packages.

For an existing installation, back up PostgreSQL, stop API writes, and apply `backend/migrations/000008_context_library.up.sql` once after migrations 001–007, using the transactional `psql` procedure in [the database upgrade notes](architecture.md#upgrading-an-existing-database). Rebuild API and Web afterward. Fresh Compose volumes apply the migration automatically. Rolling back the library table does not remove snapshots already stored in messages.

## Custom Agents

Open **Agent management** from the account menu to see built-in and personal Agents. Each Agent has a landing page explaining its purpose and linking to a new conversation. In **Create Agent**, write a name, description and system instructions, then choose Skills from the catalog. The separate Agent creation assistant can discuss your goal and suggest an editable draft; applying its suggestion only fills the form, and saving remains an explicit action.

The new-conversation composer defaults to Lester and lets you choose another Agent before sending. Starting from an Agent landing page preselects that Agent. Creation does not produce an empty conversation: the first send creates it. The API checks Agent ownership within the personal workspace and snapshots the selected Agent's name, instructions and Skill slugs into the conversation. Later edits or deletion do not rewrite existing conversations. Selected Skills install into the conversation Computer before its first model run; installation failure stops the run with an error instead of silently omitting a Skill. Existing conversations can still manage their own Skills.

**Create Agent** now starts a normal project conversation with the built-in **Agent Designer**. It asks about the goal and expected behavior first, then proposes a definition and uses its scoped `save_agent` tool after the user agrees. The conversation remains in the project's conversation tree with the usual transcript, files, terminal, and Skills. Once an Agent is saved, an **Agent** tab appears in the right panel for reviewing and editing its name, description, system prompt, Skills, and files. The prompt editor shows line numbers, line/column position, and character count. The file manager accepts up to 20 files per Agent, 10 MiB each and 50 MiB combined. Files live in S3-compatible object storage. At conversation creation, Lester snapshots their object references and, before the first model run, copies the files into `agent-resources/` in that conversation's Computer. The model receives file paths, not automatically injected file contents. Editing or deleting Agent files does not alter earlier conversation snapshots.

For an existing installation, back up PostgreSQL, stop API writes, and apply `backend/migrations/000009_agents.up.sql` once after migration 008. Rebuild API and Web afterward. Fresh Compose volumes apply it automatically. Rollback refuses to remove the schema while custom-Agent conversations exist.

For Agent files, apply `backend/migrations/000010_agent_files.up.sql` once after migration 009 with the same backup and transaction procedure, then rebuild API and Web. Fresh Compose volumes apply it automatically.

For Agent Designer conversations, apply `backend/migrations/000011_agent_designer.up.sql` once after migration 010. The migration links each design conversation to its generated Agent. Fresh Compose volumes apply it automatically.

## Skills and attachments

Skill metadata lives in PostgreSQL and versioned packages in object storage. `backend/internal/blob.Store` is the storage boundary, currently backed by MinIO and replaceable with S3 or another implementation. Startup seeds Code Review, Project Planner, and Data Explorer.

Installation extracts packages into `/workspace/conversations/{conversationId}/.agent/skills/{slug}` and records the relationship. Prompts list installed names, descriptions, and paths only. The agent must call `load_skill` to read `SKILL.md` before use. Uninstalling affects the current conversation only.

Uploads and pasted images are stored in `/workspace/conversations/{conversationId}/.agent/upload`. Messages retain metadata and pass file paths, original names, types, and sizes to the model. Contents are not automatically parsed or injected; the agent reads them when needed.

---

[← Projects, files and publishing](projects-and-files.md) · [Architecture and Computers →](architecture.md)
