"use client";
import { T, useT } from "@/components/i18n";



import { Check, ChevronDown, CircleX, FileText, LoaderCircle, Square, TerminalSquare, Wrench } from "lucide-react";
import { MessageContent } from "./message-content";
import { commandOutcome, commandResult } from "@/lib/command-outcome";

export type RunEvent = {
  id: number;
  run_id: string;
  conversation_id?: string;
  type: string;
  payload: Record<string, unknown>;
  created_at: string;
};

type ToolActivity = {
  id: number;
  tool: string;
  status: "running" | "completed" | "failed" | "cancelled" | "unknown";
  events: RunEvent[];
};

type NarrativeItem =
  | { kind: "text"; event: RunEvent }
  | { kind: "tool"; activity: ToolActivity }
  | { kind: "failure"; event: RunEvent }
  | { kind: "context"; event: RunEvent; id: number }
  | { kind: "cancellation"; event: RunEvent };

export function RunNarrative({ events, hideFinalText = false }: { events: RunEvent[]; hideFinalText?: boolean }) {
  const t = useT();

  const status = runStatus(events);
  let items = narrativeItems(events);
  if (status === "completed" && hideFinalText) {
    const lastText = items.findLastIndex((item) => item.kind === "text");
    if (lastText >= 0) items = items.filter((_, index) => index !== lastText);
  }
  if (items.length === 0) return null;

  return (
    <section className="run-narrative" aria-label={t("工具执行过程")}>
      {items.map((item) => {
        if (item.kind === "text") {
          const text = String(item.event.payload.text ?? "");
          return text ? <div className="assistant-progress" key={`text-${item.event.id}`}><MessageContent content={text} quoteSource={t("引用回答")} /></div> : null;
        }
        if (item.kind === "failure") return <RunFailure event={item.event} key={`failure-${item.event.id}`} />;
        if (item.kind === "cancellation") return <RunCancellation key={`cancellation-${item.event.id}`} />;
        if (item.kind === "context") return <ContextActivity event={item.event} running={status === "running"} key={`context-${item.id}`} />;
        return <ToolActivityRow activity={item.activity} key={`tool-${item.activity.id}`} />;
      })}
    </section>
  );
}

function ContextActivity({ event, running }: { event: RunEvent; running: boolean }) {
  const t = useT();
  const compacting = event.type === "CONTEXT_COMPACTING";
  const label = event.type === "CONTEXT_COMPACTED" ? t("历史已整理，继续任务") : compacting && running ? t("正在整理历史上下文") : compacting ? t("上下文整理已中断") : t("保留原上下文，继续检查");
  return <details className="tool-activity">
    <summary><span className="tool-activity-icon">{compacting && running ? <LoaderCircle className="tool-spinner" /> : <FileText />}</span><span>{label}</span><ChevronDown className="tool-disclosure" /></summary>
    <div className="tool-activity-details">
      <span>{t("完整聊天记录仍然保留；最新指令和未确认结果不会由摘要替代。")}</span>
      {event.type === "CONTEXT_COMPACTED" && typeof event.payload.estimated_before === "number" && typeof event.payload.estimated_after === "number" ? <code>{t("请求输入估算：{0} → {1} token；不是 Provider 精确计数。", [String(event.payload.estimated_before), String(event.payload.estimated_after)])}</code> : null}
      {event.type === "CONTEXT_COMPACTION_FAILED" ? <span>{t("本轮已停止自动整理。旧检查点保持不变，超出预算时任务会明确停止。")}</span> : null}
    </div>
  </details>;
}

function ToolActivityRow({ activity }: { activity: ToolActivity }) {
  const t = useT();
  const label = activityLabel(activity, t);
  const details = activityDetails(activity, t);
  return (
    <details className={`tool-activity ${activity.status}`}>
      <summary>
        <span className="tool-activity-icon">{activityIcon(activity)}</span>
        <span>{label}</span>
        {activity.status === "running" ? <LoaderCircle className="tool-spinner" /> : <ChevronDown className="tool-disclosure" />}
      </summary>
      {details.length > 0 && <div className="tool-activity-details">
        {details.map((detail, index) => detail.kind === "output"
          ? <pre key={`${activity.id}-${index}`}>{detail.value}</pre>
          : <code key={`${activity.id}-${index}`}>{detail.value}</code>)}
      </div>}
    </details>
  );
}

function RunFailure({ event }: { event: RunEvent }) {
  const t = useT();

  const error = String(event.payload.error ?? t("任务执行失败"));
  return (
    <details className="run-status-line failed">
      <summary><CircleX /><span><T>{"运行失败"}</T></span><ChevronDown /></summary>
      <code>{t(error)}</code>
    </details>
  );
}

function RunCancellation() {
  return <div className="run-status-line cancelled"><Square /><span><T>{"已停止生成"}</T></span></div>;
}

function narrativeItems(events: RunEvent[]): NarrativeItem[] {
  const items: NarrativeItem[] = [];
  let current: ToolActivity | null = null;
  for (const event of events) {
    if (event.type.startsWith("CONTEXT_COMPACT")) {
      if (current) { items.push({ kind: "tool", activity: current }); current = null; }
      const previous = items.at(-1);
      if (previous?.kind === "context") previous.event = event;
      else items.push({ kind: "context", event, id: event.id });
      continue;
    }
    if (event.type === "MODEL_TEXT" || event.type === "MODEL_DELTA") {
      if (current) {
        items.push({ kind: "tool", activity: current });
        current = null;
      }
      const text = event.type === "MODEL_DELTA" ? String(event.payload.delta ?? "") : String(event.payload.text ?? "");
      if (!text) continue;
      const previous = items.at(-1);
      if (previous?.kind === "text" && previous.event.run_id === event.run_id) {
        previous.event = { ...event, type: "MODEL_TEXT", payload: { text: String(previous.event.payload.text ?? "") + text } };
      } else {
        items.push({ kind: "text", event: { ...event, type: "MODEL_TEXT", payload: { text } } });
      }
      continue;
    }
    if (event.type === "TOOL_STARTED") {
      if (current) items.push({ kind: "tool", activity: current });
      current = { id: event.id, tool: String(event.payload.tool ?? "tool"), status: "running", events: [event] };
      continue;
    }
    if (current && isToolDetail(event.type)) {
      current.events.push(event);
      if (event.type === "COMMAND_COMPLETED") {
        const outcome = commandOutcome(event.payload);
        if (outcome && outcome !== "background") current.status = outcome;
      }
      if (event.type === "TOOL_COMPLETED") {
        const outcome = commandOutcome(event.payload.result);
        current.status = outcome && outcome !== "background" ? outcome : current.status === "running" ? "completed" : current.status;
      }
      if (event.type === "TOOL_FAILED") current.status = "failed";
      if (event.type === "TOOL_COMPLETED" || event.type === "TOOL_FAILED") {
        items.push({ kind: "tool", activity: current });
        current = null;
      }
      continue;
    }
    if (event.type === "RUN_FAILED") {
      if (current) {
        current.status = "failed";
        current.events.push(event);
        items.push({ kind: "tool", activity: current });
        current = null;
      } else {
        items.push({ kind: "failure", event });
      }
    }
    if (event.type === "RUN_CANCELLED") {
      if (current) {
        current.status = "cancelled";
        current.events.push(event);
        items.push({ kind: "tool", activity: current });
        current = null;
      }
      items.push({ kind: "cancellation", event });
    }
  }
  if (current) items.push({ kind: "tool", activity: current });
  return items;
}

function isToolDetail(type: string) {
  return type.startsWith("COMMAND_") || type === "BACKGROUND_STARTED" || type === "FILE_UPDATED" || type === "TOOL_COMPLETED" || type === "TOOL_FAILED";
}

function runStatus(events: RunEvent[]) {
  if (events.some((event) => event.type === "RUN_FAILED")) return "failed";
  if (events.some((event) => event.type === "RUN_CANCELLED")) return "cancelled";
  if (events.some((event) => event.type === "RUN_COMPLETED")) return "completed";
  return "running";
}

function activityLabel(activity: ToolActivity, t: ReturnType<typeof useT>) {
  if (activity.tool === "bash" || activity.tool === "computer_exec") {
    if (activity.events.some(event => event.type === "BACKGROUND_STARTED")) return t("已启动后台命令");
    const ended = activity.events.findLast(event => event.type === "COMMAND_COMPLETED");
    if (activity.status === "unknown") return t("命令状态尚未确认");
    if (ended?.payload.status === "timed_out") return t("命令执行超时");
    if (ended && ended.payload.exit_code_known !== false && typeof ended.payload.exit_code === "number") return t("命令结束 · 退出码 {0}", [String(ended.payload.exit_code)]);
  }
  const verb = activity.status === "running" ? "正在：{0}" : activity.status === "failed" ? "未能：{0}" : activity.status === "cancelled" ? "已停止：{0}" : "已完成：{0}";
  const action = ({
    bash: "运行命令",
    read: "读取文件",
    write: "写入文件",
    edit: "编辑文件",
    read_conversation: "读取引用会话",
    load_skill: "加载 Skill",
    computer_exec: "运行命令",
    computer_list_files: "查看文件",
    computer_read_file: "读取文件",
    computer_write_file: "编辑文件",
  } as Record<string, string>)[activity.tool] ?? "调用工具";
  return t(verb, [t(action)]);
}

function activityIcon(activity: ToolActivity) {
  if (activity.status === "failed") return <CircleX />;
  if (activity.status === "cancelled") return <Square />;
  if (activity.tool === "bash" || activity.tool === "computer_exec") return <TerminalSquare />;
  if (activity.tool === "read" || activity.tool === "computer_read_file" || activity.tool === "computer_list_files") return <FileText />;
  if (activity.tool === "write" || activity.tool === "edit" || activity.tool === "computer_write_file") return <Wrench />;
  if (activity.tool === "load_skill") return <Wrench />;
  return activity.status === "completed" ? <Check /> : <Wrench />;
}

function activityDetails(activity: ToolActivity, t: ReturnType<typeof useT>) {
  const details: Array<{ kind: "code" | "output"; value: string }> = [];
  const started = activity.events.find((event) => event.type === "TOOL_STARTED");
  const command = activity.events.find((event) => event.type === "COMMAND_STARTED");
  const outputs = activity.events.filter((event) => event.type === "COMMAND_OUTPUT");
  const ended = activity.events.findLast((event) => event.type === "COMMAND_COMPLETED");
  const finished = activity.events.findLast(event => event.type === "TOOL_COMPLETED" || event.type === "TOOL_FAILED");
  const result = commandResult(finished?.payload.result);
  const background = activity.events.find((event) => event.type === "BACKGROUND_STARTED");
  const file = activity.events.find((event) => event.type === "FILE_UPDATED");
  const failed = activity.events.find((event) => event.type === "TOOL_FAILED" || event.type === "RUN_FAILED");
  const argumentsDetail = formatArguments(activity.tool, started?.payload.arguments, t);
  if (command?.payload.command) details.push({ kind: "code", value: String(command.payload.command) });
  else if (argumentsDetail) details.push({ kind: "code", value: argumentsDetail });
  const updatedPath = file?.payload.path ? String(file.payload.path) : "";
  if (updatedPath && !argumentsDetail.startsWith(updatedPath)) details.push({ kind: "code", value: updatedPath });
  if (file?.payload.replacements) details.push({ kind: "code", value: t("已替换 {0} 处", [String(file.payload.replacements)]) });
  if (background?.payload.log_path) details.push({ kind: "code", value: t("后台日志：{0}", [String(background.payload.log_path)]) });
  if (ended?.payload.shell && ended.payload.shell !== "unknown") details.push({ kind: "code", value: t("执行 Shell：{0}", [String(ended.payload.shell)]) });
  if (ended && ended.payload.exit_code_known !== false && typeof ended.payload.exit_code === "number") details.push({ kind: "code", value: t("命令结束 · 退出码 {0}", [String(ended.payload.exit_code)]) });
  for (const output of outputs) {
    const value = [output.payload.stdout, output.payload.stderr].filter(Boolean).join("\n").trim();
    if (value) details.push({ kind: "output", value });
  }
  if (result?.stdout_truncated || result?.stderr_truncated || result?.preview_truncated || result?.truncated) details.push({ kind: "output", value: t("命令输出已截断。") });
  if (failed?.payload.error) details.push({ kind: "output", value: String(failed.payload.error) });
  return details;
}

function formatArguments(tool: string, value: unknown, t: ReturnType<typeof useT>) {
  if (typeof value !== "string" || value === "{}") return "";
  try {
    const parsed = JSON.parse(value) as Record<string, unknown>;
    if (tool === "bash" || tool === "computer_exec") return String(parsed.command ?? "");
    const filePath = String(parsed.file_path ?? parsed.path ?? "");
    if (tool === "edit") return `${filePath}${t(parsed.replace_all ? " · 替换全部匹配" : " · 精确替换")}`;
    if (tool === "load_skill") return String(parsed.name ?? "");
    if (tool === "write" || tool === "read" || tool.startsWith("computer_")) return filePath;
    return Object.values(parsed).map((item) => typeof item === "string" ? item : JSON.stringify(item)).join(" · ");
  } catch {
    return value;
  }
}
