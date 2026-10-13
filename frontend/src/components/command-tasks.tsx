"use client";

import { useEffect, useRef, useState } from "react";
import { T, useT } from "@/components/i18n";
import { API, api } from "@/lib/api";
import { authenticatedFetch } from "@/lib/auth-client";
import { CommandLogDecoder } from "@/lib/command-log-decoder";

type Task = { task_id: string; run_id: string; status: string; revision?: number; description?: string; shell?: string; exit_code?: number | null; cleanup_confirmed?: boolean; stored_bytes?: number; omitted_bytes?: number; truncated?: boolean; sync_error?: boolean; page?: { data: string; offset: number; next_offset: number; has_more: boolean } };
const labels: Record<string, string> = { starting: "正在启动", running: "运行中", stopping: "正在停止", exited: "已结束", cancelled: "已停止", timed_out: "命令执行超时", unknown: "命令状态尚未确认" };

function TaskRow({ conversationId, initial }: { conversationId: string; initial: Task }) {
  const t = useT();
  const [observed, setTask] = useState(initial);
  const task = (initial.revision || 0) > (observed.revision || 0) ? { ...observed, ...initial } : observed;
  const [text, setText] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const pending = useRef(false);
  const active = useRef(true);
  const logDecoder = useRef(new CommandLogDecoder());
  useEffect(() => { active.current = true; return () => { active.current = false; }; }, []);
  async function inspect(offset = 0, stop = false) {
    if (pending.current) return;
    pending.current = true; setBusy(true); setError("");
    try {
      const next = await api<Task>(`/api/v1/conversations/${conversationId}/tasks/${task.task_id}${stop ? "/stop" : `?offset=${offset}&limit=65536`}`, stop ? { method: "POST" } : undefined);
      if (!active.current) return;
      setTask(previous => ({ ...next, page: stop ? previous.page : next.page }));
      if (next.sync_error) setError(stop ? "停止结果未确认，请检查任务状态。" : "命令日志暂时无法同步，请重试。");
      if (!stop && next.page) setText(logDecoder.current.decode(next.page, ["exited", "cancelled", "timed_out"].includes(next.status)));
    } catch { if (active.current) { setError(stop ? "停止结果未确认，请检查任务状态。" : "命令日志暂时无法同步，请重试。"); if(stop) setTask(previous => ({ ...previous,status:"unknown",exit_code:null,cleanup_confirmed:false })); } }
    finally { pending.current = false; if (active.current) setBusy(false); }
  }
  const running = !["exited", "cancelled", "timed_out"].includes(task.status) || task.cleanup_confirmed === false;
  async function download() {
    if (pending.current) return;
    pending.current = true; setBusy(true); setError("");
    try {
      const response = await authenticatedFetch(`${API}/api/v1/conversations/${conversationId}/tasks/${task.task_id}/log`, { credentials: "include" });
      if (!response.ok) throw new Error("log unavailable");
      const blob = await response.blob();
      if (!active.current) return;
      const url = URL.createObjectURL(blob);
      const link = document.createElement("a");
      link.href = url; link.download = `${task.task_id}.log`; link.click();
      window.setTimeout(() => URL.revokeObjectURL(url), 1000);
    } catch { if (active.current) setError("命令日志暂时无法同步，请重试。"); }
    finally { pending.current = false; if (active.current) setBusy(false); }
  }
  return <article className="command-task-row">
    <div><strong>{task.description || task.task_id.slice(0, 8)}</strong><small>{[t(labels[task.status] || labels.unknown), task.exit_code != null ? t("退出码 {0}", [task.exit_code]) : "", task.shell || ""].filter(Boolean).join(" · ")}</small></div>
    <div className="command-task-actions"><button type="button" disabled={busy} onClick={() => void inspect()}><T>{"查看日志"}</T></button>{running && <button type="button" disabled={busy || task.status === "stopping"} onClick={() => void inspect(0, true)}><T>{"停止命令"}</T></button>}</div>
    {error && <p role="alert" className="settings-error">{t(error)}</p>}
    {task.truncated && <p className="field-help"><T>{"日志已达到 64 MiB 上限，超出的输出没有保存。"}</T></p>}
    {text != null && <div className="command-task-log"><pre aria-label={t("命令输出日志")}>{text || t("暂时没有输出。")}</pre><div className="command-task-actions"><button type="button" disabled={busy} onClick={() => void inspect()}><T>{"从头查看"}</T></button>{task.page?.has_more && <button type="button" disabled={busy} onClick={() => void inspect(task.page?.next_offset)}><T>{"下一页日志"}</T></button>}<button type="button" disabled={busy} onClick={() => void download()}><T>{"下载已保存日志"}</T></button></div></div>}
  </article>;
}

export function CommandTasks({ conversationId, revision }: { conversationId: string; revision: number }) {
  const t = useT();
  const [tasks, setTasks] = useState<Task[]>([]);
  const [error, setError] = useState(false);
  const [retry, setRetry] = useState(0);
  useEffect(() => {
    const controller = new AbortController();
    let pending = false;
    async function load() {
      if (pending || document.hidden) return;
      pending = true;
      try {
        const data = await api<{ tasks: Task[] }>(`/api/v1/conversations/${conversationId}/tasks`, { signal: controller.signal });
        if (!controller.signal.aborted) { setTasks(data.tasks); setError(false); }
      } catch { if (!controller.signal.aborted) setError(true); }
      finally { pending = false; }
    }
    void load();
    const interval = window.setInterval(load, 5000);
    document.addEventListener("visibilitychange", load);
    return () => { controller.abort(); clearInterval(interval); document.removeEventListener("visibilitychange", load); };
  }, [conversationId, revision, retry]);
  if (!tasks.length && !error) return null;
  return <details className="command-tasks"><summary>{t("命令任务 · {0}", [tasks.length])}</summary><p className="field-help"><T>{"后台命令可在 Agent 回复后继续运行。停止生成不会停止这些命令。"}</T></p>{error && <p role="status"><T>{"命令任务状态同步失败。"}</T><button type="button" className="text-button" onClick={() => setRetry(value => value + 1)}><T>{"重新同步状态"}</T></button></p>}{tasks.map(task => <TaskRow key={task.task_id} initial={task} conversationId={conversationId} />)}</details>;
}
