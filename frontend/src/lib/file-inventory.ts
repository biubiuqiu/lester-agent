import { api, type FileEntry } from "./api";

export type Directory = { entries: FileEntry[]; error: string };
export type FileState = { directories: Record<string, Directory>; files: FileEntry[]; signature: string; limited: boolean; error: string };
const skippedDirectories = new Set([".git", ".agent", "node_modules", ".next", ".venv", "venv", "__pycache__", ".cache"]);

// A newly opened, previously skipped directory is discovery, not file creation.
export function wasPathCovered(path: string, directories: Record<string, Directory>) {
  const parts = path.split("/");
  let parent = "";
  for (const part of parts) {
    const directory = directories[parent];
    if (!directory || directory.error) return false;
    const child = parent ? `${parent}/${part}` : part;
    if (!directory.entries.some((entry) => entry.path === child)) return true;
    parent = child;
  }
  return true;
}

export function relativeFilePath(conversationId: string, path: string) {
  const root = `/workspace/conversations/${conversationId}/`;
  const normalized = path.replaceAll("\\", "/");
  const relative = normalized.startsWith(root) ? normalized.slice(root.length) : normalized.replace(/^\.\//, "");
  if (!relative || relative.startsWith("/") || relative.split("/").includes("..")) return "";
  return relative;
}

export async function listDirectory(conversationId: string, path: string, signal?: AbortSignal) {
  const { files } = await api<{ files: FileEntry[] }>(`/api/v1/conversations/${conversationId}/files?path=${encodeURIComponent(path)}`, { signal });
  return files.map((file) => ({ ...file, path: relativeFilePath(conversationId, file.path) })).filter((file) => file.path).sort((a, b) => Number(b.is_dir) - Number(a.is_dir) || a.name.localeCompare(b.name, "zh-CN", { numeric: true }));
}

export async function scanFiles(conversationId: string, signal: AbortSignal, watched: string[]): Promise<FileState> {
  const directories: FileState["directories"] = {};
  const files: FileEntry[] = [];
  const queue = [{ path: "", depth: 0 }, ...watched.map((path) => ({ path, depth: 5 }))];
  const visited = new Set<string>();
  let limited = false;
  let count = 0;
  while (queue.length && count < 64 && files.length < 2000) {
    const batch = queue.splice(0, Math.min(4, 64 - count));
    count += batch.length;
    await Promise.all(batch.map(async ({ path, depth }) => {
      if (visited.has(path)) return;
      visited.add(path);
      try {
        const entries = await listDirectory(conversationId, path, signal);
        directories[path] = { entries, error: "" };
        for (const file of entries) {
          if (!file.is_dir) { if (files.length < 2000) files.push(file); else limited = true; }
          else if (!skippedDirectories.has(file.name)) {
            if (depth < 5) queue.push({ path: file.path, depth: depth + 1 }); else limited = true;
          }
        }
      } catch (reason) {
        if (!path || signal.aborted) throw reason;
        limited = true;
        directories[path] = { entries: [], error: reason instanceof Error ? reason.message : "目录加载失败" };
      }
    }));
  }
  limited ||= queue.length > 0;
  files.sort((a, b) => a.path.localeCompare(b.path));
  const signature = JSON.stringify(Object.entries(directories).sort(([a], [b]) => a.localeCompare(b)));
  return { directories, files, signature, limited, error: "" };
}
