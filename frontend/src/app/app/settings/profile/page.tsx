"use client";

import { FormEvent, useEffect, useState } from "react";
import { Check, UserRound } from "lucide-react";
import { SettingsShell } from "@/components/settings-shell";
import { avatarOptions, UserAvatar } from "@/components/user-avatar";
import { api, AvatarKey, UserProfile } from "@/lib/api";

export default function ProfileSettings() {
  const [profile, setProfile] = useState<UserProfile | null>(null);
  const [displayName, setDisplayName] = useState("");
  const [avatarKey, setAvatarKey] = useState<AvatarKey>("forest");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [saved, setSaved] = useState(false);

  useEffect(() => {
    let active = true;
    api<UserProfile>("/api/v1/me").then((value) => {
      if (!active) return;
      setProfile(value);
      setDisplayName(value.display_name);
      setAvatarKey(value.avatar_key || "forest");
    }).catch((reason: Error) => {
      if (active) setError(reason.message);
    });
    return () => { active = false; };
  }, []);

  async function save(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    setBusy(true);
    setError("");
    setSaved(false);
    try {
      const updated = await api<UserProfile>("/api/v1/me", { method: "PATCH", body: JSON.stringify({ display_name: displayName, avatar_key: avatarKey }) });
      setProfile(updated);
      setDisplayName(updated.display_name);
      setAvatarKey(updated.avatar_key);
      setSaved(true);
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : "保存失败");
    } finally {
      setBusy(false);
    }
  }

  return <SettingsShell active="profile">
    <header className="settings-heading">
      <div><p className="eyebrow">Settings / Profile</p><h1>个人资料</h1><p>管理你在 Lester 中展示的称呼和头像。</p></div>
    </header>
    <form className="profile-settings" onSubmit={save}>
      <section className="profile-identity">
        <UserAvatar displayName={displayName || profile?.display_name} avatarKey={avatarKey} size="large" />
        <div><h2>{displayName || "你的称呼"}</h2><p>{profile?.email || "正在载入账户信息…"}</p></div>
      </section>
      <section className="settings-card profile-card">
        <header><span className="card-icon"><UserRound /></span><div><h2>基本信息</h2><p>这些信息用于侧边栏和你的账户菜单。</p></div></header>
        <label className="field">称呼<input value={displayName} onChange={(event) => { setDisplayName(event.target.value); setSaved(false); }} maxLength={60} required autoComplete="name" /></label>
        <label className="field">邮箱<input value={profile?.email || ""} readOnly aria-readonly="true" /></label>
        <p className="profile-help">邮箱是当前登录账号。如需更换账号，请先退出登录。</p>
      </section>
      <section className="settings-card profile-card">
        <header><div><h2>选择头像</h2><p>选择一个 Lester 内置头像主题。</p></div></header>
        <div className="avatar-picker" role="radiogroup" aria-label="选择头像">
          {avatarOptions.map((option) => <button key={option.key} type="button" role="radio" aria-checked={avatarKey === option.key} className={avatarKey === option.key ? "selected" : ""} onClick={() => { setAvatarKey(option.key); setSaved(false); }}>
            <UserAvatar displayName={displayName} avatarKey={option.key} size="large" />
            <span>{option.label}</span>
            {avatarKey === option.key ? <Check /> : null}
          </button>)}
        </div>
      </section>
      {error ? <p className="settings-error">保存失败：{error}</p> : null}
      <footer className="profile-actions"><span>{saved ? <><Check />已保存</> : "修改后记得保存"}</span><button className="primary-button" disabled={busy || !displayName.trim()}>{busy ? "保存中…" : "保存更改"}</button></footer>
    </form>
  </SettingsShell>;
}
