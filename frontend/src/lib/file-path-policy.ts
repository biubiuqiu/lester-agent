// Runtime bookkeeping and dependency trees are not task outputs. Share this
// policy between discovery, notifications and the task's file-change summary.
const excludedDirectories = new Set([".agent", ".lester", ".git", "node_modules", ".next", ".venv", "venv", "__pycache__", ".cache"]);

export function isFileAutoOpenExcluded(path: string) {
  return path.replaceAll("\\", "/").split("/").some(part => excludedDirectories.has(part));
}

export function isInternalRuntimePath(path: string) {
  return path.replaceAll("\\", "/").split("/").includes(".lester");
}
