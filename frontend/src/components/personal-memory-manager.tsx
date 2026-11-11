"use client";

import { ProductGlyph } from "./product-glyph";
import { useCallback, useEffect, useRef, useState, type ReactNode } from "react";
import { createPortal } from "react-dom";
import { Check, LoaderCircle, Pause, Pencil, Play, Plus, Trash2, Undo2, X } from "lucide-react";
import { api, ApiError, type Project, type UserProfile } from "@/lib/api";
import { createPersonalMemory, editPersonalMemory, forgetPersonalMemory, readPersonalMemories, readPersonalMemory, readPersonalMemoryHistory, setPersonalMemoryAvailability, type PersonalMemory, type PersonalMemoryCollection, type PersonalMemoryHistory, type PersonalMemoryInput, type PersonalMemoryReceipt } from "@/lib/personal";
import { randomId } from "@/lib/random-id";
import { viewStateEpoch } from "@/lib/conversation-view-state";
import { useI18n } from "./i18n";

type Editor = PersonalMemoryInput & { id: string; revision: number; mode: "create" | "edit" };
type Pending = { id: string; revision: number; kind: "create" | "edit" | "forget" | "availability"; input?: PersonalMemoryInput; paused?: boolean };
type Props = { open: boolean; host: HTMLElement | null; revision: number; onClose: () => void; onChanged: () => void };
type EditorProps = { open: boolean; host: HTMLElement | null; memories: PersonalMemory[]; projects: Project[]; automatic: boolean; locale: string; loadError: string; onClose: () => void; onRefresh: () => Promise<boolean>; onAutomatic: (enabled: boolean) => Promise<void>; onChanged: () => void };
const kindLabel = (kind: PersonalMemory["kind"]) => kind === "preference" ? "个人偏好" : kind === "project" ? "项目事实" : "工作脉络";
const sameInput = (memory: PersonalMemory, input: PersonalMemoryInput) => memory.topic === input.topic && memory.content === input.content && memory.kind === input.kind && memory.project_id === input.project_id;
const editorKey = (editor: Editor) => editor.mode === "create" ? "new" : editor.id;
const matchesAttempt = (memory: PersonalMemory, attempt: Pending) => memory.revision > attempt.revision && (attempt.kind === "forget" ? memory.deleted : attempt.kind === "availability" ? !memory.deleted && !!memory.paused === attempt.paused : !memory.deleted && !!attempt.input && sameInput(memory, attempt.input));

/** A user-owned manager, deliberately independent of model availability. */
export function PersonalMemoryManager({ open, host, revision, onClose, onChanged }: Props) {
  const { t, locale } = useI18n();
  const [data, setData] = useState<(PersonalMemoryCollection & { owner: string; projects: Project[] }) | null>(null);
  const [error, setError] = useState("");
  const mounted = useRef(true), generation = useRef(0), requests = useRef(new Set<AbortController>()), close = useRef(onClose);
  useEffect(() => { close.current = onClose; }, [onClose]);
  const refresh = useCallback(async () => {
    const request = ++generation.current, epoch = viewStateEpoch();
    const controller = new AbortController(); requests.current.add(controller);
    try {
      const user = await api<UserProfile>("/api/v1/me", { signal: controller.signal });
      if (!mounted.current || controller.signal.aborted || request !== generation.current || epoch !== viewStateEpoch()) return false;
      const owner = `${user.user_id}:${user.workspace_id}`;
      setData(previous => previous && previous.owner !== owner ? null : previous);
      const [collection, projects] = await Promise.all([readPersonalMemories(controller.signal), api<{ projects: Project[] }>("/api/v1/projects", { signal: controller.signal })]);
      if (!mounted.current || controller.signal.aborted || request !== generation.current || epoch !== viewStateEpoch()) return false;
      if (collection.user_id !== user.user_id || collection.workspace_id !== user.workspace_id) { setData(null); throw new Error("记忆加载失败，请重试。"); }
      setData({ ...collection, projects: projects.projects, owner: `${collection.user_id}:${collection.workspace_id}` }); setError(""); return true;
    } catch (reason) { if (mounted.current && !controller.signal.aborted && request === generation.current && epoch === viewStateEpoch()) setError("记忆加载失败，请重试。"); throw reason; }
    finally { requests.current.delete(controller); }
  }, []);
  useEffect(() => {
    mounted.current = true; const active = requests.current;
    const clear = () => { active.forEach(request => request.abort()); setData(null); setError(""); close.current(); };
    window.addEventListener("lester:session-cleared", clear);
    return () => { mounted.current = false; active.forEach(request => request.abort()); window.removeEventListener("lester:session-cleared", clear); };
  }, []);
  useEffect(() => {
    if (!open && !host) return;
    const timer = window.setTimeout(() => { void refresh().catch(() => {}); }, 0);
    return () => window.clearTimeout(timer);
  }, [open, host, revision, refresh]);
  return data ? <MemoryEditor key={data.owner} open={open} host={host} memories={data.memories} projects={data.projects} automatic={data.auto_memory} locale={locale} loadError={error} onClose={onClose} onRefresh={refresh} onChanged={onChanged} onAutomatic={async enabled => {
    const owner = data.owner, epoch = viewStateEpoch();
    await api("/api/v1/personal/settings", { method: "PATCH", body: JSON.stringify({ auto_memory: enabled }) });
    if (mounted.current && epoch === viewStateEpoch()) { setData(previous => previous?.owner === owner ? { ...previous, auto_memory: enabled } : previous); onChanged(); }
  }} /> : <MemorySurface open={open} host={host} onClose={onClose}><MemoryHeading page={!!host} onClose={onClose} /><p className="personal-memory-notice" role={error ? "alert" : "status"}>{t(error || "正在载入…")}{error ? <button type="button" onClick={() => void refresh().catch(() => {})}>{t("重试")}</button> : null}</p></MemorySurface>;
}

function MemorySurface({ open, host, onClose, children }: { open: boolean; host: HTMLElement | null; onClose: () => void; children: ReactNode }) {
  const dialog = useRef<HTMLDialogElement>(null);
  useEffect(() => {
    const element = dialog.current;
    if (open && !host && !element?.open) element?.showModal();
    else if ((!open || host) && element?.open) element.close();
  }, [open, host]);
  return host ? createPortal(<section className="personal-memory-page" aria-labelledby="personal-memory-title">{children}</section>, host) : <dialog className="personal-memory-dialog" ref={dialog} aria-labelledby="personal-memory-title" onCancel={event => { event.preventDefault(); onClose(); }} onClick={event => { if (event.target === event.currentTarget) { const rect = event.currentTarget.getBoundingClientRect(); if (event.clientX < rect.left || event.clientX > rect.right || event.clientY < rect.top || event.clientY > rect.bottom) onClose(); } }}>{children}</dialog>;
}

function MemoryHeading({ page, onClose, busy = false }: { page: boolean; onClose: () => void; busy?: boolean }) {
  const { t } = useI18n();
  const Heading = page ? "h1" : "h2";
  return <header><div><Heading id="personal-memory-title" tabIndex={-1}><ProductGlyph kind="memory" size={24} />{t("Lester 的记忆")}</Heading><p>{t("记忆仅供你使用。项目记忆只带入对应项目的任务。")}</p></div>{page ? null : <button type="button" className="icon-button" aria-label={t("关闭记忆")} disabled={busy} onClick={onClose}><X size={18} /></button>}</header>;
}

function MemoryEditor({ open, host, memories, projects, automatic, locale, loadError, onClose, onRefresh, onAutomatic, onChanged }: EditorProps) {
  const { t } = useI18n();
  const mounted = useRef(true), running = useRef(false), controller = useRef<AbortController | null>(null);
  const drafts = useRef(new Map<string, Editor>());
  const [query, setQuery] = useState(""), [scope, setScope] = useState("all"), [kind, setKind] = useState("all"), [availability, setAvailability] = useState("all");
  const [editor, setEditor] = useState<Editor | null>(null), [busy, setBusy] = useState("");
  const [error, setError] = useState(""), [notice, setNotice] = useState(""), [lastChange, setLastChange] = useState("");
  const [pending, setPending] = useState<Pending | null>(null), [observed, setObserved] = useState<PersonalMemory | null>(null);
  const [uncertain, setUncertain] = useState(false);
  const mutationLocked = !!pending || uncertain;
  const [overrides, setOverrides] = useState<Record<string, PersonalMemory>>({});
  const [automaticOverride, setAutomaticOverride] = useState<boolean | null>(null);
  const automaticValue = automaticOverride ?? automatic;
  const projectNames = new Map(projects.map(project => [project.id, project.name]));
  const merged = new Map(memories.map(memory => [memory.id, memory]));
  for (const memory of Object.values(overrides)) if (!merged.has(memory.id) || merged.get(memory.id)!.revision < memory.revision) merged.set(memory.id, memory);
  const items = [...merged.values()].filter(memory => !memory.deleted).toSorted((a, b) => b.updated_at.localeCompare(a.updated_at) || a.id.localeCompare(b.id));
  const search = query.trim().toLocaleLowerCase(locale);
  const matching = items.filter(memory => (scope === "all" || (scope === "personal" ? !memory.project_id : memory.project_id === scope)) && (kind === "all" || memory.kind === kind) && (availability === "all" || !!memory.paused === (availability === "paused")) && `${memory.topic} ${memory.content} ${memory.project_id ? projectNames.get(memory.project_id) ?? "" : ""}`.toLocaleLowerCase(locale).includes(search));

  useEffect(() => { mounted.current = true; return () => { mounted.current = false; controller.current?.abort(); }; }, []);
  function close() { if (!running.current) onClose(); }
  function rememberEditor(value: Editor) { drafts.current.set(editorKey(value), value); setEditor(value); }
  function patchEditor(patch: Partial<PersonalMemoryInput>) { if (editor) rememberEditor({ ...editor, ...patch }); }
  function start(memory?: PersonalMemory) {
    if (running.current || mutationLocked) return;
    const key = memory?.id ?? "new";
    const value = drafts.current.get(key) ?? (memory ? { id: memory.id, revision: memory.revision, mode: "edit" as const, topic: memory.topic, content: memory.content, kind: memory.kind, project_id: memory.project_id } : { id: randomId(), revision: 0, mode: "create" as const, topic: "", content: "", kind: "preference" as const, project_id: scope !== "all" && scope !== "personal" ? scope : null });
    rememberEditor(value); setError(""); setNotice("");
  }
  function finishEditor() { if (editor) drafts.current.delete(editorKey(editor)); setEditor(null); }
  async function refresh() {
    try { const accepted = await onRefresh(); if (mounted.current && accepted) setOverrides({}); }
    catch { if (mounted.current) setError("修改已保存，列表刷新失败，请检查状态。"); }
  }
  async function operation(id: string, execute: (signal: AbortSignal) => Promise<void>) {
    if (running.current) return;
    running.current = true; setBusy(id); setError(""); setNotice("");
    const active = new AbortController(); controller.current = active;
    try { await execute(active.signal); }
    catch (reason) { if (mounted.current && !active.signal.aborted) setError(reason instanceof Error ? reason.message : "操作失败"); }
    finally { running.current = false; if (mounted.current) setBusy(""); }
  }
  async function write(attempt: Pending, execute: (signal: AbortSignal) => Promise<PersonalMemoryReceipt>) {
    if (pending) return;
    await operation(attempt.id, async signal => {
      let result: PersonalMemoryReceipt;
      try { result = await execute(signal); }
      catch (reason) {
        if (mounted.current && !signal.aborted && (!(reason instanceof ApiError) || reason.status >= 500)) {
          setPending(attempt); setObserved(null); setError("保存结果待确认，草稿已保留。");
          return;
        }
        throw reason;
      }
      if (!mounted.current || signal.aborted) return;
      // The create receipt can refer to a request accepted before a later edit.
      // Never replace that later state or manufacture an undo receipt on reads.
      if (result.memory) {
        setOverrides(previous => ({ ...previous, [result.memory.id]: result.memory }));
        if (attempt.input && !sameInput(result.memory, attempt.input)) { setError("记忆已有其他更新，已保留你的草稿。"); await refresh(); return; }
      }
      setLastChange(result.change_id);
      if (attempt.kind === "create" || attempt.kind === "edit" || attempt.kind === "forget" && editor?.id === attempt.id) finishEditor();
      if (attempt.kind === "forget") drafts.current.delete(attempt.id);
      setNotice(attempt.kind === "forget" ? "已忘记这条记忆" : "记忆已保存");
      onChanged();
      await refresh();
    });
  }
  async function checkPending() {
    if (!pending) { await operation("refresh", async () => { const accepted = await onRefresh(); if (mounted.current && accepted) { setOverrides({}); setAutomaticOverride(null); setUncertain(false); if (uncertain) onChanged(); } }); return; }
    const attempt = pending;
    await operation("check", async signal => {
      let current: PersonalMemory;
      try { current = await readPersonalMemory(attempt.id, signal); }
      catch (reason) {
        if (reason instanceof ApiError && reason.status === 404) { if (mounted.current) setError("尚未找到保存记录，请稍后检查状态。"); return; }
        throw reason;
      }
      if (!mounted.current || signal.aborted) return;
      setObserved(current); setOverrides(previous => ({ ...previous, [current.id]: current }));
      const matches = matchesAttempt(current, attempt);
      setNotice(matches ? "当前记忆已与本次内容一致。" : "");
      if (!matches) setError("记忆已有其他更新，已保留你的草稿。");
      // A current-state read cannot prove the original mutation receipt.
      // Leave the pending identity until the user explicitly accepts this state.
      try { const accepted = await onRefresh(); if (mounted.current && accepted) setOverrides({}); } catch { if (mounted.current) setError("列表刷新失败，请检查状态。"); }
    });
  }
  function acceptObserved() {
    if (!observed || !pending) return;
    if (pending.input && !observed.deleted && sameInput(observed, pending.input) || pending.kind === "forget" && editor?.id === pending.id) finishEditor();
    if (pending.kind === "forget") drafts.current.delete(pending.id);
    setPending(null); setObserved(null); setLastChange(""); setError(""); setNotice("");
  }
  function loadLatest(memory: PersonalMemory) {
    if (running.current || memory.deleted) return;
    rememberEditor({ id: memory.id, revision: memory.revision, mode: "edit", topic: memory.topic, content: memory.content, kind: memory.kind, project_id: memory.project_id });
    setPending(null); setObserved(null); setError(""); setNotice("");
  }
  const latest = editor ? merged.get(editor.id) : undefined;
  const invalidProject = editor?.kind === "project" && !editor.project_id;
  const validEditor = !!editor?.topic.trim() && !!editor.content.trim() && Array.from(editor.topic.trim()).length <= 80 && Array.from(editor.content.trim()).length <= 600 && !invalidProject && (!editor.project_id || projectNames.has(editor.project_id));
  const observedMatches = !!pending && !!observed && matchesAttempt(observed, pending);

  return <MemorySurface open={open} host={host} onClose={close}>
    <MemoryHeading page={!!host} onClose={close} busy={!!busy} />
    <label className="personal-auto-memory"><input type="checkbox" checked={automaticValue} disabled={!!busy || mutationLocked} onChange={event => { const enabled = event.target.checked; setAutomaticOverride(enabled); void operation("settings", async () => { try { await onAutomatic(enabled); } catch (reason) { if (mounted.current) { if (!(reason instanceof ApiError) || reason.status >= 500) { setUncertain(true); throw new Error("操作结果未确认，请检查状态；不会自动重试。"); } setAutomaticOverride(null); } throw reason; } if (mounted.current) { await refresh(); setAutomaticOverride(null); } }); }} /><span>{t("自动记住重要信息")}<small>{t("关闭后仍可明确说“记住这个”。")}</small></span></label>
    <div className="personal-memory-tools"><input className="personal-memory-search" type="search" aria-label={t("搜索记忆")} placeholder={t("搜索记忆")} value={query} onChange={event => setQuery(event.target.value)} /><button className="personal-memory-add" type="button" disabled={!!busy || mutationLocked} onClick={() => start()}><Plus size={15} />{t("新增记忆")}</button></div>
    <div className="personal-memory-filters"><select aria-label={t("筛选记忆范围")} value={scope} onChange={event => setScope(event.target.value)}><option value="all">{t("全部范围")}</option><option value="personal">{t("个人记忆")}</option>{projects.map(project => <option key={project.id} value={project.id}>{project.name}</option>)}</select><select aria-label={t("筛选记忆分类")} value={kind} onChange={event => setKind(event.target.value)}><option value="all">{t("全部分类")}</option><option value="preference">{t("个人偏好")}</option><option value="project">{t("项目事实")}</option><option value="work">{t("工作脉络")}</option></select><select aria-label={t("筛选记忆状态")} value={availability} onChange={event => setAvailability(event.target.value)}><option value="all">{t("全部状态")}</option><option value="active">{t("启用中")}</option><option value="paused">{t("已暂停")}</option></select></div>
    {error || loadError ? <p className="personal-error" role="alert">{t(error || loadError)}<button type="button" disabled={!!busy} onClick={() => void checkPending()}>{t("检查状态")}</button></p> : null}
    {notice ? <p className="personal-memory-notice" role="status">{t(notice)}</p> : null}
    {pending && !error ? <p className="personal-pending">{t("保存结果待确认，草稿已保留。")}<button type="button" disabled={!!busy} onClick={() => void checkPending()}>{t("检查状态")}</button></p> : null}
    {pending ? <div className="personal-memory-observed">{observedMatches ? <button type="button" disabled={!!busy} onClick={acceptObserved}>{t("保留当前记忆")}</button> : null}<button type="button" disabled={!!busy} onClick={() => { setPending(null); setObserved(null); setError(""); setNotice("草稿已保留"); }}>{t("返回编辑")}</button><small>{t("返回编辑不会撤销已经接受的操作。")}</small></div> : null}
    <div className="personal-memory-list">
      {editor ? <form className="personal-memory-editor" aria-label={t(editor.mode === "create" ? "新增记忆" : "编辑记忆")} onSubmit={event => {
        event.preventDefault(); if (!validEditor || busy || mutationLocked) return;
        const input: PersonalMemoryInput = { topic: editor.topic.trim(), content: editor.content.trim(), kind: editor.kind, project_id: editor.project_id };
        const attempt: Pending = { id: editor.id, revision: editor.revision, kind: editor.mode, input };
        void write(attempt, signal => editor.mode === "create" ? createPersonalMemory(editor.id, input, signal) : editPersonalMemory(editor.id, editor.revision, input, signal));
      }}>
        <div className="personal-memory-editor-title"><strong>{t(editor.mode === "create" ? "新增记忆" : "编辑记忆")}</strong><small>{t("直接添加，不需要调用模型。")}</small></div>
        <fieldset disabled={!!busy || mutationLocked}>
          <label>{t("记忆主题")}<input aria-label={t("记忆主题")} value={editor.topic} onChange={event => patchEditor({ topic: event.target.value })} maxLength={160} autoFocus /></label>
          <label>{t("记忆内容")}<textarea aria-label={t(editor.mode === "edit" ? "编辑记忆内容" : "记忆内容")} value={editor.content} onChange={event => patchEditor({ content: event.target.value })} maxLength={1200} rows={3} /></label>
          <div className="personal-memory-editor-options"><label>{t("记忆分类")}<select aria-label={t("记忆分类")} value={editor.kind} onChange={event => patchEditor({ kind: event.target.value as PersonalMemory["kind"] })}><option value="preference">{t("个人偏好")}</option><option value="project">{t("项目事实")}</option><option value="work">{t("工作脉络")}</option></select></label><label>{t("适用范围")}<select aria-label={t("适用范围")} value={editor.project_id ?? ""} onChange={event => patchEditor({ project_id: event.target.value || null })}><option value="">{t("个人记忆")}</option>{editor.project_id && !projectNames.has(editor.project_id) ? <option value={editor.project_id} disabled>{t("项目不可用")}</option> : null}{projects.map(project => <option key={project.id} value={project.id}>{project.name}</option>)}</select></label></div>
        </fieldset>
        <p className="personal-memory-field-hint">{t(invalidProject ? "项目事实需要选择一个项目。" : "记忆主题最多 80 个字符，内容最多 600 个字符。")}</p>
        <div className="personal-memory-editor-actions">{latest && !latest.deleted && (latest.revision !== editor.revision || pending) ? <button type="button" disabled={!!busy} onClick={() => loadLatest(latest)}>{t("载入最新版本")}</button> : null}<span /><button type="button" disabled={!!busy || mutationLocked} onClick={() => { setEditor(null); setNotice("草稿已保留"); }}>{t("取消")}</button><button className="personal-memory-save" disabled={!!busy || mutationLocked || !validEditor}>{busy === editor.id ? <LoaderCircle size={14} className="personal-spinner" /> : <Check size={14} />}{t("保存")}</button></div>
      </form> : null}
      {matching.map(memory => <article key={memory.id}>
        <div className="personal-memory-heading"><strong>{memory.topic}</strong><small>{memory.paused ? <span className="personal-memory-paused">{t("已暂停")} · </span> : null}{memory.project_id ? projectNames.get(memory.project_id) ?? t("项目不可用") : t("个人范围")}</small></div>
        <p>{memory.content}</p>
        <MemoryDetails memory={memory} locale={locale} projectNames={projectNames} />
        <div className="personal-memory-actions"><button type="button" disabled={!!busy || mutationLocked} onClick={() => start(memory)}><Pencil size={13} />{t("编辑")}</button><button type="button" disabled={!!busy || mutationLocked} title={t("暂停后不再作为新回复或新任务的参考，原记录仍保留。")} onClick={() => void write({ id: memory.id, revision: memory.revision, kind: "availability", paused: !memory.paused }, signal => setPersonalMemoryAvailability(memory.id, memory.revision, !memory.paused, signal))}>{memory.paused ? <Play size={13} /> : <Pause size={13} />}{t(memory.paused ? "恢复使用" : "暂停使用")}</button><button type="button" disabled={!!busy || mutationLocked} onClick={() => void write({ id: memory.id, revision: memory.revision, kind: "forget" }, signal => forgetPersonalMemory(memory.id, memory.revision, signal))}><Trash2 size={13} />{t("忘记")}</button></div>
      </article>)}
      {!matching.length && !editor ? <p className="personal-memory-empty" role="status">{t(items.length ? "没有匹配的记忆，试试其他关键词。" : "还没有记忆。聊天时告诉我“记住这个”就好。")}</p> : null}
    </div>
    <footer>{lastChange ? <button type="button" disabled={!!busy || mutationLocked} onClick={() => void operation("undo", async signal => { try { await api(`/api/v1/personal/changes/${encodeURIComponent(lastChange)}/undo`, { method: "POST", body: "{}", signal }); } catch (reason) { if (mounted.current && !signal.aborted && (!(reason instanceof ApiError) || reason.status >= 500)) { setLastChange(""); setUncertain(true); throw new Error("操作结果未确认，请检查状态；不会自动重试。"); } throw reason; } if (mounted.current) { setLastChange(""); onChanged(); await refresh(); } })}><Undo2 size={13} />{t("撤销这次记忆修改")}</button> : null}{t("忘记不会删除原聊天记录或已委派的任务。")}</footer>
  </MemorySurface>;
}

function MemoryDetails({ memory, locale, projectNames }: { memory: PersonalMemory; locale: string; projectNames: Map<string, string> }) {
  const { t } = useI18n();
  const [history, setHistory] = useState<PersonalMemoryHistory | null>(null), [busy, setBusy] = useState(false), [error, setError] = useState("");
  const mounted = useRef(true), loading = useRef(false), opened = useRef(false), revision = useRef(memory.revision);
  const latest = useRef(memory);
  const reload = useRef<() => Promise<void>>(async () => {});
  useEffect(() => { mounted.current = true; return () => { mounted.current = false; }; }, []);
  const load = useCallback(async (more = false) => {
    if (loading.current) return;
    loading.current = true; setBusy(true); setError("");
    const requestedRevision = latest.current.revision;
    try {
      const result = await readPersonalMemoryHistory(memory.id, more ? history?.changes.at(-1)?.revision : undefined);
      if (mounted.current && latest.current.revision === requestedRevision) { revision.current = requestedRevision; setHistory(previous => more && previous ? { ...result, changes: [...previous.changes, ...result.changes.filter(item => !previous.changes.some(prior => prior.id === item.id))] } : result); }
    } catch { if (mounted.current && latest.current.revision === requestedRevision) setError("历史加载失败，请重试。"); }
    finally { loading.current = false; if (mounted.current) { setBusy(false); if (opened.current && latest.current.revision !== requestedRevision) void reload.current(); } }
  }, [memory.id, history]);
  useEffect(() => { latest.current = memory; }, [memory]);
  useEffect(() => { reload.current = () => load(); }, [load]);
  useEffect(() => { const timer = window.setTimeout(() => { if (opened.current && revision.current !== memory.revision) void reload.current(); }, 0); return () => window.clearTimeout(timer); }, [memory.revision]);
  const formatter = new Intl.DateTimeFormat(locale, { dateStyle: "medium", timeStyle: "short" });
  return <details className="personal-memory-details" onToggle={event => { opened.current = event.currentTarget.open; if (opened.current && (!history || revision.current !== memory.revision)) void load(); }}>
    <summary>{t("记忆来源")} · {t("修改历史")}</summary>
    <p>{memory.source_turn_id && memory.evidence ? memory.evidence : t("手动添加或编辑")}</p>
    <small>{t(kindLabel(memory.kind))} · {t("版本 {0}", [memory.revision])}{memory.source_turn_id ? <> · {t(memory.automatic ? "自动提炼，可撤销" : "由你明确记住或更正")}</> : null}</small>
    {memory.paused ? <p className="personal-memory-pause-note">{t("暂停后不再作为新回复或新任务的参考，原记录仍保留。")}</p> : null}
    <div className="personal-memory-history" aria-busy={busy}>
      {error ? <p role="alert">{t(error)} <button type="button" disabled={busy} onClick={() => void load()}>{t("重试")}</button></p> : null}
      {history?.changes.map(change => <div className="personal-memory-version" key={change.id}><div><strong>{t("版本 {0}", [change.revision])}</strong><time dateTime={change.created_at}>{formatter.format(new Date(change.created_at))}</time></div><small>{change.undo_of ? t("撤销了先前的修改") : change.after.deleted ? t("已忘记") : change.before && !!change.before.paused !== !!change.after.paused ? t(change.after.paused ? "由你暂停" : "由你恢复") : t("已保存的修改")}{change.undone ? <> · {t("已撤销记忆更新")}</> : null} · {change.after.project_id ? projectNames.get(change.after.project_id) ?? t("项目不可用") : t("个人范围")}</small><strong className="personal-memory-history-topic">{change.after.topic}</strong>{change.before && change.before.content !== change.after.content ? <p><span>{t("更新前")}</span>{change.before.content}</p> : null}{!change.after.deleted ? <p><span>{t("更新后")}</span>{change.after.content}</p> : null}</div>)}
      {busy ? <p role="status">{t("正在载入…")}</p> : history && !history.changes.length ? <p>{t("暂无已保存的修改记录。")}</p> : null}
      {history?.has_more ? <button type="button" disabled={busy} onClick={() => void load(true)}>{t("加载更多历史")}</button> : null}
    </div>
  </details>;
}
