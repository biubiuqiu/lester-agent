export const API = process.env.NEXT_PUBLIC_API_URL || "http://localhost:18080";

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

async function parseResponse<T>(response: Response): Promise<T> {
  if (response.status === 401 && typeof window !== "undefined" && !location.pathname.startsWith("/login")) {
    location.href = "/login";
    throw new Error("authentication required");
  }
  if (!response.ok) {
    const body = await response.json().catch(() => ({ error: response.statusText }));
    throw new Error(body.error || response.statusText);
  }
  if (response.status === 204) return undefined as T;
  return response.json() as Promise<T>;
}

export type Conversation = { id: string; workspace_id: string; created_by: string; agent_slug: string; model_deployment_id: string; title: string; created_at: string; updated_at: string };
export type Attachment = { id: string; conversation_id: string; original_name: string; stored_path: string; content_type: string; size_bytes: number; created_at: string };
export type Message = { id: string; role: string; content: string; metadata?: { attachments?: Attachment[] }; created_at: string };
export type Deployment = { id: string; connection_id: string; name: string; model_id: string; is_default: boolean };
export type Skill = { id: string; slug: string; name: string; description: string; version: string; source: string; size_bytes: number; installed_at?: string };
export type FileEntry = { name: string; path: string; is_dir: boolean; size: number; modified_at: string };
export type ComputerState = { conversation_id: string; user_id: string; provider?: string; provider_ref?: string; status: "not_created" | "creating" | "running" | "suspended" | "stopped" | "unhealthy" | "missing" | "error"; last_error?: string; last_checked_at?: string };
