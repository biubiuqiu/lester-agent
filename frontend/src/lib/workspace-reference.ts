// Resolve a URL relative to a conversation file without leaving its directory tree.
export function resolveWorkspaceReference(baseFile: string, reference: string): string {
  const value = reference.trim();
  if (!value || value.startsWith("#") || /^(?:[a-z][a-z0-9+.-]*:|\/\/)/i.test(value)) return "";
  const pathOnly = value.split(/[?#]/, 1)[0];
  if (!pathOnly) return baseFile;
  const parts = value.startsWith("/") ? [] : baseFile.replaceAll("\\", "/").split("/").slice(0, -1);
  for (const raw of pathOnly.split("/")) {
    let part: string;
    try { part = decodeURIComponent(raw); } catch { return ""; }
    if (/[\\/\x00-\x1f]/.test(part)) return "";
    if (!part || part === ".") continue;
    if (part === "..") {
      if (!parts.length) return "";
      parts.pop();
    } else parts.push(part);
  }
  return parts.join("/");
}
