import type { FileEntry } from "./api";

// Inventory remains live, but the reply footer is a settled-run summary.
// `running` includes sending and cancelling, including restored active runs.
export function artifactFiles(files: FileEntry[], changes: { path: string; kind: string }[], running: boolean): FileEntry[] {
  if (running) return [];
  return changes.filter((change) => change.kind !== "deleted").flatMap((change) => {
    const file = files.find((item) => item.path === change.path);
    return file ? [file] : [];
  }).slice(0, 12);
}
