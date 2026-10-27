export const appProviders = ["github", "gmail", "x", "feishu", "slack"] as const;
export type AppProvider = typeof appProviders[number];
export type AppConnection = { provider: AppProvider; configured: boolean; status: "disconnected" | "connected" | "reauthorization_required"; account_name: string; scopes: string[]; revision: number; github_code_enabled?: boolean; updated_at?: string };
const authorizeOrigins: Record<AppProvider, string> = { github: "https://github.com/login/oauth/authorize", gmail: "https://accounts.google.com/o/oauth2/v2/auth", x: "https://x.com/i/oauth2/authorize", feishu: "https://accounts.feishu.cn/open-apis/authen/v1/authorize", slack: "https://slack.com/oauth/v2/authorize" };

/** OAuth starts only at a known provider and comes back to this deployment. */
export function appAuthorizationURL(provider: AppProvider, value: string, origin: string): string | null {
  try {
    const url = new URL(value), endpoint = new URL(authorizeOrigins[provider]);
    if (url.origin !== endpoint.origin || url.pathname !== endpoint.pathname || url.username || url.password || url.hash) return null;
    if (url.searchParams.get("redirect_uri") !== `${origin}/api/v1/connectors/${provider}/callback` || !/^[A-Za-z0-9_-]{43}$/.test(url.searchParams.get("state") || "") || !url.searchParams.get("client_id")) return null;
    if (["access_token", "refresh_token", "client_secret"].some(key => url.searchParams.has(key))) return null;
    if (provider !== "slack" && (url.searchParams.get("code_challenge_method") !== "S256" || !/^[A-Za-z0-9_-]{43}$/.test(url.searchParams.get("code_challenge") || ""))) return null;
    return url.href;
  } catch { return null; }
}

export function parseAppConnections(value: unknown): AppConnection[] {
  if (!Array.isArray(value) || value.length !== appProviders.length) throw new Error("应用连接状态读取失败，请重试。");
  const seen = new Set<string>();
  for (const item of value) {
    if (!item || !appProviders.includes(item.provider) || seen.has(item.provider) || typeof item.configured !== "boolean" || !["connected", "disconnected", "reauthorization_required"].includes(item.status) || typeof item.account_name !== "string" || !Number.isSafeInteger(item.revision) || item.revision < 0 || !Array.isArray(item.scopes) || item.scopes.length > 64 || item.scopes.some((scope: unknown) => typeof scope !== "string" || scope.length > 256)) throw new Error("应用连接状态读取失败，请重试。");
    if (item.github_code_enabled !== undefined && typeof item.github_code_enabled !== "boolean") throw new Error("应用连接状态读取失败，请重试。");
    if (item.provider !== "github" && item.github_code_enabled === true) throw new Error("应用连接状态读取失败，请重试。");
    seen.add(item.provider);
  }
  return value as AppConnection[];
}
