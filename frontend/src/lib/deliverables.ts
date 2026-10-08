import type { FileEntry } from "./api";

const supportDirectories = new Set(["agent-resources", "node_modules", "vendor", "venv", "__pycache__"]);
const supportDocuments = /^(readme(?:[.-].*)?|changelog|license|agents|claude|skill)\.md$/i;

// This is a file-type view of the live inventory, not a claim of task success.
export function deliverableKind(file: FileEntry): "html" | "markdown" | null {
  const segments = file.path.split("/");
  if (file.is_dir || segments.some((part) => part.startsWith(".") || supportDirectories.has(part)) || supportDocuments.test(file.name)) return null;
  if (/\.html?$/i.test(file.name)) return "html";
  if (/\.md$/i.test(file.name)) return "markdown";
  return null;
}

export function conversationDeliverables(files: FileEntry[]): FileEntry[] {
  return files.filter((file) => deliverableKind(file) !== null).toSorted((a, b) => b.modified_at.localeCompare(a.modified_at) || a.path.localeCompare(b.path));
}
