// An empty value keeps browser requests same-origin. This is the preferred
// Compose gateway/Ingress mode; direct development may supply an API origin.
export const API = process.env.NEXT_PUBLIC_API_URL || "";

export async function api<T>(path: string, init: RequestInit = {}): Promise<T> {
  const response = await fetch(API + path, {
    ...init,
    credentials: "include",
    headers: { "Content-Type": "application/json", ...init.headers },
  });
  return parseResponse<T>(response);
}

export async function upload<T>(path: string, body: FormData): Promise<T> {
  const response = await fetch(API + path, { method: "POST", body, credentials: "include" });
  return parseResponse<T>(response);
}

export async function readConversationFile(conversationId: string, path: string, signal?: AbortSignal): Promise<string> {
  const response = await fetchConversationFile(conversationId, path, signal);
  return response.text();
}

export async function readConversationFileBytes(conversationId: string, path: string, signal?: AbortSignal): Promise<Uint8Array> {
  const response = await fetchConversationFile(conversationId, path, signal);
  return new Uint8Array(await response.arrayBuffer());
}

async function fetchConversationFile(conversationId: string, path: string, signal?: AbortSignal): Promise<Response> {
  const response = await fetch(`${API}/api/v1/conversations/${conversationId}/files/content?path=${encodeURIComponent(path)}`, {
    credentials: "include",
    signal,
  });
  if (response.status === 401 && typeof window !== "undefined" && !location.pathname.startsWith("/login")) {
    window.location.replace("/login");
    throw new Error("authentication required");
  }
  if (!response.ok) {
    const body = await response.json().catch(() => ({ error: response.statusText }));
    throw new Error(body.error || response.statusText);
  }
  return response;
}

export function conversationFilePreviewURL(conversationId: string, path: string): string {
  const encodedPath = path.split("/").map(encodeURIComponent).join("/");
  return `${API}/api/v1/conversations/${conversationId}/preview/${encodedPath}`;
}

async function parseResponse<T>(response: Response): Promise<T> {
  if (response.status === 401 && typeof window !== "undefined" && !location.pathname.startsWith("/login")) {
    window.location.replace("/login");
    throw new Error("authentication required");
  }
  if (!response.ok) {
    const body = await response.json().catch(() => ({ error: response.statusText }));
    throw new Error(body.error || response.statusText);
  }
  if (response.status === 204) return undefined as T;
  return response.json() as Promise<T>;
}

export type ConversationRunStatus = "idle" | "running" | "cancelling" | "completed" | "failed" | "cancelled";
export type Conversation = { id: string; workspace_id: string; created_by: string; agent_slug: string; model_deployment_id: string; title: string; created_at: string; updated_at: string; run_id?: string; run_status: ConversationRunStatus };
export type UserProfile = { user_id: string; workspace_id: string; email: string; display_name: string; avatar_key: AvatarKey };
export type AvatarKey = "forest" | "ocean" | "clay" | "lilac" | "amber" | "graphite";
export type Attachment = { id: string; conversation_id: string; original_name: string; stored_path: string; content_type: string; size_bytes: number; created_at: string };
export type Message = { id: string; role: string; content: string; metadata?: { attachments?: Attachment[] }; created_at: string };
export type Deployment = { id: string; connection_id: string; name: string; model_id: string; is_default: boolean };
export type Skill = { id: string; slug: string; name: string; description: string; version: string; source: string; size_bytes: number; installed_at?: string };
export type FileEntry = { name: string; path: string; is_dir: boolean; size: number; modified_at: string };
export type ComputerState = { conversation_id: string; user_id: string; provider?: string; provider_ref?: string; status: "not_created" | "creating" | "running" | "suspended" | "stopped" | "unhealthy" | "missing" | "error"; last_error?: string; last_checked_at?: string };
