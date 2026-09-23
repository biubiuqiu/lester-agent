"use client";

import { FormEvent, useEffect, useRef, useState } from "react";
import Link from "next/link";
import { ArrowLeft, BookOpen, Plus, Search } from "lucide-react";
import { api, type ContextEntry } from "@/lib/api";

export default function ContextLibrary() {
  const [entries, setEntries] = useState<ContextEntry[]>([]);
  const [query, setQuery] = useState(""); const [editing, setEditing] = useState<ContextEntry | "new" | null>(null);
  const [error, setError] = useState(""); const [notice, setNotice] = useState("");
  const [loading, setLoading] = useState(true); const [busy, setBusy] = useState(false);
  const [confirmDelete, setConfirmDelete] = useState(false);
  const [dirty, setDirty] = useState(false);
  const pending = useRef(false); const request = useRef(0);
  function canLeave() { return !dirty || window.confirm("尚有未保存的修改，确定放弃吗？"); }
  useEffect(() => {
    if (!dirty) return;
    const warn = (event: BeforeUnloadEvent) => { event.preventDefault(); };
    window.addEventListener("beforeunload", warn);
    return () => window.removeEventListener("beforeunload", warn);
  }, [dirty]);
  async function load() { const result = await api<{ entries: ContextEntry[] }>("/api/v1/contexts"); setEntries(result.entries); }
  useEffect(() => { let active = true; api<{ entries: ContextEntry[] }>("/api/v1/contexts").then((r) => { if (active) setEntries(r.entries); }).catch((e: unknown) => { if (active) setError(e instanceof Error ? e.message : "加载失败"); }).finally(() => { if (active) setLoading(false); }); return () => { active = false; }; }, []);
  async function open(id: string) {
    if (pending.current || !canLeave()) return;
    setDirty(false);
    const token = ++request.current; setError(""); setNotice(""); setEditing(null); setConfirmDelete(false); setLoading(true);
    try { const entry = await api<ContextEntry>(`/api/v1/contexts/${id}`); if (token === request.current) setEditing(entry); }
    catch (e) { if (token === request.current) setError(e instanceof Error ? e.message : "词条加载失败"); }
    finally { if (token === request.current) setLoading(false); }
  }
  async function save(event: FormEvent<HTMLFormElement>) {
    event.preventDefault(); if (pending.current || !editing) return;
    const data = new FormData(event.currentTarget); const item = editing;
    pending.current = true; setBusy(true); setError(""); setNotice("");
    try {
      const result = await api<ContextEntry>(`/api/v1/contexts${item === "new" ? "" : `/${item.id}`}`, { method: item === "new" ? "POST" : "PATCH", body: JSON.stringify({ title: data.get("title"), description: data.get("description"), content: data.get("content"), version: item === "new" ? 0 : item.version }) });
      setDirty(false); setEditing(result); setNotice("词条已保存。下次引用时将使用最新版本。"); await load();
    } catch (e) { setError(e instanceof Error ? e.message : "保存失败"); }
    finally { pending.current = false; setBusy(false); }
  }
  async function remove() {
    if (pending.current || !editing || editing === "new") return;
    pending.current = true; setBusy(true); setError("");
    try { await api(`/api/v1/contexts/${editing.id}`, { method: "DELETE", body: JSON.stringify({ version: editing.version }) }); setDirty(false); setEditing(null); setConfirmDelete(false); setNotice("词条已删除，历史消息中的引用快照保留。"); await load(); }
    catch (e) { setError(e instanceof Error ? e.message : "删除失败"); }
    finally { pending.current = false; setBusy(false); }
  }
  const selected = editing && editing !== "new" ? editing : null;
  const visible = entries.filter((e) => `${e.title} ${e.description}`.toLowerCase().includes(query.trim().toLowerCase()));
  return <main className="context-library">
    <header className="context-library-header"><Link href="/app" onClick={(e) => { if (busy || !canLeave()) e.preventDefault(); }}><ArrowLeft size={16} />返回工作区</Link><span><BookOpen size={17} />个人上下文库</span></header>
    <section className="context-library-main">
      <div className="settings-heading"><div><p className="eyebrow">Context library</p><h1>把常用背景，留在手边</h1><p>维护术语、项目背景与写作要求。在聊天中输入 @，按需引用。</p></div><button className="primary-button" disabled={busy} onClick={() => { if (!canLeave()) return; setDirty(false); request.current++; setLoading(false); setEditing("new"); setConfirmDelete(false); setError(""); setNotice(""); }}><Plus size={16} />新建词条</button></div>
      {error && <p className="settings-error" role="alert">{error}</p>}{notice && <p className="success-banner" role="status">{notice}</p>}
      <div className="context-library-grid">
        <aside className="context-library-list"><label className="admin-search"><Search size={16} /><input aria-label="搜索上下文" value={query} onChange={(e) => setQuery(e.target.value)} placeholder="搜索名称或简介…" /></label><p className="context-list-count">{entries.length} 个词条 · 仅自己可见</p>
          {visible.map((entry) => <button type="button" key={entry.id} disabled={busy} className={selected?.id === entry.id ? "active" : ""} onClick={() => void open(entry.id)}><strong>{entry.title}</strong><small>{entry.description || "无简介"}</small></button>)}
          {!visible.length && <p className="muted-block">{loading ? "正在加载…" : entries.length ? "没有匹配的词条" : "还没有词条。可以从团队术语、产品介绍或输出格式开始。"}</p>}
        </aside>
        <section className="context-library-editor">
          {editing ? <form key={selected ? `${selected.id}:${selected.version}` : "new"} onSubmit={save} onChange={() => setDirty(true)}>
            <header><h2>{selected ? "编辑词条" : "新建词条"}</h2><small>{selected ? `版本 ${selected.version}` : "支持纯文本与 Markdown"}</small></header>
            <label className="field">名称<input disabled={busy} name="title" defaultValue={selected?.title || ""} required maxLength={80} placeholder="例如：品牌写作规范" /></label>
            <label className="field">简介<input disabled={busy} name="description" defaultValue={selected?.description || ""} maxLength={240} placeholder="用一句话说明何时使用" /></label>
            <label className="field">正文<textarea disabled={busy} name="content" defaultValue={selected?.content || ""} required maxLength={20000} rows={14} placeholder="输入需要提供给 Agent 的背景、术语定义或要求…" /></label>
            <p className="admin-help">发送消息时会保存词条的完整快照。每条正文最多 20,000 字，每次最多引用 8 条，合计最多 40,000 字。</p>
            <footer>{selected && <button type="button" disabled={busy} className="context-delete" onClick={() => setConfirmDelete(true)}>删除词条</button>}<button className="primary-button" disabled={busy}>{busy ? "保存中…" : "保存词条"}</button></footer>
            {confirmDelete && <div className="context-delete-confirm" role="alert"><p>删除“{selected?.title}”？此操作无法撤销，历史消息快照会保留。</p><button type="button" disabled={busy} onClick={() => setConfirmDelete(false)}>取消</button><button type="button" disabled={busy} onClick={() => void remove()}>确认删除</button></div>}
          </form> : <div className="context-library-empty"><BookOpen size={32} /><h2>{loading ? "正在加载…" : "选择一个词条，或新建上下文"}</h2><p>只在聊天中主动引用的词条，才会提供给 Agent。</p></div>}
        </section>
      </div>
    </section>
  </main>;
}
