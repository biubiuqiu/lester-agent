export type CommandOutcome = "completed" | "failed" | "cancelled" | "unknown" | "background";

export function commandResult(value: unknown): Record<string, unknown> | null {
  if (typeof value === "string") {
    try { value = JSON.parse(value); } catch { return null; }
  }
  return value && typeof value === "object" && !Array.isArray(value) ? value as Record<string, unknown> : null;
}

export function commandOutcome(value: unknown): CommandOutcome | null {
  const result = commandResult(value);
  if (!result) return null;
  if (result.status === "running" && typeof result.task_id === "string") return "background";
  if (result.status === "cancelled") return result.cleanup_confirmed === false ? "unknown" : "cancelled";
  if (result.status === "unknown" || result.exit_code_known === false) return "unknown";
  if (result.status === "timed_out") return "failed";
  if (typeof result.exit_code !== "number" || !Number.isFinite(result.exit_code)) return null;
  return result.exit_code === 0 ? "completed" : "failed";
}
