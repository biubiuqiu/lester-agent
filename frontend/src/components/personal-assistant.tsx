"use client";

import { useCallback, useEffect, useRef, useState, type FormEvent } from "react";
import Link from "next/link";
import { ArrowUp, ArrowUpRight, Brain, Check, LoaderCircle, Pencil, Trash2, Undo2, X, SlidersHorizontal, Minus } from "lucide-react";
import { api, ApiError, type Deployment, type Project, type UserProfile } from "@/lib/api";
import { readView, updateView, viewKey } from "@/lib/conversation-view-state";
import { mergePersonalTurns, readPersonal, type PersonalMemory, type PersonalState, type PersonalTask, type PersonalTurn } from "@/lib/personal";
import { randomId } from "@/lib/random-id";
import { useI18n } from "./i18n";
import { MessageContent } from "./message-content";

export function PersonalAssistant({ user, projects, deployments, revision, active, onClose, onActivity, onDelegated }: { user: UserProfile; projects: Project[]; deployments: Deployment[]; revision: number; active: boolean; onClose: () => void; onActivity: (busy: boolean) => void; onDelegated: () => void }) {
  const { t, locale } = useI18n();
  const key = viewKey(user.user_id, user.workspace_id, "personal");
  const [text, setText] = useState(() => readView(key).text);
  const [project, setProject] = useState(() => readView(key).personalProject);
  const [model, setModel] = useState(() => readView(key).personalModel || deployments.find(item => item.is_default)?.id || deployments[0]?.id || "");
  const selectedModel = deployments.some(item => item.id === model) ? model : deployments.find(item => item.is_default)?.id || deployments[0]?.id || "";
  const selectedProject = projects.some(item => item.id === project) ? project : "";
  const [state, setState] = useState<PersonalState | null>(null);
  const [error, setError] = useState("");
  const [pending, setPending] = useState(() => readView(key).personalPending);
  const [busy, setBusy] = useState(false), [checking, setChecking] = useState(false), [historyBusy, setHistoryBusy] = useState(false);
  const [memoryOpen, setMemoryOpen] = useState(false);
  const mounted = useRef(true), sending = useRef(false), scroll = useRef<HTMLDivElement>(null), follow = useRef(true), input = useRef<HTMLTextAreaElement>(null);
  const loadedRevision = useRef(revision);
  const readGeneration = useRef(0);
  const activeRef = useRef(active);
  useEffect(() => { onActivity(busy); }, [busy, onActivity]);
  useEffect(() => {
    activeRef.current = active;
    if (!active) return;
    input.current?.focus({ preventScroll: true });
    if (follow.current && scroll.current) scroll.current.scrollTop = scroll.current.scrollHeight;
  }, [active]);
  useEffect(() => { mounted.current = true; return () => { mounted.current = false; }; }, []);
  const draft = (value: string) => { setText(value); updateView(key, { text: value }); };
  const refresh = useCallback(async (signal?: AbortSignal) => {
    const generation = ++readGeneration.current;
    const next = await readPersonal(undefined, signal);
    if (mounted.current && !signal?.aborted && generation === readGeneration.current) setState(previous => ({ ...next, turns: mergePersonalTurns(previous?.turns ?? [], next.turns), has_more: previous && previous.turns.length > next.turns.length ? previous.has_more : next.has_more }));
  }, []);
  useEffect(() => {
    const controller = new AbortController();
    void refresh(controller.signal).catch(reason => { if (!controller.signal.aborted) setError(reason instanceof Error ? reason.message : "个人助理加载失败，请稍后检查状态"); });
    return () => controller.abort();
  }, [refresh]);
  useEffect(() => {
    if (loadedRevision.current === revision) return;
    loadedRevision.current = revision;
    const controller = new AbortController();
    void api<{ tasks: PersonalTask[] }>("/api/v1/personal/tasks", { signal: controller.signal }).then(next => { if (!controller.signal.aborted) setState(previous => previous ? { ...previous, tasks: next.tasks } : previous); }).catch(() => { if (!controller.signal.aborted) setError("任务进展同步失败，请检查状态"); });
    return () => controller.abort();
  }, [revision]);
  useEffect(() => { if (follow.current && scroll.current) scroll.current.scrollTop = scroll.current.scrollHeight; }, [state?.turns, busy]);
  useEffect(() => {
    const sync = (event: StorageEvent) => { if (event.key === `lester.draft.v1.${key}`) { const saved = readView(key); setText(saved.text); setPending(saved.personalPending); setProject(saved.personalProject); setModel(saved.personalModel); } };
    window.addEventListener("storage", sync); return () => window.removeEventListener("storage", sync);
  }, [key]);
  useEffect(() => {
    const reload = () => { if (activeRef.current && document.visibilityState === "visible") void refresh().catch(() => { if (mounted.current) setError("任务进展同步失败，请检查状态"); }); };
    window.addEventListener("focus", reload); document.addEventListener("visibilitychange", reload);
    return () => { window.removeEventListener("focus", reload); document.removeEventListener("visibilitychange", reload); };
  }, [refresh]);
  const accept = (turn: PersonalTurn) => {
    if (!mounted.current) return;
    readGeneration.current++;
    setState(previous => previous ? { ...previous, turns: mergePersonalTurns(previous.turns, [turn]) } : previous);
    if (turn.status !== "pending" && readView(key).personalPending === turn.id) {
      updateView(key, { personalPending: "" }); setPending("");
      if (turn.status === "completed" && readView(key).text.trim() === turn.content.trim()) { updateView(key, { text: "" }); setText(""); }
    }
  };
  async function check() {
    if (checking || !pending) return;
    setChecking(true); setError("");
    try {
      const turn = await api<PersonalTurn>(`/api/v1/personal/messages/${pending}`);
      accept(turn);
      if (turn.status === "pending") setError("Lester 仍在回复，可以稍后检查状态。");
      else { await refresh(); onDelegated(); }
    } catch (reason) {
      if (!mounted.current) return;
      if (reason instanceof ApiError && reason.status === 404) { updateView(key, { personalPending: "" }); setPending(""); setError("服务器未收到这条消息，草稿已保留，请重新发送。"); }
      else setError(reason instanceof Error ? reason.message : "个人助理加载失败，请稍后检查状态");
    } finally { if (mounted.current) setChecking(false); }
  }
  async function submit(event: FormEvent) {
    event.preventDefault();
    if (sending.current || pending || !state || !text.trim() || !deployments.length) return;
    sending.current = true; setBusy(true); setError(""); follow.current = true;
    const id = randomId();
    updateView(key, { personalPending: id }); setPending(id);
    try {
      const turn = await api<PersonalTurn>("/api/v1/personal/messages", { method: "POST", body: JSON.stringify({ id, content: text.trim(), model_id: selectedModel, project_id: selectedProject || null }) });
      accept(turn);
      try { await refresh(); if (turn.task) onDelegated(); }
      catch { if (mounted.current) setError("消息已保存，列表刷新失败，请检查状态。"); }
    } catch (reason) {
      if (mounted.current) {
        // Only reads recover uncertain acceptance. Never POST automatically.
        try {
          const turn = await api<PersonalTurn>(`/api/v1/personal/messages/${id}`); accept(turn);
          if (turn.status === "completed") {
            try { await refresh(); if (mounted.current && turn.task) onDelegated(); }
            catch { if (mounted.current) setError("消息已保存，列表刷新失败，请检查状态。"); }
            return;
          }
        }
        catch (readError) { if (readError instanceof ApiError && readError.status === 404) { updateView(key, { personalPending: "" }); setPending(""); } }
        setError(reason instanceof Error ? reason.message : "个人助理暂时无法回复，本次未执行任务");
      }
    } finally { sending.current = false; if (mounted.current) { setBusy(false); if (activeRef.current && (document.activeElement === document.body || input.current?.closest(".personal-assistant")?.contains(document.activeElement))) input.current?.focus({ preventScroll: true }); } }
  }
  async function older() {
    if (!state?.turns.length || historyBusy) return;
    setHistoryBusy(true); setError(""); const node = scroll.current, oldHeight = node?.scrollHeight ?? 0;
    try { const next = await readPersonal(state.turns[0].seq); if (mounted.current) { follow.current = false; setState(previous => previous ? { ...previous, turns: mergePersonalTurns(next.turns, previous.turns), has_more: next.has_more } : next); requestAnimationFrame(() => { if (node) node.scrollTop += node.scrollHeight - oldHeight; }); } }
    catch (reason) { if (mounted.current) setError(reason instanceof Error ? reason.message : "加载失败"); }
    finally { if (mounted.current) setHistoryBusy(false); }
  }
  async function undo(id: string) {
    await api(`/api/v1/personal/changes/${id}/undo`, { method: "POST", body: "{}" });
    setState(previous => previous ? { ...previous, turns: previous.turns.map(turn => ({ ...turn, changes: turn.changes.map(change => change.id === id ? { ...change, undone: true } : change) })) } : previous);
    await refresh();
  }
  return <div className="personal-assistant" onClick={event => { if (event.target instanceof Element && event.target.closest('a[href^="/app/"]') && window.matchMedia("(max-width: 1279px)").matches) onClose(); }}>
    <div className="personal-toolbar"><div className="personal-identity"><span className="personal-identity-mark" aria-hidden="true">L</span><span><strong>Lester</strong><small>{t("个人助理")}</small></span></div><div className="personal-header-actions"><button type="button" onClick={() => setMemoryOpen(true)} disabled={!state} aria-label={t("管理记忆")}><Brain size={16} />{t("记忆")}{state?.memories.length ? <small>{state.memories.length}</small> : null}</button>
      <details className="personal-options" onBlur={event => { if (!event.currentTarget.contains(event.relatedTarget as Node | null)) event.currentTarget.open = false; }} onKeyDown={event => { if (event.key === "Escape" && event.currentTarget.open) { event.stopPropagation(); event.preventDefault(); event.currentTarget.open = false; event.currentTarget.querySelector("summary")?.focus(); } }}><summary aria-label={t("助理设置")} title={t("助理设置")}><SlidersHorizontal size={16} /></summary><div>
        <label>{t("任务项目")}<select aria-label={t("对话项目范围")} value={selectedProject} onChange={event => { setProject(event.target.value); updateView(key, { personalProject: event.target.value }); }} disabled={busy}><option value="">{t("由 Lester 选择")}</option>{projects.map(item => <option key={item.id} value={item.id}>{item.name}</option>)}</select></label>
        <label>{t("个人助理模型")}<select aria-label={t("个人助理模型")} value={selectedModel} disabled={busy || !deployments.length} onChange={event => { setModel(event.target.value); updateView(key, { personalModel: event.target.value }); }}>{deployments.map(item => <option key={item.id} value={item.id}>{item.name}</option>)}</select></label>
      </div></details><button type="button" aria-label={t("收起个人助理")} title={t("收起个人助理")} onClick={onClose}><Minus size={17} /></button></div></div>
    <div className="personal-scroll" ref={scroll} onScroll={event => { const node = event.currentTarget; follow.current = node.scrollHeight - node.scrollTop - node.clientHeight < 80; }}>
      <div className="personal-transcript" aria-busy={busy}>
        {state?.has_more ? <button type="button" className="personal-history" disabled={historyBusy} onClick={() => void older()}>{t(historyBusy ? "正在载入…" : "查看更早的对话")}</button> : null}
        {!state && !error ? <p role="status">{t("正在载入…")}</p> : null}
        {state && !state.turns.length ? <div className="personal-welcome"><span className="personal-monogram" aria-hidden="true">L</span><h2>{t("随时接着聊。")}</h2><p>{t("我会记住重要的事，帮你安排接下来的工作。")}</p></div> : null}
        {state?.turns.map(turn => <article className="personal-turn" id={`personal-${turn.id}`} key={turn.id}>
          <div className="personal-user">{turn.content}</div>
          {turn.reply ? <div className="personal-reply"><MessageContent content={turn.reply} /></div> : null}
          {turn.status === "failed" || turn.status === "interrupted" ? <p className="personal-turn-notice" role="status">{t("这次回复未完成，未自动重试。你可以重新发送。")}</p> : turn.status === "pending" ? <p className="personal-turn-notice">{t("Lester 正在回复…")}</p> : null}
          {turn.memory_notice ? <p className="personal-turn-notice" role="status">{t(turn.memory_notice)}</p> : null}
          {turn.changes.length ? <div className="personal-memory-receipts">{turn.changes.map(change => <MemoryReceipt key={change.id} change={change} onUndo={() => undo(change.id)} />)}</div> : null}
          {turn.task ? <TaskCard delegation={turn.task} live={state.tasks.find(task => task.id === turn.task!.conversation_id)} projectName={projects.find(item => item.id === turn.task!.project_id)?.name} onInspect={async () => { const receipt = await api<PersonalTurn>(`/api/v1/personal/messages/${turn.id}`); accept(receipt); await refresh(); if (mounted.current && receipt.task) onDelegated(); }} /> : null}
        </article>)}
        {busy ? <p className="personal-working" role="status"><LoaderCircle size={15} />{t("Lester 正在回复…")}</p> : null}
        {state?.tasks.length ? <details className="personal-recent"><summary>{t("最近的工作")}<span>{state.tasks.length}</span></summary><ul>{state.tasks.slice(0, 8).map(task => <li key={task.id}><Link href={`/app/c/${task.id}`}><span><strong>{task.title}</strong><small>{task.project_name}</small></span><span>{t(taskStatus(task.status))}<ArrowUpRight size={14} /></span></Link></li>)}</ul></details> : null}
      </div>
    </div>
    <div className="personal-composer-wrap">
      {error ? <p className="personal-error" role="alert">{t(error)}<button type="button" disabled={checking || busy} onClick={() => pending ? void check() : void refresh().then(() => setError("")).catch(reason => setError(reason.message))}>{t("检查状态")}</button></p> : null}
      {pending && !busy ? <p className="personal-pending" role="status">{t("这条消息的回复状态待确认。")}<button type="button" disabled={checking} onClick={() => void check()}>{t("检查状态")}</button></p> : null}
      {!deployments.length ? <p className="personal-setup"><Link href="/app/settings/models?returnTo=%2Fapp">{t("配置第一个模型")}</Link></p> : null}
      <form className="personal-composer" onSubmit={submit}>
        <textarea ref={input} rows={2} maxLength={12000} aria-label={t("个人助理消息")} placeholder={t("聊聊你的想法，或告诉我需要安排什么…")} value={text} onChange={event => draft(event.target.value)} onKeyDown={event => { if (event.key === "Enter" && !event.shiftKey && !event.nativeEvent.isComposing) { event.preventDefault(); event.currentTarget.form?.requestSubmit(); } }} />
        <div className="personal-composer-actions"><span className="personal-composer-hint">{t("记住重要的事，安排接下来的工作")}</span><span className="personal-composer-spacer" /><button className="send-button" aria-label={t("发送给个人助理")} disabled={!text.trim() || busy || !!pending || !state || !deployments.length}>{busy ? <LoaderCircle className="personal-spinner" /> : <ArrowUp />}</button></div>
      </form>
    </div>
    {memoryOpen && state ? <MemoryManager memories={state.memories} projects={projects} automatic={state.auto_memory} locale={locale} onClose={() => setMemoryOpen(false)} onRefresh={refresh} onAutomatic={async enabled => { await api("/api/v1/personal/settings", { method: "PATCH", body: JSON.stringify({ auto_memory: enabled }) }); setState(previous => previous ? { ...previous, auto_memory: enabled } : previous); }} /> : null}
  </div>;
}

function MemoryReceipt({ change, onUndo }: { change: PersonalTurn["changes"][number]; onUndo: () => Promise<void> }) {
  const { t } = useI18n(), [busy, setBusy] = useState(false), [error, setError] = useState("");
  return <span className="personal-memory-receipt"><Brain size={13} /><span>{t(change.undone ? "已撤销记忆更新" : change.deleted ? "已忘记：{0}" : "已记住：{0}", [change.topic])}</span>{!change.undone ? <button type="button" disabled={busy} aria-label={t("撤销记忆 {0}", [change.topic])} title={t("撤销")} onClick={async () => { if (busy) return; setBusy(true); setError(""); try { await onUndo(); } catch (reason) { setError(reason instanceof Error ? reason.message : "操作失败"); } finally { setBusy(false); } }}><Undo2 size={13} /></button> : null}{error ? <span role="alert">{t(error)}</span> : null}</span>;
}
function taskStatus(status: string) { return ({ running: "进行中", cancelling: "正在停止", completed: "已完成", awaiting_input: "等待你确认", failed: "执行失败", cancelled: "已停止", idle: "尚未开始" } as Record<string, string>)[status] ?? "状态待确认"; }
function TaskCard({ delegation, live, projectName, onInspect }: { delegation: NonNullable<PersonalTurn["task"]>; live?: PersonalTask; projectName?: string; onInspect: () => Promise<void> }) {
  const { t } = useI18n();
  const [checking, setChecking] = useState(false), [error, setError] = useState("");
  const confirmed = delegation.status === "started" || delegation.status === "queued";
  return <div className="personal-task-card"><span className="personal-task-icon"><ArrowUpRight size={18} /></span><div><strong>{delegation.title}</strong><small>{live?.project_name ?? projectName ?? t("项目不可用")} · {t(live?.status === "awaiting_input" ? "等待你确认" : delegation.status === "queued" ? "已加入会话队列" : delegation.status === "failed" ? "任务未接受" : !confirmed ? "委派结果待确认" : live ? taskStatus(live.status) : "已交给任务会话")}</small>{confirmed && live?.reply && live.status === "completed" ? <details><summary>{t("查看最新回复")}</summary><p>{live.reply}</p></details> : null}</div>{confirmed ? <Link href={`/app/c/${delegation.conversation_id}`} aria-label={t("打开任务 {0}", [delegation.title])}>{t("查看任务")}<ArrowUpRight size={14} /></Link> : delegation.status !== "failed" ? <button type="button" className="personal-task-inspect" disabled={checking} onClick={async () => { if (checking) return; setChecking(true); setError(""); try { await onInspect(); } catch (reason) { setError(reason instanceof Error ? reason.message : "任务进展同步失败，请检查状态"); } finally { setChecking(false); } }}>{t("检查状态")}</button> : null}{error ? <p role="alert">{t(error)}</p> : null}</div>;
}

function MemoryManager({ memories, projects, automatic, locale, onClose, onRefresh, onAutomatic }: { memories: PersonalMemory[]; projects: Project[]; automatic: boolean; locale: string; onClose: () => void; onRefresh: () => Promise<void>; onAutomatic: (enabled: boolean) => Promise<void> }) {
  const { t } = useI18n(), dialog = useRef<HTMLDialogElement>(null);
  const [query, setQuery] = useState(""), [editing, setEditing] = useState(""), [content, setContent] = useState(""), [busy, setBusy] = useState(""), [error, setError] = useState("");
  const [lastChange, setLastChange] = useState("");
  const [automaticValue, setAutomaticValue] = useState(automatic);
  useEffect(() => { const element = dialog.current; element?.showModal(); return () => element?.close(); }, []);
  const close = () => { dialog.current?.close(); onClose(); };
  async function mutate(id: string, operation: () => Promise<unknown>) {
    if (busy) return; setBusy(id); setError("");
    try { const result = await operation(); if (result && typeof result === "object" && "change_id" in result && typeof result.change_id === "string") setLastChange(result.change_id); setEditing(""); try { await onRefresh(); } catch { setError("修改已保存，列表刷新失败，请检查状态。"); } }
    catch (reason) { setError(reason instanceof Error ? reason.message : "操作失败"); }
    finally { setBusy(""); }
  }
  const formatter = new Intl.DateTimeFormat(locale, { dateStyle: "medium" });
  return <dialog className="personal-memory-dialog" ref={dialog} aria-labelledby="personal-memory-title" onCancel={event => { event.preventDefault(); close(); }} onClick={event => { if (event.target === event.currentTarget) { const rect = event.currentTarget.getBoundingClientRect(); if (event.clientX < rect.left || event.clientX > rect.right || event.clientY < rect.top || event.clientY > rect.bottom) close(); } }}>
    <header><div><h2 id="personal-memory-title">{t("Lester 的记忆")}</h2><p>{t("记忆仅供你使用。项目记忆只带入对应项目的任务。")}</p></div><button type="button" className="icon-button" aria-label={t("关闭记忆")} onClick={close}><X size={18} /></button></header>
    <label className="personal-auto-memory"><input type="checkbox" checked={automaticValue} disabled={!!busy} onChange={event => { const enabled = event.target.checked; setAutomaticValue(enabled); void mutate("settings", async () => { try { await onAutomatic(enabled); } catch (reason) { setAutomaticValue(automatic); throw reason; } }); }} /><span>{t("自动记住重要信息")}<small>{t("关闭后仍可明确说“记住这个”。")}</small></span></label>
    <input className="personal-memory-search" type="search" aria-label={t("搜索记忆")} placeholder={t("搜索记忆")} value={query} onChange={event => setQuery(event.target.value)} />
    {error ? <p className="personal-error" role="alert">{t(error)}<button type="button" disabled={!!busy} onClick={() => void onRefresh().then(() => setError("")).catch(reason => setError(reason.message))}>{t("检查状态")}</button></p> : null}
    <div className="personal-memory-list">{memories.filter(memory => `${memory.topic} ${memory.content} ${projects.find(project => project.id === memory.project_id)?.name ?? ""}`.toLocaleLowerCase().includes(query.toLocaleLowerCase())).map(memory => <article key={memory.id}>
      <div className="personal-memory-heading"><strong>{memory.topic}</strong><small>{memory.project_id ? projects.find(project => project.id === memory.project_id)?.name : t(memory.kind === "work" ? "工作脉络" : "个人偏好")}</small></div>
      {editing === memory.id ? <form onSubmit={event => { event.preventDefault(); void mutate(memory.id, () => api(`/api/v1/personal/memories/${memory.id}`, { method: "PATCH", body: JSON.stringify({ revision: memory.revision, content }) })); }}><textarea aria-label={t("编辑记忆内容")} value={content} maxLength={600} onChange={event => setContent(event.target.value)} /><div><button disabled={!!busy || !content.trim()}><Check size={14} />{t("保存")}</button><button type="button" disabled={!!busy} onClick={() => setEditing("")}>{t("取消")}</button></div></form> : <p>{memory.content}</p>}
      <details><summary>{t("记忆来源")} · {formatter.format(new Date(memory.updated_at))}</summary><p>{memory.evidence || t("你手动编辑的记忆")}</p><small>{t(memory.automatic ? "自动提炼，可撤销" : "由你明确记住或更正")}</small></details>
      <div className="personal-memory-actions"><button type="button" disabled={!!busy} onClick={() => { setEditing(memory.id); setContent(memory.content); }}><Pencil size={13} />{t("编辑")}</button><button type="button" disabled={!!busy} onClick={() => void mutate(memory.id, () => api(`/api/v1/personal/memories/${memory.id}`, { method: "PATCH", body: JSON.stringify({ revision: memory.revision, content: "", forget: true }) }))}><Trash2 size={13} />{t("忘记")}</button></div>
    </article>)}{!memories.length ? <p className="personal-memory-empty">{t("还没有记忆。聊天时告诉我“记住这个”就好。")}</p> : null}</div>
    <footer>{lastChange ? <button type="button" disabled={!!busy} onClick={() => void mutate("undo", async () => { await api(`/api/v1/personal/changes/${lastChange}/undo`, { method: "POST", body: "{}" }); setLastChange(""); })}><Undo2 size={13} />{t("撤销这次记忆修改")}</button> : null}{t("忘记不会删除原聊天记录或已委派的任务。")}</footer>
  </dialog>;
}
