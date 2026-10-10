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
  failCreate = false;
  failSend = false;
  conversation: Conversation | null = null;
  messages: Message[] = [];
  files: FileEntry[] = [];
  contextEntries: ContextEntry[] = [];
  private completed = false;
  private streams = new Set<ServerResponse>();
  private server = createServer((_request, response) => {
    response.writeHead(200, { "Content-Type": "text/event-stream", "Cache-Control": "no-store", "Access-Control-Allow-Origin": "http://127.0.0.1:13020", "Access-Control-Allow-Credentials": "true" });
    response.write(": fixture connected\n\n");
    this.streams.add(response);
    response.on("close", () => this.streams.delete(response));
  });

  async install(context: BrowserContext) {
    await new Promise<void>(resolve => this.server.listen(0, "127.0.0.1", resolve));
    const streamURL = `http://127.0.0.1:${(this.server.address() as AddressInfo).port}/events`;
    await context.route("**/api/v1/**", async route => {
      if (new URL(route.request().url()).pathname === "/api/v1/events") {
        await route.continue({ url: streamURL });
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

  finish() {
    if (!this.conversation) throw new Error("A task must be explicitly created first");
    const now = new Date().toISOString();
    this.completed = true;
    this.conversation.run_status = "completed";
    this.messages.push({ id: "fixture-assistant", role: "assistant", content: "已保存 index.html 和 notes.txt，请检查成果。", created_at: now });
    this.files = ["notes.txt", "index.html"].map(path => ({ path, name: path, is_dir: false, size: 200, modified_at: now }));
    for (const [index, file] of this.files.entries()) this.emit(index + 2, "FILE_UPDATED", { path: file.path });
    this.emit(4, "RUN_COMPLETED", {});
  }

  private emit(id: number, type: string, payload: Record<string, unknown>) {
    const event = { id, type, payload, conversation_id: conversationId, run_id: "fixture-run", created_at: new Date().toISOString() };
    for (const stream of this.streams) stream.write(`id: ${id}\ndata: ${JSON.stringify(event)}\n\n`);
  }

  private async respond(route: Route) {
    const request = route.request(), url = new URL(request.url()), path = url.pathname;
    const method = request.method();
    const reply = (data: unknown, status = 200, headers: Record<string, string> = {}) => route.fulfill({ status, contentType: "application/json", body: JSON.stringify(data), headers });
    if (path === "/api/v1/auth/options") return reply({ providers: [], registration_enabled: true, email_verification_required: false, password_reset_enabled: false });
    if (path === "/api/v1/auth/login" && method === "POST") {
      const body = request.postDataJSON();
      this.mutations.push({ path, body });
      if (body.email !== user.email || body.password !== "fixture-password-123") return reply({ error: "邮箱或密码不正确" }, 401);
      return reply({}, 200, { "set-cookie": "lester_access_token=browser-test-access; HttpOnly; SameSite=Lax; Path=/" });
    }
    if (!request.headers().cookie?.includes("lester_access_token=browser-test-access")) return reply({ code: "access_required", error: "需要登录" }, 401);
    if (path === "/api/v1/auth/session") return reply({ access_expires_at: new Date(Date.now() + 7200000).toISOString(), refresh_expires_at: new Date(Date.now() + 2592000000).toISOString() });
    if (path === "/api/v1/me") return reply(user);
    if (path === "/api/v1/me/guides") return reply({ guides: ["welcome", "models", "projects", "files", "agents", "contexts", "computer", "skills", "profile", "publishing"].map(topic => ({ topic, step: 0, status: "completed" })) });
    if (/^\/api\/v1\/me\/guides\/(welcome|models|projects|files|agents|contexts|computer|skills|profile|publishing)$/.test(path) && method === "PATCH") {
      const body = request.postDataJSON(); this.mutations.push({ path, body }); return reply({ topic: path.split("/").at(-1), ...body });
    }
    if (path === "/api/v1/projects") return reply({ projects: [{ ...project, conversation_count: this.conversation ? 1 : 0 }] });
    if (path === "/api/v1/model-deployments") return reply({ deployments: this.hasModel ? [deployment] : [] });
    if (path === "/api/v1/model-connections") return reply({ connections: [] });
    if (path === "/api/v1/agents") return reply({ agents: [{ id: "fixture-agent", slug: "lester", name: "Lester", description: "模拟 Agent", instructions: "", skill_slugs: [], version: 1, builtin: true, updated_at: project.created_at }] });
    if (path === "/api/v1/conversations") {
      if (method === "GET") return reply({ conversations: this.conversation ? [this.conversation] : [] });
      if (method === "POST") {
        const body = request.postDataJSON();
        this.mutations.push({ path, body });
        if (this.failCreate) return reply({ error: "模拟会话创建失败" }, 503);
        this.conversation = { id: conversationId, workspace_id: user.workspace_id, created_by: user.user_id, project_id: project.id, agent_slug: "lester", agent_name: "Lester", model_deployment_id: deployment.id, title: String(body.title), pinned: false, run_status: "idle", created_at: new Date().toISOString(), updated_at: new Date().toISOString() };
        return reply(this.conversation, 201);
      }
    }
    const prefix = `/api/v1/conversations/${conversationId}`;
    if (path === prefix && this.conversation) return reply({ conversation: this.conversation, messages: this.messages, active_run: this.conversation.run_status === "running" ? { id: "fixture-run", status: "running" } : null });
    if (path === `${prefix}/messages` && method === "POST") {
      const body = request.postDataJSON();
      this.mutations.push({ path, body });
      if (this.failSend) return reply({ error: "模拟首条消息发送失败" }, 503);
      if (!this.conversation) throw new Error("Message sent before creating a conversation");
      this.conversation.run_status = "running"; this.conversation.run_id = "fixture-run";
      this.messages = [{ id: "fixture-user-message", role: "user", content: String(body.content), created_at: new Date().toISOString() }];
      return reply({ run_id: "fixture-run" });
    }
    if (path === `${prefix}/events/history`) return reply({ events: [] });
    if (path === `${prefix}/computer`) return reply({ conversation_id: conversationId, user_id: user.user_id, provider: "docker", status: "running" });
    if (path === `${prefix}/files`) return reply({ files: this.files });
    if (path === `${prefix}/files/content`) {
      const file = url.searchParams.get("path");
      if (!this.files.some(item => item.path === file)) return reply({ error: "模拟文件不存在" }, 404);
      return route.fulfill({ status: 200, contentType: "application/octet-stream", body: file === "index.html" ? html : "纯文本说明：请人工验收生成文件。" });
    }
    if (path === `${prefix}/deliverables`) return reply({ deliverables: this.completed ? [{ id: "fixture-deliverable", conversation_id: conversationId, run_id: "fixture-run", title: "Lester 介绍页", summary: "固定模拟任务成果，需要人工验收。", entry_path: "index.html", kind: "html", content_sha256: "a".repeat(64), created_at: new Date().toISOString(), updated_at: new Date().toISOString() }] : [] });
    if (path === "/api/v1/contexts" || path === `${prefix}/contexts`) return reply({ entries: this.contextEntries });
    if (path === "/api/v1/skills" || path === `${prefix}/skills`) return reply({ skills: [] });
    if (path === `${prefix}/artifacts`) return reply({ artifacts: [] });
    this.unexpected.push(`${method} ${path}`);
    return reply({ error: "Unexpected browser fixture request" }, 501);
  }
}
