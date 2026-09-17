"use client";

import { FormEvent, useEffect, useRef, useState } from "react";
import { Pencil, Plus, Search, Users, X } from "lucide-react";
import { api, type UserProfile } from "@/lib/api";

type ManagedUser = { id: string; email: string; display_name: string; role: "member" | "admin"; disabled: boolean; created_at: string };
export default function UsersPage() {
  const [users, setUsers] = useState<ManagedUser[]>([]);
  const [self, setSelf] = useState("");
  const [query, setQuery] = useState("");
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");
  const [message, setMessage] = useState("");
  const [editing, setEditing] = useState<ManagedUser | "new" | null>(null);
  const [busy, setBusy] = useState(false);
  const pending = useRef(false);
  const dialog = useRef<HTMLDialogElement>(null);
  const trigger = useRef<HTMLElement | null>(null);
  async function load() {
    const result = await api<{ users: ManagedUser[] }>("/api/v1/admin/users"); setUsers(result.users);
  }
  useEffect(() => {
    let active = true;
    Promise.all([api<{ users: ManagedUser[] }>("/api/v1/admin/users"), api<UserProfile>("/api/v1/me")]).then(([result, profile]) => {
      if (active) { setUsers(result.users); setSelf(profile.user_id); }
    }).catch((reason: unknown) => { if (active) setError(reason instanceof Error ? reason.message : "加载失败"); }).finally(() => { if (active) setLoading(false); });
    return () => { active = false; };
  }, []);
  useEffect(() => { if (editing) dialog.current?.showModal(); }, [editing]);
  function edit(user: ManagedUser | "new") { trigger.current = document.activeElement as HTMLElement; setError(""); setMessage(""); setEditing(user); }
  function close() { if (pending.current) return; dialog.current?.close(); setEditing(null); trigger.current?.focus(); }
  async function save(event: FormEvent<HTMLFormElement>) {
    event.preventDefault(); if (pending.current || !editing) return;
    const data = new FormData(event.currentTarget); const creating = editing === "new";
    pending.current = true; setBusy(true); setError("");
    try {
      await api(`/api/v1/admin/users${creating ? "" : `/${editing.id}`}`, { method: creating ? "POST" : "PATCH", body: JSON.stringify({
        ...(creating ? { email: data.get("email") } : {}), display_name: data.get("name"), password: data.get("password"),
        role: data.get("role") || (creating ? "member" : editing.role), disabled: creating ? false : editing.id === self ? false : data.get("disabled") === "on",
      }) });
      pending.current = false; close(); setMessage(creating ? "账号已创建，可使用设置的密码登录。" : "账号已更新。");
      await load();
    } catch (reason) { setError(reason instanceof Error ? reason.message : "保存失败"); }
    finally { pending.current = false; setBusy(false); }
  }
  const selected = editing && editing !== "new" ? editing : null;
  const filtered = users.filter((u) => `${u.display_name} ${u.email}`.toLowerCase().includes(query.trim().toLowerCase()));
  return <>
    <header className="settings-heading"><div><p className="eyebrow">Administration / People</p><h1>人员管理</h1><p>管理成员账号、管理员权限与访问状态。</p></div><button className="primary-button" onClick={() => edit("new")}><Plus size={16} />新增人员</button></header>
    <div className="admin-summary"><Users size={20} /><strong>{users.length}</strong><span>位成员</span><span className="admin-summary-detail">{users.filter((u) => u.role === "admin" && !u.disabled).length} 位管理员 · {users.filter((u) => u.disabled).length} 位已停用</span></div>
    {message && <p className="success-banner" role="status">{message}</p>}
    {error && !editing && <p className="settings-error" role="alert">{error}<button onClick={() => { setError(""); void load().catch((e: Error) => setError(e.message)); }}>重试</button></p>}
    <section className="admin-table-card"><label className="admin-search"><Search size={17} /><input aria-label="搜索人员" placeholder="搜索姓名或邮箱…" value={query} onChange={(e) => setQuery(e.target.value)} /></label>
      <div className="admin-table-scroll"><table className="admin-table"><thead><tr><th>成员</th><th>角色</th><th>状态</th><th>加入时间</th><th><span className="sr-only">操作</span></th></tr></thead><tbody>
        {filtered.map((u) => <tr key={u.id}><td><strong>{u.display_name}{u.id === self && <small>（你）</small>}</strong><small>{u.email}</small></td><td>{u.role === "admin" ? "管理员" : "成员"}</td><td><span className={`admin-badge ${u.disabled ? "disabled" : ""}`}>{u.disabled ? "已停用" : "正常"}</span></td><td>{new Date(u.created_at).toLocaleDateString("zh-CN")}</td><td><button className="admin-edit" onClick={() => edit(u)} aria-label={`编辑 ${u.display_name}`}><Pencil size={14} />编辑</button></td></tr>)}
      </tbody></table></div>
      {!filtered.length && <p className="muted-block">{loading ? "正在加载人员…" : query ? "没有匹配的人员。" : "暂无人员。"}</p>}
    </section>
    <dialog ref={dialog} className="admin-dialog" onCancel={(e) => { e.preventDefault(); close(); }}>
      {editing && <form key={selected?.id || "new"} onSubmit={save}><header><div><p className="eyebrow">Account</p><h2>{selected ? "编辑人员" : "新增人员"}</h2></div><button type="button" aria-label="关闭" disabled={busy} onClick={close}><X size={20} /></button></header>
        {error && <p className="settings-error" role="alert">{error}</p>}
        <label className="field">姓名<input name="name" required maxLength={60} defaultValue={selected?.display_name} autoFocus /></label>
        <label className="field">邮箱<input name="email" type="email" required disabled={!!selected} defaultValue={selected?.email} autoComplete="off" /></label>
        <label className="field">角色<select name="role" defaultValue={selected?.role || "member"} disabled={selected?.id === self}><option value="member">成员</option><option value="admin">管理员</option></select></label>
        <label className="field">{selected ? "重置密码（留空保留原密码）" : "初始密码"}<input name="password" type="password" minLength={10} maxLength={1024} required={!selected} autoComplete="new-password" placeholder="至少 10 个字符" /></label>
        {selected && <label className="check-field"><input type="checkbox" name="disabled" defaultChecked={selected.disabled} disabled={selected.id === self} />停用此账号</label>}
        <p className="admin-help">{selected ? "停用、重置密码或调整角色会使该账号的已有登录失效。" : "新成员会获得独立的工作区和默认项目。请通过安全渠道交付初始密码。"}</p>
        <footer><button type="button" disabled={busy} onClick={close}>取消</button><button className="primary-button" disabled={busy}>{busy ? "保存中…" : "保存"}</button></footer>
      </form>}
    </dialog>
  </>;
}
