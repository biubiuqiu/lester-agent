"use client";

import { AlertCircle, ArrowRight, Bell, LoaderCircle, RotateCcw, X } from "lucide-react";
import { useEffect, useState } from "react";
import { ConversationRunStatus } from "@/lib/api";
import { RunEvent } from "./tool-timeline";

export type UnreadRunResult = {
  runId: string;
  kind: "completed" | "failed";
};

export type RunNotice = UnreadRunResult & {
  conversationId: string;
  title: string;
};

type ActivityState = "sending" | "running" | "stopping";

export function ConversationRunMark({ status, unread }: { status: ConversationRunStatus; unread?: UnreadRunResult }) {
  if (status === "running" || status === "cancelling") {
    return <span className={`conversation-run-mark ${status}`} title={status === "cancelling" ? "正在停止" : "正在工作"}><LoaderCircle /></span>;
  }
  if (unread) {
    const label = unread.kind === "failed" ? "有一个后台任务需要查看" : "有一个后台任务已完成";
    return <span className={`conversation-unread-dot ${unread.kind}`} title={label} role="status" aria-label={label} />;
  }
  return null;
}

export function RunNoticeToast({ notice, onOpen, onDismiss }: { notice: RunNotice; onOpen: () => void; onDismiss: () => void }) {
  const failed = notice.kind === "failed";
  return (
    <aside className={`run-notice ${notice.kind}`} role={failed ? "alert" : "status"} aria-live={failed ? "assertive" : "polite"}>
      <span className="run-notice-icon"><Bell /></span>
      <button type="button" className="run-notice-copy" onClick={onOpen}>
        <strong>{failed ? "后台任务需要查看" : "后台任务已完成"}</strong>
        <small>{notice.title}</small>
      </button>
      <button type="button" className="run-notice-open" onClick={onOpen}>打开</button>
      <button type="button" className="run-notice-dismiss" onClick={onDismiss} aria-label="关闭提醒"><X /></button>
    </aside>
  );
}

export function AgentActivityIndicator({ agent, state, runId, events }: { agent: string; state: ActivityState; runId?: string; events: RunEvent[] }) {
  const [now, setNow] = useState(() => Date.now());
  useEffect(() => {
    const timer = window.setInterval(() => setNow(Date.now()), 1000);
    return () => window.clearInterval(timer);
  }, []);

  const runEvents = events.filter((event) => !runId || event.run_id === runId);
  const started = runEvents.find((event) => event.type === "RUN_STARTED") ?? runEvents[0];
  const startedAt = started ? Date.parse(started.created_at) : now;
  const elapsed = formatElapsed(Number.isFinite(startedAt) ? Math.max(0, now - startedAt) : 0);
  const latest = runEvents.findLast((event) => activityEventTypes.has(event.type));
  const label = activityLabel(state, latest);

  return (
    <div className={`agent-activity-indicator ${state}`} role="status" aria-live="polite">
      <span className="activity-pulse" aria-hidden><i /><i /><i /></span>
      <span className="agent-activity-copy"><strong>{label}</strong><small>{agent} · {elapsed}</small></span>
    </div>
  );
}

export function RunFailureRecovery({ reason, lastPrompt, onPrepare }: { reason: string; lastPrompt?: string; onPrepare: (content: string) => void }) {
  const retryPrompt = lastPrompt?.trim();
  const continuePrompt = "请检查当前会话目录里已经产生的结果，从上次中断处继续；不要重复已经成功完成的步骤。";
  return (
    <section className="run-recovery" aria-label="任务恢复选项">
      <span className="run-recovery-icon"><AlertCircle /></span>
      <div className="run-recovery-copy">
        <strong>这次任务没有完成</strong>
        <details><summary>查看原因</summary><code>{reason}</code></details>
      </div>
      <div className="run-recovery-actions">
        {retryPrompt ? <button type="button" onClick={() => onPrepare(retryPrompt)} title="填入输入框，确认后再发送"><RotateCcw />重试原任务</button> : null}
        <button type="button" onClick={() => onPrepare(continuePrompt)} title="填入输入框，确认后再发送">从当前结果继续<ArrowRight /></button>
      </div>
    </section>
  );
}

const activityEventTypes = new Set([
  "RUN_STARTED",
  "MODEL_STARTED",
  "MODEL_DELTA",
  "MODEL_TEXT",
  "TOOL_STARTED",
  "TOOL_COMPLETED",
  "TOOL_FAILED",
  "COMMAND_STARTED",
  "FILE_UPDATED",
]);

function activityLabel(state: ActivityState, latest?: RunEvent) {
  if (state === "sending") return "正在准备任务";
  if (state === "stopping") return "正在安全停止";
  if (!latest) return "正在思考下一步";
  if (latest.type === "TOOL_STARTED") return toolActivityText(String(latest.payload.tool ?? ""));
  if (latest.type === "TOOL_COMPLETED") return "工具已完成，正在继续";
  if (latest.type === "TOOL_FAILED") return "工具遇到问题，正在处理";
  if (latest.type === "COMMAND_STARTED") return "正在运行命令";
  if (latest.type === "FILE_UPDATED") return `正在更新 ${shortPath(String(latest.payload.path ?? "文件"))}`;
  if (latest.type === "MODEL_STARTED") return "正在组织下一步";
  if (latest.type === "MODEL_DELTA" || latest.type === "MODEL_TEXT") return "正在生成回复";
  return "正在分析任务";
}

function toolActivityText(tool: string) {
  return ({ bash: "正在运行命令", read: "正在读取文件", write: "正在写入文件", edit: "正在编辑文件", load_skill: "正在加载 Skill" } as Record<string, string>)[tool] ?? "正在使用工具";
}

function shortPath(path: string) {
  const parts = path.replaceAll("\\", "/").split("/").filter(Boolean);
  return parts.at(-1) || "文件";
}

function formatElapsed(milliseconds: number) {
  const totalSeconds = Math.floor(milliseconds / 1000);
  const minutes = Math.floor(totalSeconds / 60);
  const seconds = totalSeconds % 60;
  return minutes > 0 ? `${minutes}:${String(seconds).padStart(2, "0")}` : `${seconds} 秒`;
}
