import { Check, ChevronDown, CircleX, FileText, LoaderCircle, Square, TerminalSquare, Wrench } from "lucide-react";
import { MessageContent } from "./message-content";

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
  status: "running" | "completed" | "failed" | "cancelled";
  events: RunEvent[];
};

type NarrativeItem =
  | { kind: "text"; event: RunEvent }
  | { kind: "tool"; activity: ToolActivity }
  | { kind: "failure"; event: RunEvent }
  | { kind: "cancellation"; event: RunEvent };

export function RunNarrative({ events, hideFinalText = false }: { events: RunEvent[]; hideFinalText?: boolean }) {
  const status = runStatus(events);
  let items = narrativeItems(events);
  if (status === "completed" && hideFinalText) {
    const lastText = items.findLastIndex((item) => item.kind === "text");
    if (lastText >= 0) items = items.filter((_, index) => index !== lastText);
  }
  if (items.length === 0) return null;

  return (
    <section className="run-narrative" aria-label="工具执行过程">
      {items.map((item) => {
        if (item.kind === "text") {
          const text = String(item.event.payload.text ?? "");
          return text ? <div className="assistant-progress" key={`text-${item.event.id}`}><MessageContent content={text} /></div> : null;
        }
        if (item.kind === "failure") return <RunFailure event={item.event} key={`failure-${item.event.id}`} />;
        if (item.kind === "cancellation") return <RunCancellation key={`cancellation-${item.event.id}`} />;
        return <ToolActivityRow activity={item.activity} key={`tool-${item.activity.id}`} />;
      })}
    </section>
  );
}

function ToolActivityRow({ activity }: { activity: ToolActivity }) {
  const label = activityLabel(activity);
  const details = activityDetails(activity);
  return (
    <details className={`tool-activity ${activity.status}`} open={activity.status === "running"}>
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
  const error = String(event.payload.error ?? "任务执行失败");
  return (
    <details className="run-status-line failed">
      <summary><CircleX /><span>运行失败</span><ChevronDown /></summary>
      <code>{error}</code>
    </details>
  );
}

function RunCancellation() {
  return <div className="run-status-line cancelled"><Square /><span>已停止生成</span></div>;
}

function narrativeItems(events: RunEvent[]): NarrativeItem[] {
  const items: NarrativeItem[] = [];
  let current: ToolActivity | null = null;
  for (const event of events) {
    if (event.type === "MODEL_TEXT") {
      if (current) {
        items.push({ kind: "tool", activity: current });
        current = null;
      }
      items.push({ kind: "text", event });
      continue;
    }
    if (event.type === "TOOL_STARTED") {
      if (current) items.push({ kind: "tool", activity: current });
      current = { id: event.id, tool: String(event.payload.tool ?? "tool"), status: "running", events: [event] };
      continue;
    }
    if (current && isToolDetail(event.type)) {
      current.events.push(event);
      if (event.type === "TOOL_COMPLETED") current.status = "completed";
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

function activityLabel(activity: ToolActivity) {
  const verb = activity.status === "running" ? "正在" : activity.status === "failed" ? "未能" : activity.status === "cancelled" ? "已停止" : "已";
  const action = ({
    bash: "运行命令",
    read: "读取文件",
    write: "写入文件",
    edit: "编辑文件",
    load_skill: "加载 Skill",
    computer_exec: "运行命令",
    computer_list_files: "查看文件",
    computer_read_file: "读取文件",
    computer_write_file: "编辑文件",
  } as Record<string, string>)[activity.tool] ?? "调用工具";
  return `${verb}${action}`;
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

function activityDetails(activity: ToolActivity) {
  const details: Array<{ kind: "code" | "output"; value: string }> = [];
  const started = activity.events.find((event) => event.type === "TOOL_STARTED");
  const command = activity.events.find((event) => event.type === "COMMAND_STARTED");
  const output = activity.events.find((event) => event.type === "COMMAND_OUTPUT");
  const background = activity.events.find((event) => event.type === "BACKGROUND_STARTED");
  const file = activity.events.find((event) => event.type === "FILE_UPDATED");
  const failed = activity.events.find((event) => event.type === "TOOL_FAILED" || event.type === "RUN_FAILED");
  const argumentsDetail = formatArguments(activity.tool, started?.payload.arguments);
  if (command?.payload.command) details.push({ kind: "code", value: String(command.payload.command) });
  else if (argumentsDetail) details.push({ kind: "code", value: argumentsDetail });
  const updatedPath = file?.payload.path ? String(file.payload.path) : "";
  if (updatedPath && !argumentsDetail.startsWith(updatedPath)) details.push({ kind: "code", value: updatedPath });
  if (file?.payload.replacements) details.push({ kind: "code", value: `已替换 ${String(file.payload.replacements)} 处` });
  if (background?.payload.log_path) details.push({ kind: "code", value: `后台日志：${String(background.payload.log_path)}` });
  if (output) {
    const value = [output.payload.stdout, output.payload.stderr].filter(Boolean).join("\n").trim();
    if (value) details.push({ kind: "output", value });
  }
  if (failed?.payload.error) details.push({ kind: "output", value: String(failed.payload.error) });
  return details;
}

function formatArguments(tool: string, value: unknown) {
  if (typeof value !== "string" || value === "{}") return "";
  try {
    const parsed = JSON.parse(value) as Record<string, unknown>;
    if (tool === "bash" || tool === "computer_exec") return String(parsed.command ?? "");
    const filePath = String(parsed.file_path ?? parsed.path ?? "");
    if (tool === "edit") return `${filePath}${parsed.replace_all ? " · 替换全部匹配" : " · 精确替换"}`;
    if (tool === "load_skill") return String(parsed.name ?? "");
    if (tool === "write" || tool === "read" || tool.startsWith("computer_")) return filePath;
    return Object.values(parsed).map((item) => typeof item === "string" ? item : JSON.stringify(item)).join(" · ");
  } catch {
    return value;
  }
}
