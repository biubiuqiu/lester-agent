type ActivityEvent = { type: string; payload: Record<string, unknown> };
const toolLabels: Record<string, string> = { bash: "正在运行命令", read: "正在读取", write: "正在写入", edit: "正在修改", load_skill: "正在加载 Skill" };
export function activityLabel(state: "sending" | "running" | "stopping", latest?: ActivityEvent) {
  if (state === "sending") return "正在准备任务";
  if (state === "stopping") return "正在停止当前任务";
  if (!latest) return "等待任务进展";
  if (latest.type === "TOOL_STARTED") {
    const tool = String(latest.payload.tool ?? "");
    let args: Record<string, unknown> = {};
    try { args = typeof latest.payload.arguments === "string" ? JSON.parse(latest.payload.arguments) : latest.payload.arguments ?? {}; } catch { /* Never interpret partial JSON as executable tool input. */ }
    const raw = args && (args.file_path ?? args.path);
    const path = typeof raw === "string" ? raw.replaceAll("\\", "/").split("/").at(-1)?.slice(0, 100) : "";
    const label = toolLabels[tool] ?? "正在使用工具";
    return ["read", "write", "edit"].includes(tool) ? `${label} ${path || "文件"}` : label;
  }
  if (latest.type === "TOOL_COMPLETED") return "工具已完成，等待下一步";
  if (latest.type === "TOOL_FAILED") return "工具执行失败，等待下一步";
  if (latest.type === "COMMAND_STARTED") return "正在运行命令";
  if (latest.type === "FILE_UPDATED") return "文件已更新，等待下一步";
  if (latest.type === "MODEL_STARTED" || latest.type === "MODEL_DELTA" || latest.type === "MODEL_TEXT") return "正在生成回复";
  return "任务已开始";
}
