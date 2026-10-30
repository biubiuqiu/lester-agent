"use client";

import { useEffect, useRef, useState } from "react";
import { Check, LoaderCircle, Pencil, Plus, Trash2, Undo2, X } from "lucide-react";
import { api, ApiError, type Project } from "@/lib/api";
import { createPersonalMemory, editPersonalMemory, forgetPersonalMemory, readPersonalMemory, type PersonalMemory, type PersonalMemoryInput, type PersonalMemoryReceipt } from "@/lib/personal";
import { randomId } from "@/lib/random-id";
import { useI18n } from "./i18n";

type Editor = PersonalMemoryInput & { id: string; revision: number; mode: "create" | "edit" };
type Pending = { id: string; revision: number; kind: "create" | "edit" | "forget"; input?: PersonalMemoryInput };
type Props = { open: boolean; memories: PersonalMemory[]; projects: Project[]; automatic: boolean; locale: string; onClose: () => void; onRefresh: () => Promise<void>; onAutomatic: (enabled: boolean) => Promise<void> };
const kindLabel = (kind: PersonalMemory["kind"]) => kind === "preference" ? "个人偏好" : kind === "project" ? "项目事实" : "工作脉络";
const sameInput = (memory: PersonalMemory, input: PersonalMemoryInput) => memory.topic === input.topic && memory.content === input.content && memory.kind === input.kind && memory.project_id === input.project_id;
const editorKey = (editor: Editor) => editor.mode === "create" ? "new" : editor.id;

/** A user-owned manager, deliberately independent of model availability. */
export function PersonalMemoryManager({ open, memories, projects, automatic, locale, onClose, onRefresh, onAutomatic }: Props) {
  const { t } = useI18n();
  const dialog = useRef<HTMLDialogElement>(null), mounted = useRef(true), running = useRef(false), controller = useRef<AbortController | null>(null);
  const drafts = useRef(new Map<string, Editor>());
  const [query, setQuery] = useState(""), [scope, setScope] = useState("all"), [kind, setKind] = useState("all");
  const [editor, setEditor] = useState<Editor | null>(null), [busy, setBusy] = useState("");
  const [error, setError] = useState(""), [notice, setNotice] = useState(""), [lastChange, setLastChange] = useState("");
  const [pending, setPending] = useState<Pending | null>(null), [observed, setObserved] = useState<PersonalMemory | null>(null);
  const [overrides, setOverrides] = useState<Record<string, PersonalMemory>>({});
  const [automaticOverride, setAutomaticOverride] = useState<boolean | null>(null);
  const automaticValue = automaticOverride ?? automatic;
  const projectNames = new Map(projects.map(project => [project.id, project.name]));
  const merged = new Map(memories.map(memory => [memory.id, memory]));
  for (const memory of Object.values(overrides)) if (!merged.has(memory.id) || merged.get(memory.id)!.revision < memory.revision) merged.set(memory.id, memory);
  const items = [...merged.values()].filter(memory => !memory.deleted).toSorted((a, b) => b.updated_at.localeCompare(a.updated_at) || a.id.localeCompare(b.id));
  const search = query.trim().toLocaleLowerCase(locale);
  const matching = items.filter(memory => (scope === "all" || (scope === "personal" ? !memory.project_id : memory.project_id === scope)) && (kind === "all" || memory.kind === kind) && `${memory.topic} ${memory.content} ${memory.project_id ? projectNames.get(memory.project_id) ?? "" : ""}`.toLocaleLowerCase(locale).includes(search));
  const formatter = new Intl.DateTimeFormat(locale, { dateStyle: "medium" });

  useEffect(() => { mounted.current = true; return () => { mounted.current = false; controller.current?.abort(); }; }, []);
  useEffect(() => {
    const element = dialog.current;
    if (open && !element?.open) element?.showModal();
    else if (!open && element?.open) element.close();
  }, [open]);

  function close() { if (!running.current) { dialog.current?.close(); onClose(); } }
  function rememberEditor(value: Editor) { drafts.current.set(editorKey(value), value); setEditor(value); }
  function patchEditor(patch: Partial<PersonalMemoryInput>) { if (editor) rememberEditor({ ...editor, ...patch }); }
  function start(memory?: PersonalMemory) {
    if (running.current || pending) return;
    const key = memory?.id ?? "new";
    const value = drafts.current.get(key) ?? (memory ? { id: memory.id, revision: memory.revision, mode: "edit" as const, topic: memory.topic, content: memory.content, kind: memory.kind, project_id: memory.project_id } : { id: randomId(), revision: 0, mode: "create" as const, topic: "", content: "", kind: "preference" as const, project_id: scope !== "all" && scope !== "personal" ? scope : null });
    rememberEditor(value); setError(""); setNotice("");
  }
  function finishEditor() { if (editor) drafts.current.delete(editorKey(editor)); setEditor(null); }
  async function refresh() {
    try { await onRefresh(); if (mounted.current) setOverrides({}); }
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
      if (attempt.kind !== "forget" || editor?.id === attempt.id) finishEditor();
      if (attempt.kind === "forget") drafts.current.delete(attempt.id);
      setNotice(attempt.kind === "forget" ? "已忘记这条记忆" : "记忆已保存");
      await refresh();
    });
  }
  async function checkPending() {
    if (!pending) { await operation("refresh", async () => { await onRefresh(); if (mounted.current) setOverrides({}); }); return; }
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
      const matches = attempt.kind === "forget" ? current.deleted && current.revision > attempt.revision : !current.deleted && !!attempt.input && sameInput(current, attempt.input) && current.revision > attempt.revision;
      setNotice(matches ? "当前记忆已与本次内容一致。" : "");
      if (!matches) setError("记忆已有其他更新，已保留你的草稿。");
      // A current-state read cannot prove the original mutation receipt.
      // Leave the pending identity until the user explicitly accepts this state.
      try { await onRefresh(); if (mounted.current) setOverrides({}); } catch { if (mounted.current) setError("列表刷新失败，请检查状态。"); }
    });
  }
  function acceptObserved() {
    if (!observed || !pending) return;
    if (pending.kind !== "forget" && pending.input && !observed.deleted && sameInput(observed, pending.input) || pending.kind === "forget" && editor?.id === pending.id) finishEditor();
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
  const observedMatches = !!pending && !!observed && (pending.kind === "forget" ? observed.deleted && observed.revision > pending.revision : !!pending.input && !observed.deleted && sameInput(observed, pending.input) && observed.revision > pending.revision);

  return <dialog className="personal-memory-dialog" ref={dialog} aria-labelledby="personal-memory-title" onCancel={event => { event.preventDefault(); close(); }} onClick={event => { if (event.target === event.currentTarget) { const rect = event.currentTarget.getBoundingClientRect(); if (event.clientX < rect.left || event.clientX > rect.right || event.clientY < rect.top || event.clientY > rect.bottom) close(); } }}>
    <header><div><h2 id="personal-memory-title">{t("Lester 的记忆")}</h2><p>{t("记忆仅供你使用。项目记忆只带入对应项目的任务。")}</p></div><button type="button" className="icon-button" aria-label={t("关闭记忆")} disabled={!!busy} onClick={close}><X size={18} /></button></header>
    <label className="personal-auto-memory"><input type="checkbox" checked={automaticValue} disabled={!!busy || !!pending} onChange={event => { const enabled = event.target.checked; setAutomaticOverride(enabled); void operation("settings", async () => { try { await onAutomatic(enabled); } catch (reason) { if (mounted.current) setAutomaticOverride(null); throw reason; } if (mounted.current) { await refresh(); setAutomaticOverride(null); } }); }} /><span>{t("自动记住重要信息")}<small>{t("关闭后仍可明确说“记住这个”。")}</small></span></label>
    <div className="personal-memory-tools"><input className="personal-memory-search" type="search" aria-label={t("搜索记忆")} placeholder={t("搜索记忆")} value={query} onChange={event => setQuery(event.target.value)} /><button className="personal-memory-add" type="button" disabled={!!busy || !!pending} onClick={() => start()}><Plus size={15} />{t("新增记忆")}</button></div>
    <div className="personal-memory-filters"><select aria-label={t("筛选记忆范围")} value={scope} onChange={event => setScope(event.target.value)}><option value="all">{t("全部范围")}</option><option value="personal">{t("个人记忆")}</option>{projects.map(project => <option key={project.id} value={project.id}>{project.name}</option>)}</select><select aria-label={t("筛选记忆分类")} value={kind} onChange={event => setKind(event.target.value)}><option value="all">{t("全部分类")}</option><option value="preference">{t("个人偏好")}</option><option value="project">{t("项目事实")}</option><option value="work">{t("工作脉络")}</option></select></div>
    {error ? <p className="personal-error" role="alert">{t(error)}<button type="button" disabled={!!busy} onClick={() => void checkPending()}>{t("检查状态")}</button></p> : null}
    {notice ? <p className="personal-memory-notice" role="status">{t(notice)}</p> : null}
    {pending && !error ? <p className="personal-pending">{t("保存结果待确认，草稿已保留。")}<button type="button" disabled={!!busy} onClick={() => void checkPending()}>{t("检查状态")}</button></p> : null}
    {pending ? <div className="personal-memory-observed">{observedMatches ? <button type="button" disabled={!!busy} onClick={acceptObserved}>{t("保留当前记忆")}</button> : null}<button type="button" disabled={!!busy} onClick={() => { setPending(null); setObserved(null); setError(""); setNotice("草稿已保留"); }}>{t("返回编辑")}</button><small>{t("返回编辑不会撤销已经接受的操作。")}</small></div> : null}
    <div className="personal-memory-list">
      {editor ? <form className="personal-memory-editor" aria-label={t(editor.mode === "create" ? "新增记忆" : "编辑记忆")} onSubmit={event => {
        event.preventDefault(); if (!validEditor || busy || pending) return;
        const input: PersonalMemoryInput = { topic: editor.topic.trim(), content: editor.content.trim(), kind: editor.kind, project_id: editor.project_id };
        const attempt: Pending = { id: editor.id, revision: editor.revision, kind: editor.mode, input };
        void write(attempt, signal => editor.mode === "create" ? createPersonalMemory(editor.id, input, signal) : editPersonalMemory(editor.id, editor.revision, input, signal));
      }}>
        <div className="personal-memory-editor-title"><strong>{t(editor.mode === "create" ? "新增记忆" : "编辑记忆")}</strong><small>{t("直接添加，不需要调用模型。")}</small></div>
        <fieldset disabled={!!busy || !!pending}>
          <label>{t("记忆主题")}<input aria-label={t("记忆主题")} value={editor.topic} onChange={event => patchEditor({ topic: event.target.value })} maxLength={160} autoFocus /></label>
          <label>{t("记忆内容")}<textarea aria-label={t(editor.mode === "edit" ? "编辑记忆内容" : "记忆内容")} value={editor.content} onChange={event => patchEditor({ content: event.target.value })} maxLength={1200} rows={3} /></label>
          <div className="personal-memory-editor-options"><label>{t("记忆分类")}<select aria-label={t("记忆分类")} value={editor.kind} onChange={event => patchEditor({ kind: event.target.value as PersonalMemory["kind"] })}><option value="preference">{t("个人偏好")}</option><option value="project">{t("项目事实")}</option><option value="work">{t("工作脉络")}</option></select></label><label>{t("适用范围")}<select aria-label={t("适用范围")} value={editor.project_id ?? ""} onChange={event => patchEditor({ project_id: event.target.value || null })}><option value="">{t("个人记忆")}</option>{editor.project_id && !projectNames.has(editor.project_id) ? <option value={editor.project_id} disabled>{t("项目不可用")}</option> : null}{projects.map(project => <option key={project.id} value={project.id}>{project.name}</option>)}</select></label></div>
        </fieldset>
        <p className="personal-memory-field-hint">{t(invalidProject ? "项目事实需要选择一个项目。" : "记忆主题最多 80 个字符，内容最多 600 个字符。")}</p>
        <div className="personal-memory-editor-actions">{latest && !latest.deleted && (latest.revision !== editor.revision || pending) ? <button type="button" disabled={!!busy} onClick={() => loadLatest(latest)}>{t("载入最新版本")}</button> : null}<span /><button type="button" disabled={!!busy || !!pending} onClick={() => { setEditor(null); setNotice("草稿已保留"); }}>{t("取消")}</button><button className="personal-memory-save" disabled={!!busy || !!pending || !validEditor}>{busy === editor.id ? <LoaderCircle size={14} className="personal-spinner" /> : <Check size={14} />}{t("保存")}</button></div>
      </form> : null}
      {matching.map(memory => <article key={memory.id}>
        <div className="personal-memory-heading"><strong>{memory.topic}</strong><small>{memory.project_id ? projectNames.get(memory.project_id) ?? t("项目不可用") : t(kindLabel(memory.kind))}</small></div>
        <p>{memory.content}</p>
        <details><summary>{t("记忆来源")} · {formatter.format(new Date(memory.updated_at))}</summary><p>{memory.source_turn_id && memory.evidence ? memory.evidence : t("手动添加或编辑")}</p>{memory.source_turn_id ? <small>{t(memory.automatic ? "自动提炼，可撤销" : "由你明确记住或更正")}</small> : null}</details>
        <div className="personal-memory-actions"><button type="button" disabled={!!busy || !!pending} onClick={() => start(memory)}><Pencil size={13} />{t("编辑")}</button><button type="button" disabled={!!busy || !!pending} onClick={() => void write({ id: memory.id, revision: memory.revision, kind: "forget" }, signal => forgetPersonalMemory(memory.id, memory.revision, signal))}><Trash2 size={13} />{t("忘记")}</button></div>
      </article>)}
      {!matching.length && !editor ? <p className="personal-memory-empty" role="status">{t(items.length ? "没有匹配的记忆，试试其他关键词。" : "还没有记忆。聊天时告诉我“记住这个”就好。")}</p> : null}
    </div>
    <footer>{lastChange ? <button type="button" disabled={!!busy || !!pending} onClick={() => void operation("undo", async signal => { await api(`/api/v1/personal/changes/${encodeURIComponent(lastChange)}/undo`, { method: "POST", body: "{}", signal }); if (mounted.current) { setLastChange(""); await refresh(); } })}><Undo2 size={13} />{t("撤销这次记忆修改")}</button> : null}{t("忘记不会删除原聊天记录或已委派的任务。")}</footer>
  </dialog>;
}
