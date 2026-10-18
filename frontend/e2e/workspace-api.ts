import { createServer, type ServerResponse } from "node:http";
import type { AddressInfo } from "node:net";
import type { BrowserContext, Route } from "@playwright/test";
import type { Conversation, ContextEntry, FileEntry, Message } from "../src/lib/api";

export const conversationId = "11111111-1111-4111-8111-111111111111";
export const prompt = "制作一个介绍 Lester 的网页，并保存一份说明。";
export const html = '<!doctype html><html lang="zh-CN"><head><meta charset="utf-8"><title>模拟任务成果</title></head><body><h1>浏览器回归产物</h1><p>这是固定测试文件，不是真实模型生成。</p></body></html>';

const user = { user_id: "22222222-2222-4222-8222-222222222222", workspace_id: "fixture-workspace", email: "browser@example.test", display_name: "浏览器测试", avatar_key: "forest", role: "member", email_verified: true, has_password: true };
const project = { id: "fixture-project", name: "个人项目", is_default: true, pinned: false, conversation_count: 0, created_at: "2026-01-01T00:00:00Z" };
const deployment = { id: "fixture-model", connection_id: "fixture-connection", name: "模拟模型", model_id: "fixture", is_default: true, enabled: true, shared: false };

export class WorkspaceApi {
  mutations: { path: string; body: Record<string, unknown> }[] = [];
  unexpected: string[] = [];
  hasModel = true;
  modelName = deployment.name;
  failCreate = false;
  failSend = false;
  currentPermissionMode: "standard" | "auto" | "full" = "standard";
  permissionRevision = 0;
  waitForCreate?: Promise<void>;
  waitForConversation?: Promise<void>;
  conversation: Conversation | null = null;
  messages: Message[] = [];
  files: FileEntry[] = [];
  contextEntries: ContextEntry[] = [];
  referenceConversations: Conversation[] = [];
  referenceMessages: Record<string, Message[]> = {};
  skillCatalog: { id: string; slug: string; name: string; description: string; version: string; source: string; size_bytes: number }[] = [];
  installedSkillSlugs = new Set<string>();
  failReferenceSearch = false;
  private fileContents = new Map<string, string>();
  private nextEventId = 5;
  private completed = false;
  private submissions = new Map<string, { run_id: string; status: string; message_id: string }>();
  private streams = new Set<ServerResponse>();
  private eventJournal: { id: number; data: string }[] = [];
  private server = createServer((request, response) => {
    response.writeHead(200, { "Content-Type": "text/event-stream", "Cache-Control": "no-store", "Access-Control-Allow-Origin": "http://127.0.0.1:13020", "Access-Control-Allow-Credentials": "true" });
    response.write(": fixture connected\n\n");
    const requested = Number(new URL(request.url || "/events", "http://127.0.0.1").searchParams.get("last_event_id") || 0);
    const cursor = Number.isSafeInteger(requested) && requested >= 0 ? requested : 0;
    // Match production replay: events emitted while a route replaces its
    // stream remain available to the next connection without test sleeps.
    for (const event of this.eventJournal) if (event.id > cursor) response.write(event.data);
    this.streams.add(response);
    response.on("close", () => this.streams.delete(response));
  });

  async install(context: BrowserContext) {
    await new Promise<void>(resolve => this.server.listen(0, "127.0.0.1", resolve));
    const streamURL = `http://127.0.0.1:${(this.server.address() as AddressInfo).port}/events`;
    await context.route("**/api/v1/**", async route => {
      if (new URL(route.request().url()).pathname === "/api/v1/events") {
        await route.continue({ url: streamURL + new URL(route.request().url()).search });
        return;
      }
      await this.respond(route);
    });
  }

  async close() {
    this.server.closeAllConnections();
    await new Promise<void>((resolve, reject) => this.server.close(error => error ? reject(error) : resolve()));
  }

  get streamCount() { return this.streams.size; }

  skillInstalled(slug: string) {
    this.installedSkillSlugs.add(slug);
    this.emit(this.nextEventId++, "SKILL_INSTALLED", { slug });
  }

  failRun(reason = "fixture tool failure") {
    if (!this.conversation) throw new Error("A task must be created first");
    this.conversation.run_status = "failed";
    this.emit(this.nextEventId++, "RUN_FAILED", { error: reason });
  }

  finish() {
    if (!this.conversation) throw new Error("A task must be explicitly created first");
    const now = new Date().toISOString();
    this.completed = true;
    this.conversation.run_status = "completed";
    this.messages.push({ id: "fixture-assistant", role: "assistant", content: "已保存 index.html 和 notes.txt，请检查成果。", created_at: now });
    this.files = ["notes.txt", "index.html"].map(path => ({ path, name: path, is_dir: false, size: 200, modified_at: now }));
    for (const file of this.files) this.emit(this.nextEventId++, "FILE_UPDATED", { path: file.path });
    this.emit(this.nextEventId++, "RUN_COMPLETED", {});
  }

  permissionEvent(type: "PERMISSION_REQUESTED" | "PERMISSION_RESPONDED" | "PERMISSION_REVIEW_STARTED" | "PERMISSION_REVIEW_COMPLETED" | "PERMISSION_MODE_CHANGED", payload: Record<string, unknown> = {}) {
    this.emit(this.nextEventId++, type, payload);
  }

  changePermissionMode(mode: "standard" | "auto" | "full") {
    this.currentPermissionMode = mode; this.permissionRevision++;
    this.permissionEvent("PERMISSION_MODE_CHANGED", { permission_mode: mode, revision: this.permissionRevision });
  }

  planModeEvent() { this.emit(this.nextEventId++, "EXECUTION_MODE_CHANGED", { execution_mode: "plan", reason: "Fixture architectural change" }); }

  modelEvent(type: "MODEL_STARTED" | "MODEL_DELTA" | "MODEL_COMPLETED", payload: Record<string, unknown> = {}) {
    this.emit(this.nextEventId++, type, payload);
  }

  contextEvent(type: "CONTEXT_COMPACTING" | "CONTEXT_COMPACTED" | "CONTEXT_COMPACTION_FAILED", payload: Record<string, unknown> = {}) {
    this.emit(this.nextEventId++, type, payload);
  }

  private emit(id: number, type: string, payload: Record<string, unknown>) {
    const event = { id, type, payload, conversation_id: conversationId, run_id: "fixture-run", created_at: new Date().toISOString() };
    const data = `id: ${id}\ndata: ${JSON.stringify(event)}\n\n`;
    this.eventJournal.push({ id, data });
    for (const stream of this.streams) stream.write(data);
  }

  writeFile(path: string, content: string) {
    const id = this.nextEventId++;
    this.fileContents.set(path, content);
    const file = { path, name: path.split("/").at(-1)!, is_dir: false, size: Buffer.byteLength(content), modified_at: new Date(Date.now() + id * 1000).toISOString() };
    this.files = [...this.files.filter(item => item.path !== path), file];
    this.emit(id, "FILE_UPDATED", { path });
  }

  private async respond(route: Route) {
    const request = route.request(), url = new URL(request.url()), rawPath = url.pathname, path = rawPath.replace(/(\/conversations\/[^/]+)\/permissions\/(messages|inputs)(?=\/|$)/, "$1/$2");
    const method = request.method();
    const reply = (data: unknown, status = 200, headers: Record<string, string> = {}) => route.fulfill({ status, contentType: "application/json", body: JSON.stringify(data), headers });
    if (path === "/api/v1/auth/options") return reply({ providers: [], registration_enabled: true, email_verification_required: false, password_reset_enabled: false });
    if (path === "/api/v1/auth/login" && method === "POST") {
      const body = request.postDataJSON();
      this.mutations.push({ path: rawPath, body });
      if (body.email !== user.email || body.password !== "fixture-password-123") return reply({ error: "邮箱或密码不正确" }, 401);
      return reply({}, 200, { "set-cookie": "lester_access_token=browser-test-access; HttpOnly; SameSite=Lax; Path=/" });
    }
    if (!request.headers().cookie?.includes("lester_access_token=browser-test-access")) return reply({ code: "access_required", error: "需要登录" }, 401);
    if (path === "/api/v1/auth/session") return reply({ access_expires_at: new Date(Date.now() + 7200000).toISOString(), refresh_expires_at: new Date(Date.now() + 2592000000).toISOString() });
    if (path === "/api/v1/tasks") return reply({ tasks: [], next_cursor: "", attention_count: 0, observed_at: new Date().toISOString() });
    if (path === "/api/v1/me") return reply(user);
    if (path === "/api/v1/me/guides") return reply({ guides: ["welcome", "models", "projects", "files", "agents", "contexts", "computer", "skills", "profile", "publishing"].map(topic => ({ topic, step: 0, status: "completed" })) });
    if (/^\/api\/v1\/me\/guides\/(welcome|models|projects|files|agents|contexts|computer|skills|profile|publishing)$/.test(path) && method === "PATCH") {
      const body = request.postDataJSON(); this.mutations.push({ path: rawPath, body }); return reply({ topic: path.split("/").at(-1), ...body });
    }
    if (path === "/api/v1/projects") return reply({ projects: [{ ...project, conversation_count: this.conversation ? 1 : 0 }] });
    if (path === "/api/v1/model-deployments") return reply({ deployments: this.hasModel ? [{ ...deployment, name: this.modelName }] : [] });
    if (path === `/api/v1/conversations/${conversationId}/tasks` && method === "GET") return reply({ tasks: [] });
    if (path === "/api/v1/model-connections") return reply({ connections: [] });
    if (path === "/api/v1/agents") return reply({ agents: [{ id: "fixture-agent", slug: "lester", name: "Lester", description: "模拟 Agent", instructions: "", skill_slugs: [], version: 1, builtin: true, updated_at: project.created_at }] });
    if (path === "/api/v1/conversations") {
      if (method === "GET") return reply({ conversations: this.conversation ? [this.conversation] : [] });
      if (method === "POST") {
        const body = request.postDataJSON();
        this.mutations.push({ path: rawPath, body });
        await this.waitForCreate;
        if (this.failCreate) return reply({ error: "模拟会话创建失败" }, 503);
        this.conversation = { id: conversationId, workspace_id: user.workspace_id, created_by: user.user_id, project_id: project.id, agent_slug: "lester", agent_name: "Lester", model_deployment_id: deployment.id, title: String(body.title), pinned: false, run_status: "idle", created_at: new Date().toISOString(), updated_at: new Date().toISOString() };
        return reply(this.conversation, 201);
      }
    }
    if (path === "/api/v1/references") {
      if (this.failReferenceSearch) return reply({ error: "引用对象加载失败，请重试。" }, 503);
      const kind = url.searchParams.get("type") || "all", q = (url.searchParams.get("q") || "").toLowerCase();
      const items = [
        ...this.contextEntries.map(item => ({ type: "context", id: item.id, title: item.title, description: item.description, version: String(item.version) })),
        ...[...this.referenceConversations, ...(this.conversation ? [this.conversation] : [])].filter(item => item.id !== url.searchParams.get("exclude_conversation")).map(item => ({ type: "conversation", id: item.id, title: item.title, description: "个人项目", project: "个人项目" })),
        ...this.skillCatalog.map(item => ({ type: "skill", id: item.id, title: item.name, description: item.description, slug: item.slug, version: item.version, source: item.source })),
      ].filter(item => (kind === "all" || kind === item.type) && `${item.title} ${item.description} ${"slug" in item ? item.slug : ""}`.toLowerCase().includes(q)).slice(0, 24);
      return reply({ items });
    }
    if (path.startsWith("/api/v1/contexts/")) {
      const item = this.contextEntries.find(item => item.id === path.split("/").at(-1));
      return item ? reply(item) : reply({ error: "引用对象已不可用，请移除后重新选择。" }, 404);
    }
    if (path.startsWith("/api/v1/references/conversations/")) {
      const source = this.referenceConversations.find(item => item.id === path.split("/").at(-1));
      const messages = source ? this.referenceMessages[source.id] || [] : [];
      return source ? reply({ conversation: { id: source.id, title: source.title, project: "个人项目", max_seq: messages.length }, messages: messages.slice(0, 5), has_more: messages.length > 5 }) : reply({ error: "引用的会话已删除或不可访问，请移除后重新选择" }, 404);
    }
    if (path.startsWith("/api/v1/references/skills/")) {
      const item = this.skillCatalog.find(item => item.id === path.split("/").at(-1));
      return item ? reply({ ...item, type: "skill", title: item.name }) : reply({ error: "引用对象已不可用，请移除后重新选择。" }, 404);
    }
    const prefix = `/api/v1/conversations/${conversationId}`;
    if (path === `${prefix}/runs/fixture-run/permissions`) {
      if (method === "PUT") {
        const body = request.postDataJSON(); this.mutations.push({ path: rawPath, body });
        if (body.revision !== this.permissionRevision) return reply({ error: "stale permission revision" }, 409);
        this.changePermissionMode(body.permission_mode);
      }
      return reply({ run_id: "fixture-run", permission_mode: this.currentPermissionMode, revision: this.permissionRevision, status: this.conversation?.run_status || "running" });
    }
    if (path === prefix && this.conversation) {
      if (method === "PATCH") {
        const body = request.postDataJSON();
        this.mutations.push({ path: rawPath, body });
        this.conversation.model_deployment_id = String(body.model_deployment_id);
        return reply(this.conversation);
      }
      await this.waitForConversation;
      return reply({ conversation: this.conversation, messages: this.messages, active_run: this.conversation.run_status === "running" || this.conversation.run_status === "cancelling" ? { id: "fixture-run", status: this.conversation.run_status } : null });
    }
    if (path === `${prefix}/messages` && method === "POST") {
      const body = request.postDataJSON();
      this.mutations.push({ path: rawPath, body });
      if (this.failSend) return reply({ error: "模拟首条消息发送失败" }, 503);
      if (!this.conversation) throw new Error("Message sent before creating a conversation");
      this.conversation.run_status = "running"; this.conversation.run_id = "fixture-run";
      this.currentPermissionMode = body.permission_mode || "standard"; this.permissionRevision = 0;
      const references = (body.references || []) as { type: string; id: string }[];
      const contexts = this.contextEntries.filter(item => ((body.context_ids || []) as string[]).includes(item.id));
      const conversations = this.referenceConversations.filter(item => references.some(ref => ref.type === "conversation" && ref.id === item.id)).map(item => ({ id: item.id, title: item.title, project: "个人项目", max_seq: (this.referenceMessages[item.id] || []).length, updated_at: item.updated_at }));
      const skills = this.skillCatalog.filter(item => references.some(ref => ref.type === "skill" && ref.id === item.id));
      this.messages = [{ id: "fixture-user-message", role: "user", content: String(body.content), metadata: JSON.parse(JSON.stringify({ contexts, conversations, skills })), created_at: new Date().toISOString() }];
      if (typeof body.submission_id === "string") this.submissions.set(body.submission_id, { run_id: "fixture-run", status: "running", message_id: "fixture-user-message" });
      return reply({ run_id: "fixture-run" });
    }
    if (path.startsWith(`${prefix}/submissions/`) && method === "GET") {
      const result = this.submissions.get(path.split("/").at(-1)!);
      return result ? reply({ ...result, status: this.conversation?.run_status || result.status }) : reply({ error: "not found" }, 404);
    }
    if (path === `${prefix}/events/history`) return reply({ events: [] });
    if (path === `${prefix}/file-changes`) return reply({ changes: [] });
    if (path === `${prefix}/inputs`) return reply({ inputs: [] });
    if (path === `${prefix}/permissions` && method === "GET") return reply({ permissions: [] });
    if (path === `${prefix}/interactions`) return reply({ interactions: [], execution_mode: "execute" });
    if (path === `${prefix}/computer`) return reply({ conversation_id: conversationId, user_id: user.user_id, provider: "docker", status: "running" });
    if (path === `${prefix}/files`) return reply({ files: this.files });
    if (path === `${prefix}/files/content`) {
      const file = url.searchParams.get("path");
      if (!this.files.some(item => item.path === file)) return reply({ error: "模拟文件不存在" }, 404);
      return route.fulfill({ status: 200, contentType: "application/octet-stream", body: this.fileContents.get(file ?? "") ?? (file === "index.html" ? html : "纯文本说明：请人工验收生成文件。") });
    }
    if (path === `${prefix}/deliverables`) return reply({ deliverables: this.completed ? [{ id: "fixture-deliverable", conversation_id: conversationId, run_id: "fixture-run", title: "Lester 介绍页", summary: "固定模拟任务成果，需要人工验收。", entry_path: "index.html", kind: "html", content_sha256: "a".repeat(64), created_at: new Date().toISOString(), updated_at: new Date().toISOString() }] : [] });
    if (path === "/api/v1/contexts" || path === `${prefix}/contexts`) return reply({ entries: this.contextEntries });
    if (path === "/api/v1/skills") return reply({ skills: this.skillCatalog });
    if (path === `${prefix}/skills`) return reply({ skills: this.skillCatalog.filter(item => this.installedSkillSlugs.has(item.slug)) });
    if (path === `${prefix}/artifacts`) return reply({ artifacts: [] });
    this.unexpected.push(`${method} ${path}`);
    return reply({ error: "Unexpected browser fixture request" }, 501);
  }
}
