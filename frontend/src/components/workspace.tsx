"use client";
import { T, useT } from "@/components/i18n";


import { ensureSession, redirectToLogin, SessionExpired } from "@/lib/auth-client";
import { GuideLauncher } from "./user-guide";

import { ContextInput, useContextReferences } from "./context-input";
import { CSSProperties, FormEvent, useEffect, useRef, useState, useSyncExternalStore } from "react";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { Bot, Check, ChevronDown, FileText, Folder, GripVertical, Maximize2, Menu, Minimize2, MoreHorizontal, PanelLeftClose, PanelLeftOpen, Paperclip, Plus, Send, Square, TerminalSquare, Wrench, X } from "lucide-react";
import { readView, updateView, viewKey, resumeViewState } from "@/lib/conversation-view-state";
import { useConversationScroll } from "./use-conversation-scroll";
import { ArtifactIcon } from "./workspace-icons";
import { ProjectRail } from "./project-rail";
import type { Project } from "@/lib/api";
import { Brand } from "./brand";
import { NewConversationComposer } from "./new-conversation-composer";
import { ArtifactCards, FileReferenceChip, FileWorkspaceProvider, OpenFilesButton, useFileWorkspace } from "./file-workspace";
import { Deliverables } from "./deliverables";
import { ConversationTimeline } from "./conversation-timeline";
import { FileExplorer } from "./file-explorer";
import { ComputerTerminal } from "./computer-terminal";
import { AgentConfiguration } from "./agent-configuration";
import { AgentActivityIndicator, RunFailureRecovery, RunNotice, RunNoticeToast, UnreadRunResult } from "./run-awareness";
import { RunEvent } from "./tool-timeline";
import { UserMenu } from "./user-menu";
import { API, api, Attachment, ComputerState, Conversation, ConversationRunStatus, Deployment, Message, Skill, upload, UserProfile } from "@/lib/api";
import { pastedImageFiles } from "@/lib/clipboard";

const agents = [
  { slug: "lester", name: "Lester", initial: "L", copy: "冷静、聪明、务实" },
  { slug: "franklin", name: "Franklin", initial: "F", copy: "直接、高效、重视推进" },
  { slug: "michael", name: "Michael", initial: "M", copy: "结构化、审慎、质量优先" },
  { slug: "trevor", name: "Trevor", initial: "T", copy: "大胆、主动、敢于探索" },
];
const agentName = (slug: string, savedName?: string, t: (source: string) => string = source => source) => slug === "agent-designer" ? t("智能体设计师") : savedName || agents.find((agent) => agent.slug === slug)?.name || "Agent";
type RunState = "idle" | "sending" | "running" | "stopping" | "cancelled" | "failed";
type RunStatus = { conversationId?: string; runId?: string; state: RunState };
type ConversationData = {
  conversation: Conversation;
  messages: Message[];
  active_run?: { id: string; status: "running" | "cancelling" } | null;
};
const sidebarPreferenceKey = "lester.workspace.sidebar-collapsed.v1";
const panelWidthPreferenceKey = "lester.workspace.computer-panel-width.v1";
const eventCursorKey = (workspaceId: string) => `lester.workspace.event-cursor.v1.${workspaceId}`;
const unreadRunResultsKey = (workspaceId: string) => `lester.workspace.unread-run-results.v2.${workspaceId}`;
const runNoticeReplayToleranceMs = 30_000;
const fileScanEvents = new Set(["FILE_UPDATED", "DELIVERABLE_REGISTERED", "TOOL_COMPLETED", "TOOL_FAILED", "RUN_COMPLETED", "RUN_FAILED", "RUN_CANCELLED"]);
const defaultPanelWidth = 420;
const minPanelWidth = 320;
const maxPanelWidth = 1600;
const conversationSidebarWidth = 252;
const compactConversationWidth = 420;
const fullConversationWidth = 780;
const wideLayoutBreakpoint = 1440;
const subscribeToHydration = () => () => {};
const getClientSnapshot = () => true;
const getServerSnapshot = () => false;

function clampPanelWidth(width: number, sidebarCollapsed: boolean) {
  if (typeof window === "undefined") return Math.min(maxPanelWidth, Math.max(minPanelWidth, width));
  const conversationWidth = window.innerWidth >= wideLayoutBreakpoint ? fullConversationWidth : compactConversationWidth;
  const available = window.innerWidth - (sidebarCollapsed ? 0 : conversationSidebarWidth) - conversationWidth;
  const upperBound = Math.max(minPanelWidth, Math.min(maxPanelWidth, available));
  return Math.min(upperBound, Math.max(minPanelWidth, width));
}

function currentPanelMaxWidth(sidebarCollapsed: boolean) {
  return clampPanelWidth(maxPanelWidth, sidebarCollapsed);
}

function mergeRunEvents(previous: RunEvent[], incoming: RunEvent[]) {
  if (incoming.length === 0) return previous;
  const byId = new Map(previous.map((event) => [event.id, event]));
  for (const event of incoming) byId.set(event.id, event);
  return Array.from(byId.values()).toSorted((a, b) => a.id - b.id).slice(-1200);
}

function eventRunStatus(type: string): ConversationRunStatus | null {
  if (type === "RUN_CANCELLING") return "cancelling";
  if (type === "RUN_COMPLETED") return "completed";
  if (type === "RUN_CANCELLED") return "cancelled";
  if (type === "RUN_FAILED") return "failed";
  if (type === "RUN_STARTED" || type === "MODEL_STARTED" || type === "TOOL_STARTED") return "running";
  return null;
}

const terminalRunStatuses = new Set<ConversationRunStatus>(["completed", "failed", "cancelled"]);

function readUnreadRunResults(workspaceId: string) {
  try {
    const value = JSON.parse(window.localStorage.getItem(unreadRunResultsKey(workspaceId)) ?? "{}") as Record<string, UnreadRunResult>;
    return Object.fromEntries(Object.entries(value).filter(([, item]) => item && typeof item.runId === "string" && (item.kind === "completed" || item.kind === "failed")).slice(-100));
  } catch {
    return {};
  }
}

function persistUnreadRunResults(workspaceId: string, value: Record<string, UnreadRunResult>) {
  const bounded = Object.fromEntries(Object.entries(value).slice(-100));
  window.localStorage.setItem(unreadRunResultsKey(workspaceId), JSON.stringify(bounded));
}

export function Workspace({ conversationId, projectId, initialAgentSlug }: { conversationId?: string; projectId?:string; initialAgentSlug?:string }) {
  const t = useT();

  const router = useRouter();
  const [projects,setProjects] = useState<Project[]>([]);
  const [conversations, setConversations] = useState<Conversation[]>([]);
  const [deployments, setDeployments] = useState<Deployment[]>([]);
  const [user, setUser] = useState<UserProfile | null>(null);
  const [current, setCurrent] = useState<Conversation | null>(null);
  const [messages, setMessages] = useState<Message[]>([]);
  const [eventsByConversation, setEventsByConversation] = useState<Record<string, RunEvent[]>>({});
  const [liveFiles, setLiveFiles] = useState<{ conversationId?: string; events: RunEvent[] }>({ conversationId, events: [] });
  if (liveFiles.conversationId !== conversationId) setLiveFiles({ conversationId, events: [] });
  const [runStatus, setRunStatus] = useState<RunStatus>({ state: "idle" });
  const [unreadRunResults, setUnreadRunResults] = useState<Record<string, UnreadRunResult>>({});
  const [runNotice, setRunNotice] = useState<RunNotice | null>(null);
  const activeConversationIdRef = useRef(conversationId);
  const conversationsRef = useRef(conversations);
  const refreshConversationRef = useRef<((syncRunState?: boolean) => Promise<void>) | null>(null);
  const [mobileMenu, setMobileMenu] = useState(false);
  const [sidebarCollapsed, setSidebarCollapsed] = useState(() => typeof window !== "undefined" && window.localStorage.getItem(sidebarPreferenceKey) === "true");
  const [panelWidth, setPanelWidth] = useState(() => {
    if (typeof window === "undefined") return defaultPanelWidth;
    const storedWidth = Number(window.localStorage.getItem(panelWidthPreferenceKey));
    const collapsed = window.localStorage.getItem(sidebarPreferenceKey) === "true";
    return Number.isFinite(storedWidth) && storedWidth > 0 ? clampPanelWidth(storedWidth, collapsed) : defaultPanelWidth;
  });
  const [panelResize, setPanelResize] = useState<{ startX: number; startWidth: number } | null>(null);
  const [loading, setLoading] = useState(true);
  const [pageError, setPageError] = useState("");
  const [streamAttempt, setStreamAttempt] = useState(0);
  const [streamError, setStreamError] = useState({ conversationId: "", message: "" });
  const panelWidthRef = useRef(panelWidth);
  const layoutHydrated = useSyncExternalStore(subscribeToHydration, getClientSnapshot, getServerSnapshot);

  useEffect(() => {
    panelWidthRef.current = panelWidth;
  }, [panelWidth]);

  useEffect(() => {
    activeConversationIdRef.current = conversationId;
  }, [conversationId]);

  useEffect(() => {
    conversationsRef.current = conversations;
  }, [conversations]);

  useEffect(() => {
    if (!runNotice) return;
    const timer = window.setTimeout(() => setRunNotice(null), 7000);
    return () => window.clearTimeout(timer);
  }, [runNotice]);

  useEffect(() => {
    if (!panelResize) return;
    const resize = (event: PointerEvent) => {
      const nextWidth = clampPanelWidth(panelResize.startWidth + panelResize.startX - event.clientX, sidebarCollapsed);
      panelWidthRef.current = nextWidth;
      setPanelWidth(nextWidth);
    };
    const finish = (event: PointerEvent) => {
      const nextWidth = clampPanelWidth(panelResize.startWidth + panelResize.startX - event.clientX, sidebarCollapsed);
      panelWidthRef.current = nextWidth;
      setPanelWidth(nextWidth);
      window.localStorage.setItem(panelWidthPreferenceKey, String(Math.round(nextWidth)));
      setPanelResize(null);
    };
    const previousCursor = document.body.style.cursor;
    const previousUserSelect = document.body.style.userSelect;
    document.body.style.cursor = "col-resize";
    document.body.style.userSelect = "none";
    window.addEventListener("pointermove", resize);
    window.addEventListener("pointerup", finish, { once: true });
    return () => {
      document.body.style.cursor = previousCursor;
      document.body.style.userSelect = previousUserSelect;
      window.removeEventListener("pointermove", resize);
      window.removeEventListener("pointerup", finish);
    };
  }, [panelResize, sidebarCollapsed]);

  function setConversationSidebar(collapsed: boolean) {
    setSidebarCollapsed(collapsed);
    window.localStorage.setItem(sidebarPreferenceKey, String(collapsed));
    setPanelWidth((width) => clampPanelWidth(width, collapsed));
  }

  function updatePanelWidth(width: number) {
    const nextWidth = clampPanelWidth(width, sidebarCollapsed);
    setPanelWidth(nextWidth);
    window.localStorage.setItem(panelWidthPreferenceKey, String(Math.round(nextWidth)));
  }

  useEffect(() => {
    let active = true;
    Promise.all([
      api<{ conversations: Conversation[] }>("/api/v1/conversations"),
      api<{ deployments: Deployment[] }>("/api/v1/model-deployments"),
      api<UserProfile>("/api/v1/me"),
      api<{projects:Project[]}>("/api/v1/projects"),
    ]).then(([conversationResult, deploymentResult, userResult,projectResult]) => {
      if (!active) return;
      setProjects(projectResult.projects);
      setConversations(conversationResult.conversations);
      setDeployments(deploymentResult.deployments);
      resumeViewState(`${userResult.user_id}.${userResult.workspace_id}.`);
      setUser(userResult);
      const unread = readUnreadRunResults(userResult.workspace_id);
      const activeConversationId = activeConversationIdRef.current;
      if (activeConversationId && unread[activeConversationId]) {
        delete unread[activeConversationId];
        persistUnreadRunResults(userResult.workspace_id, unread);
      }
      setUnreadRunResults(unread);
    }).catch((reason: unknown) => {
      setPageError(reason instanceof Error ? reason.message : "工作区加载失败");
    }).finally(() => { if (active) setLoading(false); });
    return () => { active = false; };
  }, []);

  useEffect(() => {
    if (!conversationId) return;
    let active = true;
    const refresh = async (syncRunState = false) => {
      try {
        const [data, history] = await Promise.all([
          api<ConversationData>(`/api/v1/conversations/${conversationId}`),
          api<{ events: RunEvent[] }>(`/api/v1/conversations/${conversationId}/events/history`),
        ]);
        if (!active) return;
        setCurrent(data.conversation);
        setMessages(data.messages);
        setEventsByConversation((previous) => ({
          ...previous,
          [conversationId]: mergeRunEvents(previous[conversationId] ?? [], history.events),
        }));
        setConversations((previous) => previous.map((item) => item.id === conversationId ? data.conversation : item));
        if (syncRunState) {
          const inactiveState: RunState = data.conversation.run_status === "failed" ? "failed" : data.conversation.run_status === "cancelled" ? "cancelled" : "idle";
          setRunStatus(data.active_run
            ? { conversationId, runId: data.active_run.id, state: data.active_run.status === "cancelling" ? "stopping" : "running" }
            : { conversationId, runId: data.conversation.run_id, state: inactiveState });
        }
        setPageError("");
      } catch (reason: unknown) {
        if (active) setPageError(reason instanceof Error ? reason.message : "对话加载失败");
      }
    };
    refreshConversationRef.current = refresh;
    void refresh(true);
    return () => {
      active = false;
      if (refreshConversationRef.current === refresh) refreshConversationRef.current = null;
    };
  }, [conversationId]);

  useEffect(() => {
    const workspaceId = user?.workspace_id;
    if (!workspaceId) return;
    let active = true;
    let reconnect: ReturnType<typeof setTimeout> | undefined;
    const endpoint = new URL(`${API}/api/v1/events`, window.location.origin);
    const streamStartedAt = Date.now();
    const storedCursor = Number(window.sessionStorage.getItem(eventCursorKey(workspaceId)) ?? 0);
    if (Number.isSafeInteger(storedCursor) && storedCursor > 0) endpoint.searchParams.set("last_event_id", String(storedCursor));
    const stream = new EventSource(endpoint.toString(), { withCredentials: true });
    stream.onmessage = (message) => {
      let incoming: RunEvent;
      try {
        incoming = JSON.parse(message.data) as RunEvent;
      } catch {
        setStreamError({ conversationId: "*", message: "收到无法解析的运行事件，正在等待重新连接" });
        return;
      }
      const conversation = incoming.conversation_id;
      if (!conversation) return;
      const event = { ...incoming, conversation_id: conversation };
      const currentCursor = Number(window.sessionStorage.getItem(eventCursorKey(workspaceId)) ?? 0);
      // History and old outbox deliveries still update the timeline, but only
      // fresh, unseen notifications may request a new file tab.
      if (activeConversationIdRef.current === conversation && event.id > currentCursor
        && Date.parse(event.created_at) >= streamStartedAt - runNoticeReplayToleranceMs
        && fileScanEvents.has(event.type)) {
        setLiveFiles((previous) => previous.conversationId === conversation ? { ...previous, events: [...previous.events, event].slice(-64) } : previous);
      }
      if (activeConversationIdRef.current === conversation) {
        setEventsByConversation((previous) => ({
          ...previous,
          [conversation]: mergeRunEvents(previous[conversation] ?? [], [event]),
        }));
      }
      const nextStatus = eventRunStatus(event.type);
      if (nextStatus) {
        setConversations((previous) => previous.map((item) => item.id === conversation
          ? { ...item, run_id: event.run_id, run_status: nextStatus, updated_at: terminalRunStatuses.has(nextStatus) ? event.created_at : item.updated_at }
          : item));
      }
      if (!Number.isSafeInteger(currentCursor) || event.id > currentCursor) {
        window.sessionStorage.setItem(eventCursorKey(workspaceId), String(event.id));
      }
      if (activeConversationIdRef.current !== conversation) {
        const eventCreatedAt = Date.parse(event.created_at);
        const happenedDuringThisSession = Number.isFinite(eventCreatedAt) && eventCreatedAt >= streamStartedAt - runNoticeReplayToleranceMs;
        if (happenedDuringThisSession && (nextStatus === "completed" || nextStatus === "failed")) {
          const kind = nextStatus;
          const unread = { runId: event.run_id, kind } satisfies UnreadRunResult;
          setUnreadRunResults((previous) => {
            const next = { ...previous, [conversation]: unread };
            persistUnreadRunResults(workspaceId, next);
            return next;
          });
          const title = conversationsRef.current.find((item) => item.id === conversation)?.title || "后台任务";
          setRunNotice({ conversationId: conversation, title, ...unread });
        }
        return;
      }
      if (nextStatus === "running") {
        setRunStatus((previous) => previous.conversationId === conversation && previous.runId === event.run_id && previous.state === "stopping"
          ? previous
          : { conversationId: conversation, runId: event.run_id, state: "running" });
      }
      if (nextStatus === "completed") setRunStatus({ conversationId: conversation, runId: event.run_id, state: "idle" });
      if (nextStatus === "cancelling") setRunStatus({ conversationId: conversation, runId: event.run_id, state: "stopping" });
      if (nextStatus === "cancelled") setRunStatus({ conversationId: conversation, runId: event.run_id, state: "cancelled" });
      if (nextStatus === "failed") setRunStatus({ conversationId: conversation, runId: event.run_id, state: "failed" });
      if (nextStatus === "completed" || nextStatus === "cancelled" || nextStatus === "failed") {
        void refreshConversationRef.current?.();
      }
    };
    stream.onopen = () => {
      setStreamError({ conversationId: "*", message: "" });
    };
    stream.onerror = () => {
      void ensureSession(true).catch(error => { if (active && error instanceof SessionExpired) redirectToLogin(); }).finally(() => {
        // A 401 permanently closes native EventSource. After renewal recreate
        // it with the persisted cursor; ordinary disconnects keep native retry.
        if (active && stream.readyState === EventSource.CLOSED && !reconnect) reconnect = setTimeout(() => { if (active) setStreamAttempt(value => value + 1); }, 1500);
      });
      setStreamError({ conversationId: "*", message: "实时连接暂时中断，浏览器正在自动重连" });
    };
    return () => { active = false; clearTimeout(reconnect); stream.close(); };
  }, [user?.workspace_id, streamAttempt]);

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

  async function sendMessage(content: string, attachments: Attachment[], contextIds: string[]) {
    if (!conversationId) return;
    const optimisticId = `optimistic-${crypto.randomUUID()}`;
    const visibleContent = content || t("已上传附件：{0}", [attachments.map((item) => item.original_name).join("、")]);
    const optimisticMessage: Message = { id: optimisticId, role: "user", content: visibleContent, metadata: { attachments }, created_at: new Date().toISOString() };
    setMessages((previous) => [...previous, optimisticMessage]);
    setRunStatus({ conversationId, state: "sending" });
    setConversations((previous) => previous.map((item) => item.id === conversationId ? { ...item, run_status: "running" } : item));
    try {
      const started = await api<{ run_id: string }>(`/api/v1/conversations/${conversationId}/messages`, { method: "POST", body: JSON.stringify({ content, context_ids: contextIds, attachment_ids: attachments.map((item) => item.id) }) });
      setRunStatus({ conversationId, runId: started.run_id, state: "running" });
      setConversations((previous) => previous.map((item) => item.id === conversationId ? { ...item, run_id: started.run_id, run_status: "running" } : item));
      const data = await api<ConversationData>(`/api/v1/conversations/${conversationId}`);
      setCurrent(data.conversation);
      setMessages(data.messages);
      setConversations((previous) => previous.map((item) => item.id === conversationId ? data.conversation : item));
    } catch (error) {
      setRunStatus({ conversationId, state: "failed" });
      setConversations((previous) => previous.map((item) => item.id === conversationId ? { ...item, run_status: "failed" } : item));
      setMessages((previous) => previous.filter((message) => message.id !== optimisticId));
      throw error;
    }
  }

  async function stopRun() {
    if (!conversationId || !runStatus.runId || (runStatus.state !== "running" && runStatus.state !== "stopping")) return;
    const runId = runStatus.runId;
    setPageError("");
    setRunStatus({ conversationId, runId, state: "stopping" });
    setConversations((previous) => previous.map((item) => item.id === conversationId ? { ...item, run_id: runId, run_status: "cancelling" } : item));
    try {
      await api(`/api/v1/conversations/${conversationId}/runs/${runId}/cancel`, { method: "POST" });
      const data = await api<ConversationData>(`/api/v1/conversations/${conversationId}`);
      setCurrent(data.conversation);
      setMessages(data.messages);
      setRunStatus(data.active_run
        ? { conversationId, runId: data.active_run.id, state: data.active_run.status === "cancelling" ? "stopping" : "running" }
        : { conversationId, runId, state: "cancelled" });
      setConversations((previous) => previous.map((item) => item.id === conversationId ? data.conversation : item));
    } catch (reason) {
      setRunStatus({ conversationId, runId, state: "running" });
      setConversations((previous) => previous.map((item) => item.id === conversationId ? { ...item, run_id: runId, run_status: "running" } : item));
      setPageError(reason instanceof Error ? reason.message : "停止任务失败");
    }
  }

  function openConversation(id: string) {
    const workspaceId = user?.workspace_id;
    if (workspaceId) {
      setUnreadRunResults((previous) => {
        if (!previous[id]) return previous;
        const next = { ...previous };
        delete next[id];
        persistUnreadRunResults(workspaceId, next);
        return next;
      });
    }
    setRunNotice((previous) => previous?.conversationId === id ? null : previous);
    setMobileMenu(false);
    router.push(`/app/c/${id}`);
  }

  const [conversationSearch, setConversationSearch] = useState("");
  const visibleConversations = conversations.filter((item) => item.title.toLocaleLowerCase().includes(conversationSearch.trim().toLocaleLowerCase()));
  const runState = runStatus.conversationId === conversationId ? runStatus.state : "idle";
  const displayedCurrent = current?.id === conversationId ? current : null;
  const selectedProject = displayedCurrent?.project_id || projectId || projects.find(p=>p.is_default)?.id;
  const newConversationPath = selectedProject ? `/app/p/${selectedProject}` : "/app";
  function updateOrganizedConversation(id: string, patch: Partial<Conversation>) {
    const currentConversation = conversations.find(item => item.id === id);
    setConversations(prev => prev.map(item => item.id === id ? {...item, ...patch} : item));
    if (patch.project_id && currentConversation && patch.project_id !== currentConversation.project_id) {
      setProjects(prev => prev.map(project => project.id === currentConversation.project_id
        ? {...project, conversation_count: Math.max(0, project.conversation_count - 1)}
        : project.id === patch.project_id
          ? {...project, conversation_count: project.conversation_count + 1}
          : project));
    }
    setCurrent(prev => prev?.id === id ? {...prev, ...patch} : prev);
  }
  const visibleError = pageError || (streamError.conversationId === "*" || streamError.conversationId === conversationId ? streamError.message : "");
  const activeLabel = runState === "sending" ? t("发送中") : runState === "running" ? t("正在工作") : runState === "stopping" ? t("正在停止") : runState === "cancelled" ? t("已停止") : runState === "failed" ? t("上次运行失败") : t("在线");
  const renderedSidebarCollapsed = layoutHydrated && sidebarCollapsed;
  const renderedPanelWidth = layoutHydrated ? panelWidth : defaultPanelWidth;
  const renderedPanelMaxWidth = layoutHydrated ? currentPanelMaxWidth(renderedSidebarCollapsed) : maxPanelWidth;
  const shellStyle = { "--computer-panel-width": `${renderedPanelWidth}px` } as CSSProperties;
  return <FileWorkspaceProvider key={`${user?.user_id ?? "pending"}:${displayedCurrent?.id ?? "empty"}`} conversationId={displayedCurrent?.id} storageKey={user && displayedCurrent ? viewKey(user.user_id, user.workspace_id, displayedCurrent.id) : ""} liveFileEvents={liveFiles.events} events={eventsByConversation[displayedCurrent?.id ?? ""] ?? []} runId={runStatus.conversationId === conversationId ? runStatus.runId : undefined} running={runState === "running" || runState === "sending" || runState === "stopping"} runOutcome={runState === "failed" || runState === "cancelled" ? runState : undefined}><main className={`workspace-shell ${renderedSidebarCollapsed ? "sidebar-collapsed" : ""} ${panelResize ? "panel-resizing" : ""}`} style={shellStyle}>
    <aside className={`conversation-sidebar ${mobileMenu ? "mobile-open" : ""}`}>
      <div className="sidebar-top"><div className="sidebar-brand-row"><Brand /><button type="button" className="sidebar-collapse-button" onClick={() => setConversationSidebar(true)} title={t("收起会话栏")} aria-label={t("收起会话栏")}><PanelLeftClose /></button></div><button className="icon-button mobile-close" onClick={() => setMobileMenu(false)} aria-label={t("关闭")}><X /></button></div>



      <div className="conversation-list">{loading ? <p className="muted-block"><T>{"正在载入…"}</T></p> : <ProjectRail projects={projects} conversations={visibleConversations} currentId={conversationId} selectedProject={selectedProject} unread={unreadRunResults} onOpen={openConversation} onNew={id=>{setMobileMenu(false);setConversationSearch("");router.push(`/app/p/${id}`);}} search={conversationSearch} onSearch={setConversationSearch} onProjects={setProjects} onConversation={updateOrganizedConversation}/>}</div>
      <div className="sidebar-footer"><UserMenu user={user} /></div>
    </aside>
    <section className={`conversation-main ${displayedCurrent ? "" : "empty-conversation"}`}>
      <header className="conversation-header"><div className="conversation-header-leading"><button className="icon-button mobile-menu" onClick={() => setMobileMenu(true)} aria-label={t("打开会话栏")}><Menu /></button><button type="button" className="sidebar-restore-button" onClick={() => setConversationSidebar(false)} title={t("展开会话栏")} aria-label={t("展开会话栏")}><PanelLeftOpen /></button>{displayedCurrent ? <div className="agent-heading"><span className="agent-avatar">{agentName(displayedCurrent.agent_slug,displayedCurrent.agent_name,t)[0]}</span><span><strong>{agentName(displayedCurrent.agent_slug,displayedCurrent.agent_name,t)}</strong><small className={`run-state ${runState}`}>{activeLabel}</small></span></div> : <Brand />}</div>{displayedCurrent ? <label className="model-selector conversation-model-selector"><select aria-label={t("当前模型")} value={displayedCurrent.model_deployment_id || ""} onChange={(event) => chooseModel(event.target.value)}>{deployments.length === 0 ? <option value=""><T>{"请先配置模型"}</T></option> : null}{deployments.map((item) => <option key={item.id} value={item.id}>{item.name}</option>)}</select><ChevronDown /></label> : <button className="new-button mobile-new" onClick={() => { setMobileMenu(false); router.push(newConversationPath); }}><Plus /><T>{"新会话"}</T></button>}<GuideLauncher /><Link className="workspace-artifacts-link" href={`/app/artifacts?returnTo=${encodeURIComponent(conversationId ? `/app/c/${conversationId}` : newConversationPath)}`} aria-label={t("打开产物管理")} title={t("产物管理")}><ArtifactIcon size={18}/><span><T>{"产物管理"}</T></span></Link>{displayedCurrent?.created_agent_id ? <OpenAgentButton /> : null}{displayedCurrent ? <OpenResultsButton compact /> : null}{displayedCurrent ? <details className="mobile-conversation-options"><summary aria-label={t("更多会话选项")} title={t("更多会话选项")}><MoreHorizontal /></summary><div><label className="field"><T>{"当前模型"}</T><select aria-label={t("切换模型")} value={displayedCurrent.model_deployment_id || ""} onChange={event => { void chooseModel(event.target.value); const menu = event.currentTarget.closest("details"); if (menu) menu.open = false; }}>{deployments.map(item => <option key={item.id} value={item.id}>{item.name}</option>)}</select></label><OpenFilesButton menu /><Link href={`/app/artifacts?returnTo=${encodeURIComponent(`/app/c/${displayedCurrent.id}`)}`}><T>{"管理已发布的产物"}</T></Link></div></details> : null}</header>
      {visibleError ? <div className="workspace-error" role="alert">{visibleError}</div> : null}
      {displayedCurrent ? <ConversationView key={displayedCurrent.id} conversation={displayedCurrent} messages={messages} events={eventsByConversation[displayedCurrent.id] ?? []} runState={runState} runId={runStatus.conversationId === conversationId ? runStatus.runId : undefined} onSend={sendMessage} onStop={stopRun} /> : loading || conversationId ? <div className="workspace-loading"><T>{"正在载入对话…"}</T></div> : user ? <NewConversationComposer key={`${selectedProject}:${initialAgentSlug||"lester"}`} deployments={deployments} user={user} projectId={selectedProject} projectName={projects.find(p=>p.id===selectedProject)?.name} initialAgentSlug={initialAgentSlug} /> : null}
    </section>
    {displayedCurrent ? <ComputerPanel conversationId={displayedCurrent.id} createdAgentId={displayedCurrent.agent_slug === "agent-designer" ? displayedCurrent.created_agent_id : undefined} agentRevision={eventsByConversation[displayedCurrent.id]?.findLast(event => event.type === "AGENT_SAVED")?.id ?? 0} width={renderedPanelWidth} maxWidth={renderedPanelMaxWidth} resizing={Boolean(panelResize)} onResizeStart={(clientX) => setPanelResize({ startX: clientX, startWidth: renderedPanelWidth })} onWidthChange={updatePanelWidth} /> : null}
    {runNotice ? <RunNoticeToast notice={runNotice} onOpen={() => openConversation(runNotice.conversationId)} onDismiss={() => setRunNotice(null)} /> : null}
  </main></FileWorkspaceProvider>;
}

function ConversationView({ conversation, messages, events, runState, runId, onSend, onStop }: { conversation: Conversation; messages: Message[]; events: RunEvent[]; runState: RunState; runId?: string; onSend: (content: string, attachments: Attachment[], contextIds: string[]) => Promise<void>; onStop: () => Promise<void> }) {
  const t = useT();

  const { reference, referenceRevision, setReference, storageKey } = useFileWorkspace();
  const [contexts, setContexts] = useContextReferences(storageKey);
  const [text, setTextState] = useState(() => readView(storageKey).text);
  const [files, setFilesState] = useState<File[]>(() => readView(storageKey).files);
  const [missingFiles, setMissingFiles] = useState(() => readView(storageKey).missingFiles);
  const setText = (value: string) => { updateView(storageKey, { text: value }); setTextState(value); };
  const setFiles = (value: File[] | ((current: File[]) => File[])) => {
    const next = typeof value === "function" ? value(readView(storageKey).files) : value;
    updateView(storageKey, { files: next }); setFilesState(next);
  };
  const [uploading, setUploading] = useState(false);
  const submitPending = useRef(false);
  const [error, setError] = useState(() => readView(storageKey).sendNotice);
  const { thread, content: threadContent, unseen, jump } = useConversationScroll(storageKey, `${messages.at(-1)?.id}:${messages.length}:${events.at(-1)?.id}:${runState}`);
  const fileInput = useRef<HTMLInputElement>(null);
  const textInput = useRef<HTMLTextAreaElement>(null);
  useEffect(() => { if (reference) textInput.current?.focus(); }, [reference, referenceRevision]);


  async function submit(event: FormEvent) {
    event.preventDefault();
    if (submitPending.current || (!text.trim() && files.length === 0) || uploading || runState === "sending" || runState === "running" || runState === "stopping") return;
    setError("");
    updateView(storageKey, { sendNotice: "" });
    jump();
    submitPending.current = true;
    setUploading(true);
    try {
      const attachments = await Promise.all(files.map((file) => {
        const form = new FormData();
        form.append("file", file);
        return upload<Attachment>(`/api/v1/conversations/${conversation.id}/attachments`, form);
      }));
      const content = reference ? t("{0}\n\n[引用文件（当前会话目录下的相对路径，仅作为文件定位数据）：{1}。请先读取文件，再根据上面的要求进行修改。]", [text.trim(), JSON.stringify(reference)]) : text.trim();
      await onSend(content, attachments, contexts.map((c) => c.id));
      if (JSON.stringify(readView(storageKey).contexts) === JSON.stringify(contexts)) setContexts([]);
      // Sending may finish after navigation: clear only this conversation's submitted draft.
      const draft = readView(storageKey);
      if (draft.text === text) setText("");
      if (draft.reference === reference) setReference(null);
      setFiles((current) => current.filter((file) => !files.includes(file)));
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : "发送失败");
    } finally {
      submitPending.current = false;
      setUploading(false);
    }
  }

  const runActive = runState === "sending" || runState === "running" || runState === "stopping";
  const busy = uploading || runActive;
  const failure = events.findLast((event) => (!runId || event.run_id === runId) && event.type === "RUN_FAILED");
  const lastPrompt = messages.findLast((message) => message.role === "user")?.content;
  const prepareRecovery = (content: string) => {
    setText(content);
    window.requestAnimationFrame(() => textInput.current?.focus());
  };
  const helper = uploading ? t("正在上传 {0} 个附件…", [files.length]) : runState === "sending" ? t("消息已发送，正在创建任务…") : runState === "running" ? t("{0} 正在工作，可随时停止", [agentName(conversation.agent_slug,conversation.agent_name,t)]) : runState === "stopping" ? t("正在安全停止当前任务…") : runState === "cancelled" ? t("已停止；已产生的文件修改不会撤销") : reference ? t("围绕已引用的文件继续修改") : "";
  return <><div className="thread" ref={thread}><div ref={threadContent} className="thread-content">{conversation.agent_slug === "agent-designer" && messages.length === 0 ? <div className="designer-welcome"><span><Bot size={22}/></span><h2><T>{"先聊聊，你想创建怎样的智能体？"}</T></h2><p><T>{"我是智能体设计师。告诉我它要帮谁做什么，平时会收到哪些材料，以及你希望它如何交付结果。我会先了解需求，再和你一起确定配置。"}</T></p></div> : null}<ConversationTimeline messages={messages} events={events} /><Deliverables /><ArtifactCards />{runActive ? <AgentActivityIndicator agent={agentName(conversation.agent_slug,conversation.agent_name,t)} state={runState} runId={runId} events={events} /> : null}</div></div><form className="composer" onSubmit={submit}>{unseen ? <button type="button" className="new-content-button" onClick={jump}><ChevronDown /><T>{"有新内容 · 回到底部"}</T></button> : null}{runState === "failed" && failure ? <RunFailureRecovery reason={String(failure.payload.error ?? t("任务执行失败"))} lastPrompt={lastPrompt} onPrepare={prepareRecovery} /> : null}{helper ? <p className={busy ? "composer-status active" : "composer-status"}>{helper}</p> : null}<div className="compose-box"><FileReferenceChip />{missingFiles.length ? <p className="draft-attachment-notice" role="status"><T>{"草稿已恢复；刷新前的本地附件需要重新选择："}</T>{missingFiles.join("、")}<button type="button" onClick={() => { setMissingFiles([]); updateView(storageKey, { missingFiles: [] }); }}><T>{"知道了"}</T></button></p> : null}{files.length > 0 ? <div className="pending-attachments">{files.map((file, index) => <span key={`${file.name}-${file.lastModified}`}><FileText />{file.name}<button type="button" onClick={() => setFiles((current) => current.filter((_, itemIndex) => itemIndex !== index))} aria-label={t("移除 {0}", [file.name])}><X /></button></span>)}</div> : null}<ContextInput references={contexts} onReferences={setContexts} inputRef={textInput} rows={2} value={text} onText={setText} onPaste={(event) => { const images = pastedImageFiles(event); if (images.length) { event.preventDefault(); setFiles((current) => [...current, ...images]); } }} onKeyDown={(event) => { if (event.key === "Enter" && !event.shiftKey && !event.nativeEvent.isComposing) { event.preventDefault(); event.currentTarget.form?.requestSubmit(); } }} aria-label={t("消息输入框")} placeholder={reference ? t("描述要如何修改这个文件…") : conversation.agent_slug === "agent-designer" ? t("描述你想创建的智能体…") : t("给 {0} 一个目标…", [agentName(conversation.agent_slug,conversation.agent_name,t)])} disabled={uploading || runState === "sending"} /><div className="compose-actions"><input ref={fileInput} type="file" multiple hidden onChange={(event) => setFiles((current) => [...current, ...Array.from(event.target.files || [])])} /><button type="button" className="icon-button upload-button" onClick={() => fileInput.current?.click()} disabled={busy} aria-label={t("添加附件")} title={t("添加附件或直接粘贴图片；文件保存在当前会话，Agent 按需读取，不会自动解析进上下文")}><Paperclip /></button><span className="composer-keyboard-hint"><T>{"Shift + Enter 换行"}</T></span>{runActive ? <button type="button" className={`send-button stop-button ${runState === "stopping" ? "stopping" : ""}`} onClick={() => void onStop()} disabled={!runId || runState === "sending" || runState === "stopping"} title={runState === "stopping" ? t("正在停止") : t("停止生成")} aria-label={runState === "stopping" ? t("正在停止任务") : t("停止生成")}><Square /></button> : <button className="send-button" disabled={busy || (!text.trim() && files.length === 0)} aria-label={t("发送消息")}><Send /></button>}</div>{error ? <p className="compose-error">{t(error)}</p> : null}</div></form></>;
}

const computerStatusLabel: Record<ComputerState["status"], string> = { not_created: "未创建", creating: "创建中", running: "运行中", suspended: "已暂停", stopped: "已停止", unhealthy: "异常", missing: "待恢复", error: "连接异常" };

function OpenResultsButton({ compact = false }: { compact?: boolean }) {
  const t = useT();

  const { setPanelTab, setPanelOpen } = useFileWorkspace();
  return <button type="button" className={compact ? "open-deliverables-button" : "open-results-button"} onClick={(event) => { setPanelTab("results"); setPanelOpen(true); const menu = event.currentTarget.closest("details"); if (menu) menu.open = false; }}>{compact ? <><FileText size={16} /><T>{"成果"}</T></> : t("查看会话成果")}</button>;
}

function OpenAgentButton() {
  const { setPanelTab, setPanelOpen } = useFileWorkspace();
  return <button type="button" className="open-agent-button" onClick={() => { setPanelTab("agent"); setPanelOpen(true); }}><Bot size={16} /><T>{"智能体"}</T></button>;
}

function ComputerPanel({ conversationId, createdAgentId, agentRevision, width, maxWidth, resizing, onResizeStart, onWidthChange }: { conversationId: string; createdAgentId?: string; agentRevision: number; width: number; maxWidth: number; resizing: boolean; onResizeStart: (clientX: number) => void; onWidthChange: (width: number) => void }) {
  const t = useT();

  const { panelTab: tab, setPanelTab: setTab, expanded, setExpanded, panelOpen, setPanelOpen } = useFileWorkspace();
  const previousAgentId = useRef<string | undefined>(undefined);
  useEffect(() => {
    if (createdAgentId === previousAgentId.current) return;
    previousAgentId.current = createdAgentId;
    if (new URLSearchParams(window.location.search).has("preview")) return;
    if (createdAgentId) { setTab("agent"); setPanelOpen(true); }
    else { setTab("files"); setPanelOpen(false); }
  }, [createdAgentId, setTab, setPanelOpen]);
  useEffect(() => {
    const onEscape = (event: KeyboardEvent) => { if (event.key === "Escape") { setExpanded(false); setPanelOpen(false); } };
    window.addEventListener("keydown", onEscape);
    return () => window.removeEventListener("keydown", onEscape);
  }, [setExpanded, setPanelOpen]);
  const [state, setState] = useState<ComputerState | null>(null);
  useEffect(() => {
    let active = true;
    const refresh = () => api<ComputerState>(`/api/v1/conversations/${conversationId}/computer`).then((value) => { if (active) setState(value); }).catch(() => { if (active) setState((previous) => previous ? { ...previous, status: "error" } : null); });
    void refresh();
    const timer = window.setInterval(refresh, 10000);
    return () => { active = false; window.clearInterval(timer); };
  }, [conversationId]);
  const status = state?.status || "not_created";
  const providerLabel = state?.provider === "acs" ? "Alibaba Cloud ACS" : state?.provider === "docker" ? "Docker" : "Computer";
  return <aside className={`computer-panel ${expanded ? "preview-expanded" : ""} ${panelOpen ? "files-open" : ""}`}><button type="button" className={`panel-resizer ${resizing ? "active" : ""}`} onPointerDown={(event) => { event.preventDefault(); onResizeStart(event.clientX); }} onKeyDown={(event) => { if (event.key === "ArrowLeft") onWidthChange(width + 24); if (event.key === "ArrowRight") onWidthChange(width - 24); }} role="separator" aria-label={t("调整 Computer 面板宽度")} aria-orientation="vertical" aria-valuemin={minPanelWidth} aria-valuemax={Math.round(maxWidth)} aria-valuenow={Math.round(width)} title={t("拖动调整面板宽度")}><GripVertical /></button><header className="computer-panel-header">
      <nav className="computer-tabs" aria-label={t("工作区功能")}>
        <button aria-pressed={tab === "results"} className={tab === "results" ? "active" : ""} onClick={() => setTab("results")}><ArtifactIcon size={15} /><T>{"成果"}</T></button>
        <button aria-pressed={tab === "files"} className={tab === "files" ? "active" : ""} onClick={() => setTab("files")}><Folder /><T>{"文件"}</T></button>
        <button aria-pressed={tab === "terminal"} className={tab === "terminal" ? "active" : ""} onClick={() => setTab("terminal")}><TerminalSquare /><T>{"终端"}</T></button>
        <button aria-pressed={tab === "skills"} className={tab === "skills" ? "active" : ""} onClick={() => setTab("skills")}><Wrench /><T>{"技能"}</T></button>
        {createdAgentId ? <button aria-pressed={tab === "agent"} className={tab === "agent" ? "active" : ""} onClick={() => setTab("agent")}><Bot /><T>{"智能体"}</T></button> : null}
      </nav>
      <GuideLauncher label={t("功能教学")} />
      <span className="sandbox-state" title={t("{0} · 用户级工作区 · {1}{2}", [providerLabel, t(computerStatusLabel[status]), state?.last_error ? `：${state.last_error}` : ""])} aria-label={t("工作区{0}", [t(computerStatusLabel[status])])}><i className={`computer-status ${status}`} /></span>
      {tab === "agent" && createdAgentId ? <button type="button" className="designer-expand-button" onClick={() => setExpanded(!expanded)} aria-label={expanded ? t("收起智能体配置") : t("放大智能体配置")} title={expanded ? t("收起配置") : t("放大编辑提示词")}>{expanded ? <Minimize2 size={16} /> : <Maximize2 size={16} />}</button> : null}
      <button type="button" className="close-files-panel" onClick={() => { setExpanded(false); setPanelOpen(false); }} aria-label={t("关闭文件面板")}><X /></button>
    </header>{tab === "results" ? <Deliverables key={conversationId} panel /> : null}{tab === "files" ? <FileExplorer key={conversationId} conversationId={conversationId} /> : null}{tab === "terminal" ? <ComputerTerminal key={conversationId} conversationId={conversationId} /> : null}{tab === "skills" ? <ConversationSkills conversationId={conversationId} /> : null}{tab === "agent" && createdAgentId ? <AgentConfiguration agentId={createdAgentId} revision={agentRevision} /> : null}</aside>;
}

function ConversationSkills({ conversationId }: { conversationId: string }) {
  const t = useT();

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
  return <div className="conversation-skills"><div className="skills-intro"><strong><T>{"会话级 Skills"}</T></strong><span><T>{"安装到 .agent/skills"}</T></span></div>{error ? <p className="skills-error">{t(error)}</p> : null}{catalog.map((skill) => { const active = installedSlugs.has(skill.slug); return <article className="conversation-skill" key={skill.id}><span className="skill-mini-icon">{active ? <Check /> : <Wrench />}</span><div><strong>{skill.name}</strong><p>{skill.description}</p><small>v{skill.version}</small></div><button className={active ? "installed" : ""} disabled={busy === skill.slug} onClick={() => toggle(skill)}>{busy === skill.slug ? t("处理中") : active ? t("卸载") : t("安装")}</button></article>; })}</div>;
}
