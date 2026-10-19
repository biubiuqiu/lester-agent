"use client";

import { useEffect, useRef, useState } from "react";
import { createPortal } from "react-dom";
import Link from "next/link";
import { Activity, ArrowLeft, ArrowUpRight, History, RefreshCw, X } from "lucide-react";
import { api, type Conversation, type Project } from "@/lib/api";
import { useI18n, useT } from "./i18n";
import { RecentConversationList } from "./recent-conversation-list";
import type { UnreadRunResult } from "./run-awareness";

type Task = { run_id: string; conversation_id: string; title: string; project: string; agent: string; state: string; started_at: string; completed_at: string | null; last_activity_at: string; queued: number; deliverables: number; background_commands: number; unknown_commands: number };
type Feed = { tasks: Task[]; next_cursor: string; attention_count: number; observed_at: string };
type Usage = { elapsed_seconds: number | null; requests: number; in_flight: number; actual_input_tokens: number; actual_input_requests: number; actual_output_tokens: number; actual_output_requests: number; estimated_input_tokens: number; estimated_input_requests: number; missing_output_requests: number; observed_at: string };
const filters = { all: "全部任务", active: "进行中", attention: "需处理", finished: "已结束" };
const states: Record<string, string> = { running: "正在工作", cancelling: "正在停止", waiting_permission: "等待操作确认", waiting_plan: "等待计划审阅", waiting_schedule: "定时任务等待确认", waiting_question: "等待你的回答", completed: "已完成", failed: "需要检查", cancelled: "已停止" };

export function RecentConversations({ revision, onOpen, conversations, projects, currentId, unread }: { revision: number; onOpen: (id: string) => void; conversations: Conversation[]; projects: Project[]; currentId?: string; unread: Record<string, UnreadRunResult> }) {
  const t = useT(), { locale } = useI18n();
  const [open, setOpen] = useState(false), [filter, setFilter] = useState<keyof typeof filters>("all");
  const [view, setView] = useState<"recent" | "tasks">("recent");
  const taskOpen = open && view === "tasks";
  const [feed, setFeed] = useState<{ query: string; data: Feed } | null>(null);
  const [error, setError] = useState(false), [loading, setLoading] = useState(false), [more, setMore] = useState(false), [attempt, setAttempt] = useState(0);
  const [attention, setAttention] = useState(0);
  const pagesExpanded = useRef(false);
  const trigger = useRef<HTMLButtonElement>(null), dialog = useRef<HTMLDialogElement>(null), generation = useRef(0), morePending = useRef(false);
  const close = () => { dialog.current?.close(); setOpen(false); trigger.current?.focus(); };
  useEffect(() => {
    const panel = dialog.current;
    if (!open || !panel) return;
    if (!panel.open) panel.showModal();
    panel.querySelector<HTMLElement>(taskOpen ? ".recent-conversations-back" : "input[type=search]")?.focus({ preventScroll: true });
  }, [open, taskOpen]);
  useEffect(() => {
    const epoch = ++generation.current, controller = new AbortController(); let pending = false; pagesExpanded.current = false; morePending.current = false;
    const load = async () => {
      if (pending || document.hidden) return;
      pending = true; setLoading(true); setMore(false); setError(false);
      try {
        const query = taskOpen ? filter : "attention";
        const data = await api<Feed>(`/api/v1/tasks?filter=${query}&limit=${taskOpen ? 20 : 1}`, { signal: controller.signal });
        if (generation.current === epoch) { setFeed({ query, data }); setAttention(data.attention_count); }
      } catch { if (!controller.signal.aborted && generation.current === epoch) setError(true); }
      finally { pending = false; if (generation.current === epoch) setLoading(false); }
    };
    // Paging uses a stable cursor; a refresh starts again at the latest page.
    const visible = () => { if (!document.hidden && !pagesExpanded.current) void load(); };
    void load(); const timer = window.setInterval(visible, 15000);
    document.addEventListener("visibilitychange", visible);
    return () => { generation.current = epoch + 1; controller.abort(); clearInterval(timer); document.removeEventListener("visibilitychange", visible); };
  }, [taskOpen, filter, revision, attempt]);
  const loadMore = async () => {
    if (morePending.current || !feed?.data.next_cursor || loading) return;
    morePending.current = true; setMore(true); const epoch = generation.current;
    try {
      const data = await api<Feed>(`/api/v1/tasks?filter=${filter}&limit=20&cursor=${encodeURIComponent(feed.data.next_cursor)}`);
      if (epoch === generation.current) { pagesExpanded.current = true; setFeed(previous => previous && previous.query === filter ? { query: filter, data: { ...data, tasks: [...previous.data.tasks, ...data.tasks.filter(item => !previous.data.tasks.some(old => old.run_id === item.run_id))] } } : previous); setAttention(data.attention_count); }
    } catch { if (epoch === generation.current) setError(true); }
    finally { if (epoch === generation.current) { morePending.current = false; setMore(false); } }
  };
  const current = feed?.query === filter ? feed.data : null;
  const stamp = (value: string) => new Intl.DateTimeFormat(locale, { month: "short", day: "numeric", hour: "2-digit", minute: "2-digit" }).format(new Date(value));
  return <>
    <button ref={trigger} type="button" className="cloud-tasks-trigger" onClick={() => { setView("recent"); setOpen(true); }} aria-label={t("最近会话")} title={t("最近会话")}><History size={17} /><span>{t("最近会话")}</span>{attention > 0 ? <small aria-label={t("{0} 个任务需要处理", [attention])}>{attention > 99 ? "99+" : attention}</small> : null}</button>
    {open ? createPortal(<dialog ref={dialog} className={`cloud-tasks-dialog ${taskOpen ? "" : "recent-conversations-dialog"}`} aria-label={t(taskOpen ? "任务状态" : "最近会话")} onCancel={event => { event.preventDefault(); close(); }}>
      <header>{taskOpen ? <button type="button" className="recent-conversations-back" onClick={() => setView("recent")} aria-label={t("返回最近会话")}><ArrowLeft size={18} /></button> : null}<div><h2>{t(taskOpen ? "任务状态" : "最近会话")}</h2>{taskOpen ? <p>{t("每个会话的最近任务")}</p> : null}</div>{taskOpen ? <button type="button" onClick={() => setAttempt(value => value + 1)} aria-label={t("刷新任务")} disabled={loading || more}><RefreshCw size={17} /></button> : <button type="button" className="recent-task-status" onClick={() => setView("tasks")} aria-label={t("查看任务状态")}><Activity size={15} /><span>{t("任务状态")}</span></button>}<button type="button" onClick={close} aria-label={t("关闭会话面板")}><X size={19} /></button></header>
      {!taskOpen ? <RecentConversationList conversations={conversations} projects={projects} currentId={currentId} unread={unread} onOpen={id => { close(); onOpen(id); }} /> : <>
      <div className="cloud-task-filters" role="group" aria-label={t("筛选任务")}>{(Object.keys(filters) as (keyof typeof filters)[]).map(value => <button type="button" key={value} aria-pressed={filter === value} onClick={() => { morePending.current = false; setMore(false); setFilter(value); }}>{t(filters[value])}</button>)}</div>
      <div className="cloud-task-list" aria-busy={loading}>
        {error ? <p role="alert">{t("任务列表暂不可用")} <button type="button" onClick={() => setAttempt(value => value + 1)}>{t("重试")}</button></p> : null}
        {!current && loading ? <p role="status">{t("正在加载…")}</p> : null}
        {current?.tasks.map(task => <article key={task.run_id} className="cloud-task-card">
          <div className="cloud-task-heading"><span className={`cloud-task-state ${task.state}`}>{t(states[task.state] || "状态尚未确认")}</span><span>{stamp(task.started_at)}</span></div>
          <button type="button" className="cloud-task-open" onClick={() => { close(); onOpen(task.conversation_id); }}><strong>{task.title}</strong><ArrowUpRight size={16} /></button>
          <p className="cloud-task-meta">{task.project} · {task.agent || "Lester"}</p>
          <div className="cloud-task-facts"><span>{t("最近活动：{0}", [stamp(task.last_activity_at)])}</span>{task.queued > 0 ? <span>{t("{0} 条排队或暂缓任务", [task.queued])}</span> : null}{task.background_commands > 0 ? <span>{t("{0} 个会话后台命令运行中", [task.background_commands])}</span> : null}{task.unknown_commands > 0 ? <span>{t("{0} 个后台命令状态待确认", [task.unknown_commands])}</span> : null}{task.deliverables > 0 ? <span>{t("{0} 项已登记成果", [task.deliverables])}</span> : null}</div>
          <details className="cloud-task-usage"><summary>{t("耗时与模型用量")}</summary><UsageDetails task={task} revision={revision + attempt} /></details>
        </article>)}
        {current && !current.tasks.length && !loading && !error ? <p className="cloud-task-empty">{t("当前筛选下没有任务")}</p> : null}
        {current?.next_cursor ? <button type="button" className="cloud-task-more" disabled={loading || more} onClick={() => void loadMore()}>{t(more ? "正在加载…" : "加载更多")}</button> : null}
      </div>
      <footer><span>{t("查看任务不会启动执行；需要确认时请打开会话。")}</span><Link href="/app/settings/automations">{t("管理自动任务")}</Link></footer>
      </>}
    </dialog>, document.body) : null}
  </>;
}

function UsageDetails({ task, revision }: { task: Task; revision: number }) {
  const t = useT(), { locale } = useI18n();
  const [visible, setVisible] = useState(false), [usage, setUsage] = useState<Usage | null>(null), [error, setError] = useState(false), [attempt, setAttempt] = useState(0);
  const node = useRef<HTMLDivElement>(null);
  useEffect(() => { const details = node.current?.closest("details"); if (!details) return; const toggle = () => setVisible(details.open); details.addEventListener("toggle", toggle); return () => details.removeEventListener("toggle", toggle); }, []);
  useEffect(() => {
    if (!visible) return;
    const controller = new AbortController(); let pending = false;
    const load = async () => {
      if (pending || document.hidden) return; pending = true;
      try { const value = await api<Usage>(`/api/v1/conversations/${task.conversation_id}/runs/${task.run_id}/usage`, { signal: controller.signal }); if (!controller.signal.aborted) { setUsage(value); setError(false); } }
      catch { if (!controller.signal.aborted) setError(true); } finally { pending = false; }
    };
    void load(); const timer = task.state === "completed" || task.state === "failed" || task.state === "cancelled" ? undefined : window.setInterval(load, 15000); return () => { controller.abort(); clearInterval(timer); };
  }, [visible, task.conversation_id, task.run_id, task.state, revision, attempt]);
  const number = (value: number) => new Intl.NumberFormat(locale).format(value);
  return <div ref={node} className="cloud-usage-content">
    {error ? <p role="alert">{t(usage ? "用量刷新失败，显示上次记录" : "用量暂不可用")} <button type="button" onClick={() => setAttempt(value => value + 1)}>{t("重试")}</button></p> : null}
    {usage ? <><dl><div><dt>{t("已用时间（含等待）")}</dt><dd>{usage.elapsed_seconds === null ? t("未记录") : t("{0} 分 {1} 秒", [Math.floor(usage.elapsed_seconds / 60), usage.elapsed_seconds % 60])}</dd></div><div><dt>{t("已记录模型请求")}</dt><dd>{number(usage.requests)}{usage.in_flight ? <span> · {t("{0} 个请求进行中", [usage.in_flight])}</span> : null}</dd></div>
      <div><dt>{t("已报告输入 token")}</dt><dd>{usage.actual_input_requests ? number(usage.actual_input_tokens) : t("未报告")}</dd></div><div><dt>{t("已报告输出 token")}</dt><dd>{usage.actual_output_requests ? number(usage.actual_output_tokens) : t("未报告")}</dd></div>
      {usage.estimated_input_requests ? <div><dt>{t("其余请求的输入估算")}</dt><dd>{number(usage.estimated_input_tokens)} · {t("{0} 个请求", [usage.estimated_input_requests])}</dd></div> : null}</dl>
      <p>{t("统计更新于 {0}", [new Intl.DateTimeFormat(locale, { hour: "2-digit", minute: "2-digit", second: "2-digit" }).format(new Date(usage.observed_at))])}</p><p>{t("实测与估算分开统计，缓存输入只计一次；包含上下文整理请求，不换算费用。")}</p>{usage.missing_output_requests ? <p>{t("{0} 个请求缺少已报告的输出用量，未计为零。", [usage.missing_output_requests])}</p> : null}
    </> : !error ? <p>{t("正在加载…")}</p> : null}
  </div>;
}
