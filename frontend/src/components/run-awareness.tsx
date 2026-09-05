"use client";

import { AlertCircle, ArrowRight, Bell, LoaderCircle, RotateCcw, X } from "lucide-react";
import { useEffect, useMemo, useState } from "react";
import { ConversationRunStatus } from "@/lib/api";
import { activityLabel } from "@/lib/activity-label";
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

  const runEvents = useMemo(() => events.filter((event) => runId ? event.run_id === runId : false), [events, runId]);
  const started = runEvents.find((event) => event.type === "RUN_STARTED") ?? runEvents[0];
  const startedAt = started ? Date.parse(started.created_at) : now;
  const elapsed = formatElapsed(Number.isFinite(startedAt) ? Math.max(0, now - startedAt) : 0);
  const latest = runEvents.findLast((event) => activityEventTypes.has(event.type));
  const label = useMemo(() => activityLabel(state, latest), [state, latest]);

  return (
    <div className={`agent-activity-indicator ${state}`} >
      <span className="activity-pulse" aria-hidden><i /><i /><i /></span>
      <span className="agent-activity-copy"><strong role="status" aria-live="polite">{label}</strong><small aria-label={`${agent} 任务耗时`} aria-live="off">· {elapsed}</small></span>
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
        <strong>这次任务没有完成</strong><p>先检查现有结果，再决定是否继续；重试可能重复已执行的操作。</p>
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

function formatElapsed(milliseconds: number) {
  const totalSeconds = Math.floor(milliseconds / 1000);
  const minutes = Math.floor(totalSeconds / 60);
  const seconds = totalSeconds % 60;
  return minutes > 0 ? `${minutes}:${String(seconds).padStart(2, "0")}` : `${seconds} 秒`;
}
