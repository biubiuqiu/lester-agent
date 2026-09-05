// Tab-local UI state, never part of the model transcript or shared across accounts.
export type ViewState = {
  text: string;
  files: File[];
  missingFiles: string[];
  reference: string | null;
  tabs: string[];
  selected: string | null;
  treeOpen: boolean;
  directories: string[];
  modes: Record<string, "preview" | "source">;
  positions: Record<string, { top: number; left: number }>;
  thread: { top: number; following: boolean };
};
const prefix = "lester.view.v1.";
const cache = new Map<string, ViewState>();
let writeScope: string | null = "";
export function resumeViewState(scope: string) { writeScope = scope; }
const defaults = (): ViewState => ({ text: "", files: [], missingFiles: [], reference: null, tabs: [], selected: null, treeOpen: true, directories: [""], modes: {}, positions: {}, thread: { top: 0, following: true } });
export const viewKey = (userId: string, workspaceId: string, conversationId: string) => `${userId}.${workspaceId}.${conversationId}`;

export function readView(key: string): ViewState {
  if (!key || typeof window === "undefined" || writeScope === null || !key.startsWith(writeScope)) return defaults();
  const cached = cache.get(key);
  if (cached) return cached;
  const value = defaults();
  try {
    const saved = JSON.parse(window.sessionStorage.getItem(prefix + key) ?? "null");
    if (saved && saved.version === 1) {
      value.text = typeof saved.text === "string" ? saved.text.slice(0, 100_000) : "";
      value.reference = typeof saved.reference === "string" ? saved.reference : null;
      value.tabs = Array.isArray(saved.tabs) ? saved.tabs.filter((p: unknown) => typeof p === "string").slice(-8) : [];
      value.selected = value.tabs.includes(saved.selected) ? saved.selected : null;
      value.treeOpen = saved.treeOpen !== false;
      value.directories = Array.isArray(saved.directories) ? saved.directories.filter((p: unknown) => typeof p === "string").slice(-64) : [""];
      value.missingFiles = Array.isArray(saved.attachmentNames) ? saved.attachmentNames.filter((p: unknown) => typeof p === "string").slice(0, 100) : [];
      if (saved.modes && typeof saved.modes === "object") for (const [path, mode] of Object.entries(saved.modes).slice(-16)) {
        if (mode === "preview" || mode === "source") value.modes[path] = mode;
      }
      if (saved.positions && typeof saved.positions === "object") for (const [path, position] of Object.entries(saved.positions).slice(-24)) {
        const p = position as { top?: number; left?: number } | null;
        if (p && Number.isFinite(p.top) && Number.isFinite(p.left)) value.positions[path] = { top: Math.max(0, p.top!), left: Math.max(0, p.left!) };
      }
      if (saved.thread && Number.isFinite(saved.thread.top)) value.thread = { top: Math.max(0, saved.thread.top), following: saved.thread.following !== false };
    }
  } catch { /* Storage may be unavailable; in-memory navigation still works. */ }
  cache.set(key, value);
  return value;
}

export function updateView(key: string, patch: Partial<ViewState>) {
  if (!key || typeof window === "undefined" || writeScope === null || !key.startsWith(writeScope)) return;
  const next = { ...readView(key), ...patch };
  cache.delete(key);
  cache.set(key, next);
  while (cache.size > 50) cache.delete(cache.keys().next().value!);
  try {
    const { files, missingFiles, ...serializable } = next;
    window.sessionStorage.setItem(prefix + key, JSON.stringify({ ...serializable, text: next.text.slice(0, 100_000), version: 1, attachmentNames: [...files.map((file) => file.name), ...missingFiles] }));
    const keys = Object.keys(window.sessionStorage).filter((item) => item.startsWith(prefix));
    for (const old of keys.slice(0, Math.max(0, keys.length - 50))) if (old !== prefix + key) window.sessionStorage.removeItem(old);
  } catch { /* Quota/private mode must never break sending or navigation. */ }
}

export function clearViewState() {
  // Unmount/pagehide callbacks after logout must not write the cleared draft back.
  writeScope = null;
  cache.clear();
  if (typeof window === "undefined") return;
  try { for (const key of Object.keys(window.sessionStorage)) if (key.startsWith(prefix)) window.sessionStorage.removeItem(key); } catch { /* best effort */ }
}
