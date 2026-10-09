import { API } from "./api-origin";

export type SessionStatus = { access_expires_at: string; refresh_expires_at: string };
export class SessionExpired extends Error {
  constructor() { super("登录已过期，请重新登录"); }
}
let renewal: Promise<SessionStatus> | null = null;
let accessExpires = 0;
const margin = 2 * 60 * 1000;

async function authRequest(path: string, method = "GET") {
  return fetch(`${API}/api/v1/auth/${path}`, { method, credentials: "include", cache: "no-store", signal: AbortSignal.timeout(10000) });
}
function remember(status: SessionStatus) {
  accessExpires = Date.parse(status.access_expires_at);
  return status;
}
async function renew(): Promise<SessionStatus> {
  // Recheck under the browser's cross-tab lock: another tab may have already
  // replaced the shared HttpOnly cookies while this tab was waiting.
  const current = await authRequest("session");
  if (current.ok) {
    const status = remember(await current.json() as SessionStatus);
    if (accessExpires > Date.now() + margin) return status;
  } else if (current.status !== 401) throw new Error("暂时无法验证登录状态，请稍后重试");
  const refreshed = await authRequest("refresh", "POST");
  if (refreshed.ok) return remember(await refreshed.json() as SessionStatus);
  if (refreshed.status === 409) {
    // Browser fallback without Web Locks: do not replay a consumed credential.
    // Give the successful response time to install its shared cookies instead.
    for (let attempt = 0; attempt < 5; attempt++) {
      await new Promise(resolve => setTimeout(resolve, 200));
      const check = await authRequest("session");
      if (check.ok) {
        const status = await check.json() as SessionStatus;
        if (Date.parse(status.access_expires_at) > Date.now() + margin) return remember(status);
      }
    }
    throw new Error("登录正在续期，请稍后重试");
  }
  if (refreshed.status === 401) { accessExpires = 0; throw new SessionExpired(); }
  throw new Error("登录续期暂时不可用，请稍后重试");
}

export function ensureSession(force = false): Promise<SessionStatus | null> {
  if (!force && accessExpires > Date.now() + margin) return Promise.resolve(null);
  if (!renewal) {
    const operation = (async () => typeof navigator !== "undefined" && navigator.locks
      ? await navigator.locks.request("lester-session-renewal", renew)
      : await renew())();
    renewal = operation.finally(() => { renewal = null; });
  }
  return renewal;
}

export function safeLoginReturn(value: string | null): string {
  if (!value || /[\\\x00-\x1f]/.test(value)) return "/app";
  try {
    const url = new URL(value, "https://lester.invalid");
    if (url.origin !== "https://lester.invalid" || !/^\/(?:app(?:\/|$)|admin(?:\/|$)|preview\/[a-f0-9-]{36}(?:\/|$))/i.test(url.pathname)) return "/app";
    return url.pathname + url.search;
  } catch { return "/app"; }
}
export function redirectToLogin() {
  if (typeof window === "undefined" || location.pathname.startsWith("/login")) return;
  const returnTo = safeLoginReturn(location.pathname + location.search);
  location.replace(`/login?returnTo=${encodeURIComponent(returnTo)}`);
}

export async function authenticatedFetch(url: string, init: RequestInit = {}): Promise<Response> {
  const options = { ...init, credentials: "include" as const };
  const response = await fetch(url, options);
  if (response.status !== 401) return response;
  const body = await response.clone().json().catch(() => null);
  // This code is emitted exclusively before the protected handler executes.
  // Never replay a domain mutation that returned an ordinary error itself.
  if (body?.code !== "access_required") return response;
  try { await ensureSession(true); }
  catch (error) { if (error instanceof SessionExpired) redirectToLogin(); throw error; }
  init.signal?.throwIfAborted();
  const retried = await fetch(url, options);
  if (retried.status === 401) {
    const failure = await retried.clone().json().catch(() => null);
    if (failure?.code === "access_required") redirectToLogin();
  }
  return retried;
}
