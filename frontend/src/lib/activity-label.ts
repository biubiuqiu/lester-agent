type ActivityEvent = { type: string; payload: Record<string, unknown> };
const toolLabels: Record<string, string> = { bash: "正在运行命令", read: "正在读取", write: "正在写入", edit: "正在修改", load_skill: "正在加载 Skill" };
export function activityLabel(state: "sending" | "running" | "stopping", latest?: ActivityEvent, t: (source: string) => string = source => source) {
  if (state === "sending") return t("正在准备任务");
  if (state === "stopping") return t("正在停止当前任务");
  if (!latest) return t("等待任务进展");
  if (latest.type === "TOOL_STARTED") {
    const tool = String(latest.payload.tool ?? "");
    let args: Record<string, unknown> = {};
    try { args = typeof latest.payload.arguments === "string" ? JSON.parse(latest.payload.arguments) : latest.payload.arguments ?? {}; } catch { /* Never interpret partial JSON as executable tool input. */ }
    const raw = args && (args.file_path ?? args.path);
    const path = typeof raw === "string" ? raw.replaceAll("\\", "/").split("/").at(-1)?.slice(0, 100) : "";
    const label = t(toolLabels[tool] ?? "正在使用工具");
    return ["read", "write", "edit"].includes(tool) ? `${label} ${path || t("文件")}` : label;
  }
  if (latest.type === "TOOL_COMPLETED") return t("工具已完成，等待下一步");
  if (latest.type === "TOOL_FAILED") return t("工具执行失败，等待下一步");
  if (latest.type === "COMMAND_STARTED") return t("正在运行命令");
  if (latest.type === "FILE_UPDATED") return t("文件已更新，等待下一步");
  if (latest.type === "MODEL_STARTED" || latest.type === "MODEL_DELTA" || latest.type === "MODEL_TEXT") return t("正在生成回复");
  return t("任务已开始");
}
