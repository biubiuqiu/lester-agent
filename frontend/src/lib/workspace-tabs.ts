import { isInternalRuntimePath } from "./file-path-policy";

export type WorkspaceToolKind = "files" | "results" | "skills" | "agent";
export type WorkspaceTabKind = WorkspaceToolKind | "terminal";
export type WorkspaceTab =
  | { id: string; kind: "file"; path: string }
  | { id: string; kind: WorkspaceToolKind }
  | { id: string; kind: "terminal"; serial: number };

export const MAX_TERMINAL_TABS = 4;
export const MAX_WORKSPACE_TABS = 24;
export const fileWorkspaceTabId = (path: string) => `file:${path}`;
export const workspaceTabElementId = (id: string) => `workspace-tab-${encodeURIComponent(id)}`;
export const workspacePaneElementId = (id: string) => `workspace-pane-${encodeURIComponent(id)}`;

const toolKinds = new Set<unknown>(["files", "results", "skills", "agent"]);
const validPath = (path: unknown): path is string => typeof path === "string" && path.length > 0 && path.length <= 1000 &&
  !path.startsWith("/") && !/[\u0000-\u001f\\]/.test(path) &&
  !path.split("/").some(part => !part || part === "." || part === "..") && !isInternalRuntimePath(path);

// Layout data has no file authority. Restore only bounded descriptors whose
// paths are also in the conversation's separately validated open-file list.
export function restoreWorkspaceTabs(value: unknown, filePaths: string[]): WorkspaceTab[] {
  if (!Array.isArray(value)) return [...new Set(filePaths)].filter(validPath).map(path => ({ id: fileWorkspaceTabId(path), kind: "file", path }));
  const files = new Set(filePaths), ids = new Set<string>(), serials = new Set<number>();
  const result: WorkspaceTab[] = [];
  for (const item of value.slice(0, 64)) {
    if (!item || typeof item !== "object" || typeof item.id !== "string" || ids.has(item.id)) continue;
    let tab: WorkspaceTab;
    if (item.kind === "file" && validPath(item.path) && files.has(item.path) && item.id === fileWorkspaceTabId(item.path)) {
      tab = { id: item.id, kind: "file", path: item.path };
    } else if (toolKinds.has(item.kind) && item.id === item.kind) {
      tab = { id: item.id, kind: item.kind as WorkspaceToolKind };
    } else if (item.kind === "terminal" && /^terminal:[a-zA-Z0-9_-]{1,100}$/.test(item.id) &&
      Number.isSafeInteger(item.serial) && item.serial > 0 && item.serial <= 1_000_000 &&
      serials.size < MAX_TERMINAL_TABS && !serials.has(item.serial)) {
      tab = { id: item.id, kind: "terminal", serial: item.serial };
      serials.add(item.serial);
    } else continue;
    ids.add(tab.id); result.push(tab);
    if (result.length >= MAX_WORKSPACE_TABS) break;
  }
  return result;
}

// File caps may evict a file, but must never evict a live terminal/tool. Keep
// the user's mixed ordering and append only genuinely new file descriptors.
export function mergeWorkspaceFiles(tabs: WorkspaceTab[], filePaths: string[]): WorkspaceTab[] {
  const files = new Set(filePaths);
  const next = tabs.filter(tab => tab.kind !== "file" || files.has(tab.path));
  const existing = new Set(next.map(tab => tab.id));
  for (const path of filePaths) {
    const id = fileWorkspaceTabId(path);
    if (!existing.has(id)) { next.push({ id, kind: "file", path }); existing.add(id); }
  }
  return next;
}

export function removeWorkspaceTab(tabs: WorkspaceTab[], active: string | null, id: string) {
  const index = tabs.findIndex(tab => tab.id === id);
  if (index < 0) return { tabs, active };
  const next = tabs.filter(tab => tab.id !== id);
  return { tabs: next, active: active === id ? next[Math.min(index, next.length - 1)]?.id ?? null : active };
}
