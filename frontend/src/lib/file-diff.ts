export type DiffLine = { kind: "context" | "added" | "removed"; text: string };
// Bounded exact line comparison of retained file bytes. Large line counts use
// the original before/after text panes rather than inventing a shortened diff.
export function fileDiff(before: string, after: string): DiffLine[] | null {
  const a = before.split("\n"), b = after.split("\n");
  if (a.length > 1000 || b.length > 1000) return null;
  const width = b.length + 1, table = new Uint16Array((a.length + 1) * width);
  for (let i = a.length - 1; i >= 0; i--) for (let j = b.length - 1; j >= 0; j--) table[i * width + j] = a[i] === b[j] ? 1 + table[(i + 1) * width + j + 1] : Math.max(table[(i + 1) * width + j], table[i * width + j + 1]);
  const result: DiffLine[] = [];
  let i = 0, j = 0;
  while (i < a.length || j < b.length) {
    if (i < a.length && j < b.length && a[i] === b[j]) { result.push({ kind: "context", text: a[i++] }); j++; }
    else if (i < a.length && (j === b.length || table[(i + 1) * width + j] >= table[i * width + j + 1])) result.push({ kind: "removed", text: a[i++] });
    else result.push({ kind: "added", text: b[j++] });
  }
  return result;
}
