"use client";
import { T, useI18n } from "@/components/i18n";


import { FormEvent, useEffect, useRef, useState } from "react";
import { Pencil, Plus, Search, Users, X } from "lucide-react";
import { api, type UserProfile } from "@/lib/api";

type ManagedUser = { id: string; email: string; display_name: string; role: "member" | "admin"; disabled: boolean; created_at: string };
export default function UsersPage() {
  const { locale, t } = useI18n();

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
    <header className="settings-heading"><div><p className="eyebrow">Administration / People</p><h1><T>{"人员管理"}</T></h1><p><T>{"管理成员账号、管理员权限与访问状态。"}</T></p></div><button className="primary-button" onClick={() => edit("new")}><Plus size={16} /><T>{"新增人员"}</T></button></header>
    <div className="admin-summary"><Users size={20} /><strong>{users.length}</strong><span><T>{"位成员"}</T></span><span className="admin-summary-detail">{users.filter((u) => u.role === "admin" && !u.disabled).length} <T>{"位管理员 ·"}</T>{users.filter((u) => u.disabled).length} <T>{"位已停用"}</T></span></div>
    {message && <p className="success-banner" role="status">{message}</p>}
    {error && !editing && <p className="settings-error" role="alert">{t(error)}<button onClick={() => { setError(""); void load().catch((e: Error) => setError(e.message)); }}><T>{"重试"}</T></button></p>}
    <section className="admin-table-card"><label className="admin-search"><Search size={17} /><input aria-label={t("搜索人员")} placeholder={t("搜索姓名或邮箱…")} value={query} onChange={(e) => setQuery(e.target.value)} /></label>
      <div className="admin-table-scroll"><table className="admin-table"><thead><tr><th><T>{"成员"}</T></th><th><T>{"角色"}</T></th><th><T>{"状态"}</T></th><th><T>{"加入时间"}</T></th><th><span className="sr-only"><T>{"操作"}</T></span></th></tr></thead><tbody>
        {filtered.map((u) => <tr key={u.id}><td><strong>{u.display_name}{u.id === self && <small><T>{"（你）"}</T></small>}</strong><small>{u.email}</small></td><td>{u.role === "admin" ? t("管理员") : t("成员")}</td><td><span className={`admin-badge ${u.disabled ? "disabled" : ""}`}>{u.disabled ? t("已停用") : t("正常")}</span></td><td>{new Date(u.created_at).toLocaleDateString(locale)}</td><td><button className="admin-edit" onClick={() => edit(u)} aria-label={t("编辑 {0}", [u.display_name])}><Pencil size={14} /><T>{"编辑"}</T></button></td></tr>)}
      </tbody></table></div>
      {!filtered.length && <p className="muted-block">{loading ? t("正在加载人员…") : query ? t("没有匹配的人员。") : t("暂无人员。")}</p>}
    </section>
    <dialog ref={dialog} className="admin-dialog" onCancel={(e) => { e.preventDefault(); close(); }}>
      {editing && <form key={selected?.id || "new"} onSubmit={save}><header><div><p className="eyebrow">Account</p><h2>{selected ? t("编辑人员") : t("新增人员")}</h2></div><button type="button" aria-label={t("关闭")} disabled={busy} onClick={close}><X size={20} /></button></header>
        {error && <p className="settings-error" role="alert">{t(error)}</p>}
        <label className="field"><T>{"姓名"}</T><input name="name" required maxLength={60} defaultValue={selected?.display_name} autoFocus /></label>
        <label className="field"><T>{"邮箱"}</T><input name="email" type="email" required disabled={!!selected} defaultValue={selected?.email} autoComplete="off" /></label>
        <label className="field"><T>{"角色"}</T><select name="role" defaultValue={selected?.role || "member"} disabled={selected?.id === self}><option value="member"><T>{"成员"}</T></option><option value="admin"><T>{"管理员"}</T></option></select></label>
        <label className="field">{selected ? t("重置密码（留空保留原密码）") : t("初始密码")}<input name="password" type="password" minLength={10} maxLength={1024} required={!selected} autoComplete="new-password" placeholder={t("至少 10 个字符")} /></label>
        {selected && <label className="check-field"><input type="checkbox" name="disabled" defaultChecked={selected.disabled} disabled={selected.id === self} /><T>{"停用此账号"}</T></label>}
        <p className="admin-help">{selected ? t("停用、重置密码或调整角色会使该账号的已有登录失效。") : t("新成员会获得独立的工作区和默认项目。请通过安全渠道交付初始密码。")}</p>
        <footer><button type="button" disabled={busy} onClick={close}><T>{"取消"}</T></button><button className="primary-button" disabled={busy}>{busy ? t("保存中…") : t("保存")}</button></footer>
      </form>}
    </dialog>
  </>;
}
