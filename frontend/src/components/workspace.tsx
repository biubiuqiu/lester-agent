"use client";

import { FormEvent, useEffect, useRef, useState } from "react";
import { useRouter } from "next/navigation";
import { Check, ChevronDown, FileText, Folder, Menu, Paperclip, Plus, Send, TerminalSquare, Wrench, X } from "lucide-react";
import { Brand } from "./brand";
import { ConversationTimeline } from "./conversation-timeline";
import { FileExplorer } from "./file-explorer";
import { RunEvent } from "./tool-timeline";
import { UserMenu } from "./user-menu";
import { API, api, Attachment, ComputerState, Conversation, Deployment, Message, Skill, upload, UserProfile } from "@/lib/api";

const agents = [
  { slug: "lester", name: "Lester", initial: "L", copy: "冷静、聪明、务实" },
  { slug: "franklin", name: "Franklin", initial: "F", copy: "直接、高效、重视推进" },
  { slug: "michael", name: "Michael", initial: "M", copy: "结构化、审慎、质量优先" },
  { slug: "trevor", name: "Trevor", initial: "T", copy: "大胆、主动、敢于探索" },
];
const agentName = (slug: string) => agents.find((agent) => agent.slug === slug)?.name || "Lester";
type RunState = "idle" | "sending" | "running" | "failed";

function appendTimelineEvent(previous: RunEvent[], event: RunEvent) {
  if (event.type !== "MODEL_DELTA") return [...previous, event].slice(-1200);
  const delta = String(event.payload.delta || "");
  if (!delta) return previous;
  const last = previous.at(-1);
  if (last?.type === "MODEL_TEXT" && last.run_id === event.run_id) {
    return [...previous.slice(0, -1), { ...last, payload: { text: String(last.payload.text || "") + delta } }];
  }
  return [...previous, { ...event, type: "MODEL_TEXT", payload: { text: delta } }].slice(-1200);
}

export function Workspace({ conversationId }: { conversationId?: string }) {
  const router = useRouter();
  const [conversations, setConversations] = useState<Conversation[]>([]);
  const [deployments, setDeployments] = useState<Deployment[]>([]);
  const [user, setUser] = useState<UserProfile | null>(null);
  const [current, setCurrent] = useState<Conversation | null>(null);
  const [messages, setMessages] = useState<Message[]>([]);
  const [events, setEvents] = useState<RunEvent[]>([]);
  const [runStatus, setRunStatus] = useState<{ conversationId?: string; state: RunState }>({ state: "idle" });
  const seenEventIds = useRef(new Set<number>());
  const [dialog, setDialog] = useState(false);
  const [mobileMenu, setMobileMenu] = useState(false);
  const [loading, setLoading] = useState(true);
  const [pageError, setPageError] = useState("");
  const [streamError, setStreamError] = useState({ conversationId: "", message: "" });

  useEffect(() => {
    Promise.all([
      api<{ conversations: Conversation[] }>("/api/v1/conversations"),
      api<{ deployments: Deployment[] }>("/api/v1/model-deployments"),
      api<UserProfile>("/api/v1/me"),
    ]).then(([conversationResult, deploymentResult, userResult]) => {
      setConversations(conversationResult.conversations);
      setDeployments(deploymentResult.deployments);
      setUser(userResult);
    }).catch((reason: unknown) => {
      setPageError(reason instanceof Error ? reason.message : "工作区加载失败");
    }).finally(() => setLoading(false));
  }, []);

  useEffect(() => {
    if (!conversationId) return;
    let active = true;
    seenEventIds.current = new Set<number>();
    const refresh = () => api<{ conversation: Conversation; messages: Message[] }>(`/api/v1/conversations/${conversationId}`).then((data) => {
      if (!active) return;
      setCurrent(data.conversation);
      setMessages(data.messages);
      setPageError("");
    }).catch((reason: unknown) => {
      if (active) setPageError(reason instanceof Error ? reason.message : "对话加载失败");
    });
    void refresh();
    const stream = new EventSource(`${API}/api/v1/conversations/${conversationId}/events`, { withCredentials: true });
    stream.onmessage = (message) => {
      let incoming: RunEvent;
      try {
        incoming = JSON.parse(message.data) as RunEvent;
      } catch {
        setStreamError({ conversationId, message: "收到无法解析的运行事件，正在等待重新连接" });
        return;
      }
      const event = { ...incoming, conversation_id: incoming.conversation_id || conversationId };
      if (seenEventIds.current.has(event.id)) return;
      seenEventIds.current.add(event.id);
      setEvents((previous) => appendTimelineEvent(previous, event));
      if (event.type === "RUN_STARTED" || event.type === "MODEL_STARTED" || event.type === "TOOL_STARTED") setRunStatus({ conversationId, state: "running" });
      if (event.type === "RUN_COMPLETED") {
        setRunStatus({ conversationId, state: "idle" });
        void refresh();
      }
      if (event.type === "RUN_FAILED") {
        setRunStatus({ conversationId, state: "failed" });
        void refresh();
      }
    };
    stream.onopen = () => {
      if (active) setStreamError({ conversationId, message: "" });
    };
    stream.onerror = () => {
      if (active) setStreamError({ conversationId, message: "实时连接暂时中断，浏览器正在自动重连" });
    };
    return () => { active = false; stream.close(); };
  }, [conversationId]);

  async function chooseModel(id: string) {
    if (!conversationId) return;
    setPageError("");
    try {
      await api(`/api/v1/conversations/${conversationId}`, { method: "PATCH", body: JSON.stringify({ model_deployment_id: id }) });
      setCurrent((value) => value ? { ...value, model_deployment_id: id } : value);
    } catch (reason) {
      setPageError(reason instanceof Error ? reason.message : "模型切换失败");
    }
  }

  async function sendMessage(content: string, attachments: Attachment[]) {
    if (!conversationId) return;
    const optimisticId = `optimistic-${crypto.randomUUID()}`;
    const visibleContent = content || `已上传附件：${attachments.map((item) => item.original_name).join("、")}`;
    const optimisticMessage: Message = { id: optimisticId, role: "user", content: visibleContent, metadata: { attachments }, created_at: new Date().toISOString() };
    setMessages((previous) => [...previous, optimisticMessage]);
    setRunStatus({ conversationId, state: "sending" });
    try {
      await api(`/api/v1/conversations/${conversationId}/messages`, { method: "POST", body: JSON.stringify({ content, attachment_ids: attachments.map((item) => item.id) }) });
      setRunStatus({ conversationId, state: "running" });
      const data = await api<{ conversation: Conversation; messages: Message[] }>(`/api/v1/conversations/${conversationId}`);
      setCurrent(data.conversation);
      setMessages(data.messages);
    } catch (error) {
      setRunStatus({ conversationId, state: "failed" });
      setMessages((previous) => previous.filter((message) => message.id !== optimisticId));
      throw error;
    }
  }

  const runState = runStatus.conversationId === conversationId ? runStatus.state : "idle";
  const displayedCurrent = current?.id === conversationId ? current : null;
  const visibleError = pageError || (streamError.conversationId === conversationId ? streamError.message : "");
  const activeLabel = runState === "sending" ? "发送中" : runState === "running" ? "正在工作" : runState === "failed" ? "上次运行失败" : "在线";
  return <main className="workspace-shell">
    <aside className={`conversation-sidebar ${mobileMenu ? "mobile-open" : ""}`}>
      <div className="sidebar-top"><Brand /><button className="icon-button mobile-close" onClick={() => setMobileMenu(false)} aria-label="关闭"><X /></button><button className="new-button" onClick={() => setDialog(true)}><Plus />新对话</button></div>
      <div className="conversation-list">{loading ? <p className="muted-block">正在载入…</p> : conversations.map((item) => <button key={item.id} className={`conversation-item ${item.id === conversationId ? "active" : ""}`} onClick={() => router.push(`/app/c/${item.id}`)}><span className="mini-agent">{agentName(item.agent_slug)[0]}</span><span><strong>{item.title}</strong><small>{agentName(item.agent_slug)} · {relativeTime(item.updated_at)}</small></span></button>)}</div>
      <div className="sidebar-footer"><UserMenu user={user} /></div>
    </aside>
    <section className={`conversation-main ${displayedCurrent ? "" : "empty-conversation"}`}>
      <header className="conversation-header"><button className="icon-button mobile-menu" onClick={() => setMobileMenu(true)}><Menu /></button>{displayedCurrent ? <><div className="agent-heading"><span className="agent-avatar">{agentName(displayedCurrent.agent_slug)[0]}</span><span><strong>{agentName(displayedCurrent.agent_slug)}</strong><small className={`run-state ${runState}`}>{activeLabel}</small></span></div><label className="model-selector"><select value={displayedCurrent.model_deployment_id || ""} onChange={(event) => chooseModel(event.target.value)}>{deployments.length === 0 ? <option value="">请先配置模型</option> : null}{deployments.map((item) => <option key={item.id} value={item.id}>{item.name}</option>)}</select><ChevronDown /></label></> : <><Brand /><button className="new-button mobile-new" onClick={() => setDialog(true)}><Plus />新对话</button></>}</header>
      {visibleError ? <div className="workspace-error" role="alert">{visibleError}</div> : null}
      {displayedCurrent ? <ConversationView conversation={displayedCurrent} messages={messages} events={events.filter((event) => event.conversation_id === displayedCurrent.id)} runState={runState} onSend={sendMessage} /> : loading || conversationId ? <div className="workspace-loading">正在载入对话…</div> : <EmptyState onNew={() => setDialog(true)} />}
    </section>
    {displayedCurrent ? <ComputerPanel conversationId={displayedCurrent.id} /> : null}
    {dialog ? <NewConversation deployments={deployments} onClose={() => setDialog(false)} /> : null}
  </main>;
}

function EmptyState({ onNew }: { onNew: () => void }) {
  return <div className="empty-hero"><span className="agent-orbit">L</span><p className="eyebrow">A private directory for every mission</p><h1>把目标交给 Lester</h1><p>选择 Agent 与模型，开始一段拥有独立文件目录、终端和运行历史的对话。</p><button className="primary-button" onClick={onNew}><Plus />开始新任务</button></div>;
}

function ConversationView({ conversation, messages, events, runState, onSend }: { conversation: Conversation; messages: Message[]; events: RunEvent[]; runState: RunState; onSend: (content: string, attachments: Attachment[]) => Promise<void> }) {
  const [text, setText] = useState("");
  const [files, setFiles] = useState<File[]>([]);
  const [uploading, setUploading] = useState(false);
  const [error, setError] = useState("");
  const thread = useRef<HTMLDivElement>(null);
  const fileInput = useRef<HTMLInputElement>(null);
  useEffect(() => { thread.current?.scrollTo({ top: thread.current.scrollHeight, behavior: "smooth" }); }, [messages, events]);

  async function submit(event: FormEvent) {
    event.preventDefault();
    if ((!text.trim() && files.length === 0) || uploading || runState === "sending" || runState === "running") return;
    setError("");
    setUploading(files.length > 0);
    try {
      const attachments = await Promise.all(files.map((file) => {
        const form = new FormData();
        form.append("file", file);
        return upload<Attachment>(`/api/v1/conversations/${conversation.id}/attachments`, form);
      }));
      await onSend(text.trim(), attachments);
      setText("");
      setFiles([]);
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : "发送失败");
    } finally {
      setUploading(false);
    }
  }

  const busy = uploading || runState === "sending" || runState === "running";
  const helper = uploading ? `正在上传 ${files.length} 个附件…` : runState === "sending" ? "消息已发送，正在创建任务…" : runState === "running" ? `${agentName(conversation.agent_slug)} 正在思考和使用工具…` : "附件只保存到当前会话的 .agent/upload，不会自动解析进上下文";
  return <><div className="thread" ref={thread}><ConversationTimeline messages={messages} events={events} /></div><form className="composer" onSubmit={submit}><p className={busy ? "composer-status active" : "composer-status"}>{helper}</p><div className="compose-box">{files.length > 0 ? <div className="pending-attachments">{files.map((file, index) => <span key={`${file.name}-${file.lastModified}`}><FileText />{file.name}<button type="button" onClick={() => setFiles((current) => current.filter((_, itemIndex) => itemIndex !== index))} aria-label={`移除 ${file.name}`}><X /></button></span>)}</div> : null}<textarea rows={2} value={text} onChange={(event) => setText(event.target.value)} onKeyDown={(event) => { if (event.key === "Enter" && !event.shiftKey) { event.preventDefault(); event.currentTarget.form?.requestSubmit(); } }} placeholder={`给 ${agentName(conversation.agent_slug)} 一个目标…`} disabled={busy} /><div className="compose-actions"><input ref={fileInput} type="file" multiple hidden onChange={(event) => setFiles((current) => [...current, ...Array.from(event.target.files || [])])} /><button type="button" className="icon-button upload-button" onClick={() => fileInput.current?.click()} disabled={busy} title="添加附件"><Paperclip /></button><button className="send-button" disabled={busy || (!text.trim() && files.length === 0)}><Send /></button></div>{error ? <p className="compose-error">{error}</p> : null}</div></form></>;
}

function NewConversation({ deployments, onClose }: { deployments: Deployment[]; onClose: () => void }) {
  const router = useRouter();
  const [selected, setSelected] = useState("lester");
  const [model, setModel] = useState(deployments.find((deployment) => deployment.is_default)?.id || deployments[0]?.id || "");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  async function create() {
    setBusy(true);
    setError("");
    try {
      const item = await api<Conversation>("/api/v1/conversations", { method: "POST", body: JSON.stringify({ agent_slug: selected, model_deployment_id: model, title: "新对话" }) });
      router.push(`/app/c/${item.id}`);
      onClose();
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : "创建对话失败");
    } finally { setBusy(false); }
  }
  return <div className="dialog-backdrop" role="presentation"><section className="dialog" role="dialog" aria-modal><header><div><p className="eyebrow">New mission</p><h2>选择这段对话的 Agent</h2></div><button className="icon-button" onClick={onClose}><X /></button></header><div className="agent-grid">{agents.map((agent) => <button key={agent.slug} className={`agent-option ${selected === agent.slug ? "selected" : ""}`} onClick={() => setSelected(agent.slug)}><span>{agent.initial}</span><strong>{agent.name}</strong><small>{agent.copy}</small></button>)}</div><label className="field">模型<select value={model} onChange={(event) => setModel(event.target.value)}>{deployments.length === 0 ? <option value="">先去设置模型</option> : null}{deployments.map((item) => <option key={item.id} value={item.id}>{item.name}</option>)}</select></label>{error ? <p className="settings-error" role="alert">{error}</p> : null}<footer><button className="secondary-button" onClick={onClose}>取消</button><button className="primary-button" onClick={create} disabled={busy || !model}>{busy ? "创建中…" : "创建对话"}</button></footer></section></div>;
}

const computerStatusLabel: Record<ComputerState["status"], string> = { not_created: "未创建", creating: "创建中", running: "运行中", suspended: "已暂停", stopped: "已停止", unhealthy: "异常", missing: "待恢复", error: "连接异常" };

function ComputerPanel({ conversationId }: { conversationId: string }) {
  const [tab, setTab] = useState<"files" | "terminal" | "skills">("files");
  const [state, setState] = useState<ComputerState | null>(null);
  useEffect(() => {
    let active = true;
    const refresh = () => api<ComputerState>(`/api/v1/conversations/${conversationId}/computer`).then((value) => { if (active) setState(value); }).catch(() => { if (active) setState((previous) => previous ? { ...previous, status: "error" } : null); });
    void refresh();
    const timer = window.setInterval(refresh, 10000);
    return () => { active = false; window.clearInterval(timer); };
  }, [conversationId]);
  const status = state?.status || "not_created";
  return <aside className="computer-panel"><header><span><i className={`computer-status ${status}`} />Computer</span><small title={state?.last_error || undefined}>Docker · 用户级 · {computerStatusLabel[status]}</small></header><nav className="computer-tabs"><button className={tab === "files" ? "active" : ""} onClick={() => setTab("files")}><Folder />Files</button><button className={tab === "terminal" ? "active" : ""} onClick={() => setTab("terminal")}><TerminalSquare />Terminal</button><button className={tab === "skills" ? "active" : ""} onClick={() => setTab("skills")}><Wrench />Skills</button></nav>{tab === "files" ? <FileExplorer key={conversationId} conversationId={conversationId} /> : null}{tab === "terminal" ? <Terminal conversationId={conversationId} /> : null}{tab === "skills" ? <ConversationSkills conversationId={conversationId} /> : null}</aside>;
}

function ConversationSkills({ conversationId }: { conversationId: string }) {
  const [catalog, setCatalog] = useState<Skill[]>([]);
  const [installed, setInstalled] = useState<Skill[]>([]);
  const [busy, setBusy] = useState("");
  const [error, setError] = useState("");
  const refresh = async () => {
    const [catalogResult, installedResult] = await Promise.all([
      api<{ skills: Skill[] }>("/api/v1/skills"),
      api<{ skills: Skill[] }>(`/api/v1/conversations/${conversationId}/skills`),
    ]);
    setCatalog(catalogResult.skills);
    setInstalled(installedResult.skills);
  };
  useEffect(() => { let active = true; Promise.all([api<{ skills: Skill[] }>("/api/v1/skills"), api<{ skills: Skill[] }>(`/api/v1/conversations/${conversationId}/skills`)]).then(([catalogResult, installedResult]) => { if (active) { setCatalog(catalogResult.skills); setInstalled(installedResult.skills); } }).catch((reason: Error) => { if (active) setError(reason.message); }); return () => { active = false; }; }, [conversationId]);
  const installedSlugs = new Set(installed.map((skill) => skill.slug));
  async function toggle(skill: Skill) {
    setBusy(skill.slug); setError("");
    try {
      if (installedSlugs.has(skill.slug)) await api(`/api/v1/conversations/${conversationId}/skills/${skill.slug}`, { method: "DELETE" });
      else await api(`/api/v1/conversations/${conversationId}/skills/${skill.slug}/install`, { method: "POST" });
      await refresh();
    } catch (reason) { setError(reason instanceof Error ? reason.message : "操作失败"); }
    finally { setBusy(""); }
  }
  return <div className="conversation-skills"><div className="skills-intro"><strong>会话级 Skills</strong><span>安装到 .agent/skills</span></div>{error ? <p className="skills-error">{error}</p> : null}{catalog.map((skill) => { const active = installedSlugs.has(skill.slug); return <article className="conversation-skill" key={skill.id}><span className="skill-mini-icon">{active ? <Check /> : <Wrench />}</span><div><strong>{skill.name}</strong><p>{skill.description}</p><small>v{skill.version}</small></div><button className={active ? "installed" : ""} disabled={busy === skill.slug} onClick={() => toggle(skill)}>{busy === skill.slug ? "处理中" : active ? "卸载" : "安装"}</button></article>; })}</div>;
}

function Terminal({ conversationId }: { conversationId: string }) {
  const mount = useRef<HTMLDivElement>(null);
  useEffect(() => {
    let socket: WebSocket | undefined;
    let terminal: { dispose: () => void; write: (value: string) => void; onData: (callback: (value: string) => void) => void } | undefined;
    void (async () => {
      const [{ Terminal }, { FitAddon }] = await Promise.all([import("@xterm/xterm"), import("@xterm/addon-fit")]);
      if (!mount.current) return;
      const instance = new Terminal({ cursorBlink: true, fontSize: 12, theme: { background: "#151a16", foreground: "#dce6dd" } });
      const fit = new FitAddon();
      instance.loadAddon(fit); instance.open(mount.current); fit.fit(); instance.write("Lester Computer\r\nConnecting…\r\n");
      const endpoint = new URL(`${API}/api/v1/conversations/${conversationId}/terminal`, window.location.origin);
      endpoint.protocol = endpoint.protocol === "https:" ? "wss:" : "ws:";
      socket = new WebSocket(endpoint);
      socket.onmessage = (event) => { const data = JSON.parse(event.data); if (data.Type === "output" || data.type === "output") instance.write(data.Data || data.data); };
      socket.onopen = () => instance.onData((data) => socket?.send(JSON.stringify({ Type: "input", Data: data })));
      socket.onerror = () => instance.write("\r\nTerminal unavailable until the Computer starts.");
      terminal = instance;
    })();
    return () => { socket?.close(); terminal?.dispose(); };
  }, [conversationId]);
  return <div className="terminal" ref={mount} />;
}

function relativeTime(value: string) { const minutes = Math.max(0, Math.round((Date.now() - new Date(value).getTime()) / 60000)); return minutes < 1 ? "刚刚" : minutes < 60 ? `${minutes} 分钟前` : minutes < 1440 ? `${Math.floor(minutes / 60)} 小时前` : `${Math.floor(minutes / 1440)} 天前`; }
