"use client";

import { FormEvent, useEffect, useRef, useState } from "react";
import { Check, Link2, LockKeyhole } from "lucide-react";
import { api, AuthIdentity, AuthOptions, UserProfile } from "@/lib/api";
import { oauthErrors } from "@/lib/auth-ui";
import { AuthProviderIcon } from "./auth-provider-icon";

export function AccountSecurity({ profile, unsaved, onPasswordSet, onProviderAvatar }: { profile: UserProfile | null; unsaved: boolean; onPasswordSet: () => void; onProviderAvatar: (provider: "google" | "github") => Promise<void> }) {
  const [identities, setIdentities] = useState<AuthIdentity[]>([]);
  const [options, setOptions] = useState<AuthOptions | null>(null);
  const [error, setError] = useState("");
  const [notice, setNotice] = useState("");
  const [action, setAction] = useState("");
  const actionRef = useRef(false);
  const [current, setCurrent] = useState("");
  const [password, setPassword] = useState("");
  const [confirmation, setConfirmation] = useState("");
  const initialURL = useRef<URL | null>(null);

  useEffect(() => {
    let active = true;
    const url = initialURL.current ??= new URL(window.location.href);
    const authError = url.searchParams.get("auth_error");
    if (authError || url.searchParams.has("auth")) window.history.replaceState(null, "", "/app/settings/profile");
    Promise.all([api<{ identities: AuthIdentity[] }>("/api/v1/me/identities"), api<AuthOptions>("/api/v1/auth/options")]).then(([result, value]) => {
      if (active) {
        setIdentities(result.identities); setOptions(value);
        if (authError) setError(oauthErrors[authError] || oauthErrors.provider_error);
        if (url.searchParams.get("auth") === "linked") setNotice("第三方登录已绑定。");
      }
    }).catch(reason => { if (active) setError(authError ? oauthErrors[authError] || oauthErrors.provider_error : reason instanceof Error ? reason.message : "登录方式读取失败，请刷新重试"); });
    return () => { active = false; };
  }, []);

  async function run(name: string, callback: () => Promise<void>) {
    if (actionRef.current) return;
    actionRef.current = true; setAction(name); setError(""); setNotice("");
    try { await callback(); } catch (reason) { setError(reason instanceof Error ? reason.message : "操作失败，请重试"); }
    finally { actionRef.current = false; setAction(""); }
  }
  async function changePassword(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (password !== confirmation) { setError("两次输入的密码不一致"); return; }
    await run("password", async () => {
      await api("/api/v1/me/password", { method: "PATCH", body: JSON.stringify({ current_password: current, password }) });
      setCurrent(""); setPassword(""); setConfirmation(""); onPasswordSet(); setNotice("密码已保存，其他设备的登录已退出。");
    });
  }
  return <div className="account-security">
    <section className="settings-card profile-card">
      <header><span className="card-icon"><Link2 /></span><div><h2>登录方式</h2><p>将 Google 或 GitHub 绑定到当前账号，使用任一已绑定方式登录。</p></div></header>
      {options ? <div className="identity-list">{(["google", "github"] as const).map(provider => {
        const identity = identities.find(item => item.provider === provider);
        const configured = options.providers.includes(provider);
        const lastMethod = profile?.has_password === false && identities.filter(item => item.provider !== provider && options.providers.includes(item.provider)).length === 0 && Boolean(identity);
        return <div className="identity-row" key={provider}><div className="identity-info"><AuthProviderIcon provider={provider} /><div><strong>{provider === "google" ? "Google" : "GitHub"}</strong><small>{identity ? identity.email : configured ? "尚未绑定" : "此部署尚未配置"}</small></div></div><div className="identity-actions">{identity ? <>
          {identity.has_avatar && <button type="button" className="text-button" disabled={Boolean(action) || !profile} onClick={() => void run(`avatar-${provider}`, async () => { await onProviderAvatar(provider); setNotice("已使用第三方头像。"); })}>使用其头像</button>}
          <button type="button" className="secondary-button" disabled={Boolean(action) || !profile || lastMethod} title={lastMethod ? "先设置密码或绑定另一种登录方式" : undefined} onClick={() => void run(`unlink-${provider}`, async () => { await api(`/api/v1/me/identities/${provider}`, { method: "DELETE" }); setIdentities(items => items.filter(item => item.provider !== provider)); setNotice("已解除绑定，其他设备的登录已退出。"); })}>{action === `unlink-${provider}` ? "处理中…" : "解除绑定"}</button>
        </> : <button type="button" className="secondary-button" disabled={!configured || Boolean(action) || !profile || unsaved} onClick={() => void run(`link-${provider}`, async () => { const result = await api<{ url: string }>(`/api/v1/me/identities/${provider}/link`, { method: "POST" }); window.location.assign(result.url); })}>{action === `link-${provider}` ? "前往授权…" : "绑定账号"}</button>}</div></div>;
      })}</div> : <p className="profile-help">正在读取登录方式…</p>}
      <p className="profile-help">至少保留一种可用登录方式；解除绑定会退出其他设备。{unsaved ? "绑定会离开本页，请先保存资料更改。" : "相同邮箱不会自动合并账户。"}</p>
    </section>
    <section className="settings-card profile-card">
      <header><span className="card-icon"><LockKeyhole /></span><div><h2>{profile?.has_password === false ? "设置登录密码" : "修改密码"}</h2><p>保存后保留当前登录，退出其他设备。</p></div></header>
      <form className="account-password-form" onSubmit={changePassword}>
        {profile?.has_password !== false && <label className="field">当前密码<input value={current} onChange={event => setCurrent(event.target.value)} type="password" autoComplete="current-password" maxLength={1024} required disabled={Boolean(action) || !profile} /></label>}
        <label className="field">新密码<input value={password} onChange={event => setPassword(event.target.value)} type="password" autoComplete="new-password" minLength={10} maxLength={1024} required disabled={Boolean(action) || !profile} /></label>
        <label className="field">确认新密码<input value={confirmation} onChange={event => setConfirmation(event.target.value)} type="password" autoComplete="new-password" required disabled={Boolean(action) || !profile} /></label>
        <p className="profile-help">至少 10 个字符。第三方登录账号设置密码后，也可以使用账号邮箱登录。</p>
        <button className="primary-button" disabled={Boolean(action) || !profile}>{action === "password" ? "保存中…" : "保存密码"}</button>
      </form>
    </section>
    {error && <p className="settings-error" role="alert">{error}</p>}
    {notice && <p className="account-feedback" role="status"><Check size={16} />{notice}</p>}
  </div>;
}
