import type { Conversation, Project, UserProfile } from "./api";

declare global {
  interface Window {
    __LESTER_NATIVE__?: { version: number; platform?: "ios" | "android" };
    LesterAndroid?: { postMessage: (message: string) => void };
    webkit?: { messageHandlers?: { lester?: { postMessage: (message: unknown) => void } } };
  }
}

export function nativePlatform(): "ios" | "android" | null {
  if (typeof window === "undefined" || window.top !== window || window.__LESTER_NATIVE__?.version !== 1) return null;
  if (window.__LESTER_NATIVE__.platform === "android") return typeof window.LesterAndroid?.postMessage === "function" ? "android" : null;
  return typeof window.webkit?.messageHandlers?.lester?.postMessage === "function" ? "ios" : null;
}
export const isNativeShell = () => nativePlatform() !== null;
export const nativeShellSnapshot = () => isNativeShell();
export const nativeShellServerSnapshot = () => false;
export const subscribeNativeShell = () => () => {};

export function postNative(type: string, payload: unknown) {
  const platform = nativePlatform();
  if (!platform) return;
  const message = { version: 1, type, payload };
  try {
    if (platform === "android") window.LesterAndroid!.postMessage(JSON.stringify(message));
    else window.webkit!.messageHandlers!.lester!.postMessage(message);
  } catch { /* Native capabilities never block a task or draft. */ }
}

export function nativeWorkspaceSnapshot(user: UserProfile, projects: Project[], conversations: Conversation[], currentId: string | undefined, locale: string) {
  const names = new Map(projects.map(project => [project.id, project.name]));
  return {
    user_id: user.user_id, workspace_id: user.workspace_id, display_name: user.display_name,
    current_id: currentId ?? null, locale,
    projects: projects.slice(0, 100).map(project => ({ id: project.id, name: project.name })),
    conversations: conversations.toSorted((a, b) => Date.parse(b.updated_at) - Date.parse(a.updated_at) || b.id.localeCompare(a.id)).slice(0, 200).map(item => ({
      id: item.id, title: item.title, project_id: item.project_id, project_name: names.get(item.project_id) ?? "",
      updated_at: item.updated_at, run_id: item.run_id ?? null, run_status: item.run_status,
    })),
  };
}

// Only allow native navigation to existing surfaces. Native commands cannot
// send, approve, publish, stop or execute anything. The Web API still authorizes.
export function nativeCommandPath(value: unknown): string | null {
  if (!value || typeof value !== "object") return null;
  const input = value as Record<string, unknown>;
  if (input.action === "home") return "/app";
  if (input.action === "settings") return "/app/settings/profile";
  if (input.action === "tasks") return "/app/settings/automations";
  const uuid = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i;
  if (input.action === "conversation" && typeof input.id === "string" && uuid.test(input.id)) return `/app/c/${input.id}`;
  if (input.action === "project" && typeof input.id === "string" && uuid.test(input.id)) return `/app/p/${input.id}`;
  return null;
}

// Feed only live SSE callbacks, never the transcript/history renderer. Arm from
// authoritative running snapshots. Replays cannot
// resurrect a completed run, and a new document starts with no haptic backlog.
export class NativeRunFeedback {
  private foregroundSince = Date.now();
  private account = "";
  private active = new Set<string>();
  private finished = new Set<string>();
  foreground() { this.foregroundSince = Date.now(); this.active.clear(); }
  reset(account: string, conversations: Conversation[]) {
    if (this.account !== account) { this.account = account; this.active.clear(); this.finished.clear(); }
    for (const item of conversations) if (item.run_id) {
      if (item.run_status === "running" || item.run_status === "cancelling") { if (!this.finished.has(item.run_id)) this.active.add(item.run_id); }
      else { this.active.delete(item.run_id); this.finished.add(item.run_id); }
    }
    this.finished = new Set([...this.finished].slice(-256));
  }
  complete(run: string, conversation: string, event: number, createdAt?: string) {
    if (!this.active.delete(run) || this.finished.has(run)) return null;
    this.finished.add(run); this.finished = new Set([...this.finished].slice(-256));
    if (createdAt !== undefined && (!Number.isFinite(Date.parse(createdAt)) || Date.parse(createdAt) < this.foregroundSince)) return null;
    return { account: this.account, run_id: run, conversation_id: conversation, event_id: event };
  }
}
