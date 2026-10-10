"use client";
import { T, useT } from "@/components/i18n";


import { ChangeEvent, FormEvent, useEffect, useRef, useState } from "react";
import dynamic from "next/dynamic";
import { authenticatedFetch } from "@/lib/auth-client";
import { API } from "@/lib/api";
import { Check, Upload, UserRound } from "lucide-react";
import { SettingsShell } from "@/components/settings-shell";
import { AccountSecurity } from "@/components/account-security";
import { avatarOptions, UserAvatar } from "@/components/user-avatar";
import { api, upload, AvatarKey, AuthOptions, UserProfile } from "@/lib/api";

const AvatarCropper = dynamic(() => import("@/components/avatar-cropper").then(module => module.AvatarCropper), { ssr: false });

export default function ProfileSettings() {
  const t = useT();

  const [cropFile, setCropFile] = useState<File | null>(null);
  const [profile, setProfile] = useState<UserProfile | null>(null);
  const [options, setOptions] = useState<AuthOptions | null>(null);
  const [displayName, setDisplayName] = useState("");
  const [avatarKey, setAvatarKey] = useState<AvatarKey>("forest");
  const [useDefaultAvatar, setUseDefaultAvatar] = useState(false);
  const [busy, setBusy] = useState(false);
  const inFlight = useRef(false);
  const [error, setError] = useState("");
  const [notice, setNotice] = useState("");

  useEffect(() => {
    let active = true;
    api<UserProfile>("/api/v1/me").then(value => {
      if (!active) return;
      setProfile(value); setDisplayName(value.display_name); setAvatarKey(value.avatar_key || "forest");
    }).catch(reason => { if (active) setError(reason instanceof Error ? reason.message : "账户读取失败，请刷新重试"); });
    api<AuthOptions>("/api/v1/auth/options").then(value => { if (active) setOptions(value); }).catch(() => {});
    return () => { active = false; };
  }, []);
  const dirty = Boolean(profile && (displayName !== profile.display_name || avatarKey !== profile.avatar_key || (useDefaultAvatar && profile.avatar_url)));
  async function run(callback: () => Promise<void>) {
    if (inFlight.current) return;
    inFlight.current = true; setBusy(true); setError(""); setNotice("");
    try { await callback(); } catch (reason) { setError(reason instanceof Error ? reason.message : "操作失败"); }
    finally { inFlight.current = false; setBusy(false); }
  }
  async function save(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    await run(async () => {
      const updated = await api<UserProfile>("/api/v1/me", { method: "PATCH", body: JSON.stringify({ display_name: displayName, avatar_key: avatarKey, use_default_avatar: useDefaultAvatar }) });
      setProfile(updated); setDisplayName(updated.display_name); setAvatarKey(updated.avatar_key); setUseDefaultAvatar(false); setNotice("个人资料已保存。");
    });
  }
  async function changeAvatar(event: ChangeEvent<HTMLInputElement>) {
    const file = event.target.files?.[0]; event.target.value = "";
    if (!file) return;
    if (file.size > 2 * 1024 * 1024) { setError("请选择小于 2 MB 的图片。"); return; }
    if (!["image/png", "image/jpeg", "image/gif"].includes(file.type)) { setError("请选择 PNG、JPEG 或 GIF 图片。"); return; }
    setError(""); setCropFile(file);
  }
  async function saveCroppedAvatar(file: File) {
    if (inFlight.current) throw new Error("其他资料正在保存，请稍后重试。");
    inFlight.current = true; setBusy(true); setError("");
    try {
      const data = new FormData(); data.append("avatar", file);
      const updated = await upload<UserProfile>("/api/v1/me/avatar", data);
      setProfile(updated); setUseDefaultAvatar(false); setNotice("图片头像已保存。");
    } finally { inFlight.current = false; setBusy(false); }
  }
  async function recropAvatar() {
    if (!profile?.avatar_url) return;
    await run(async () => {
      const response = await authenticatedFetch(API + profile.avatar_url);
      if (!response.ok) throw new Error("头像读取失败，请重试。");
      const blob = await response.blob();
      setCropFile(new File([blob], "avatar.png", { type: "image/png" }));
    });
  }
  async function useProviderAvatar(provider: "google" | "github") {
    const updated = await api<UserProfile>(`/api/v1/me/avatar/${provider}`, { method: "POST" });
    setProfile(updated); setUseDefaultAvatar(false);
  }
  return <SettingsShell active="profile">
    <header className="settings-heading"><div><p className="eyebrow">Settings / Account</p><h1><T>{"个人资料与账号"}</T></h1><p><T>{"管理称呼、头像、登录方式和账号安全。"}</T></p></div></header>
    <form className="profile-settings" onSubmit={save}>
      <section className="profile-identity"><UserAvatar displayName={displayName || profile?.display_name} avatarKey={avatarKey} avatarURL={useDefaultAvatar ? undefined : profile?.avatar_url} size="large" /><div><h2>{displayName || t("你的称呼")}</h2><p>{profile?.email || t("正在载入账户信息…")}</p></div></section>
      <section className="settings-card profile-card"><header><span className="card-icon"><UserRound /></span><div><h2><T>{"基本信息"}</T></h2><p><T>{"这些信息用于侧边栏和你的账户菜单。"}</T></p></div></header><label className="field"><T>{"称呼"}</T><input value={displayName} onChange={event => { setDisplayName(event.target.value); setNotice(""); }} maxLength={60} required autoComplete="name" disabled={busy || !profile} /></label><label className="field"><T>{"账号邮箱"}</T><input value={profile?.email || ""} readOnly aria-readonly="true" /></label><div className="profile-email-status"><span>{profile?.email_verified ? <><Check size={14} /><T>{"邮箱已验证"}</T></> : t("邮箱尚未验证")}</span>{profile && !profile.email_verified && options?.email_verification_required && <button className="text-button" type="button" disabled={busy} onClick={() => void run(async () => { const result = await api<{ message: string }>("/api/v1/auth/resend-verification", { method: "POST", body: JSON.stringify({ email: profile.email }) }); setNotice(result.message); })}><T>{"发送验证邮件"}</T></button>}</div><p className="profile-help"><T>{"绑定第三方登录不会更改账号邮箱、已有项目或文件。"}</T></p></section>
      <section className="settings-card profile-card"><header><div><h2><T>{"头像"}</T></h2><p><T>{"上传自己的照片，或选择一个内置头像主题。"}</T></p></div></header><div className="avatar-upload-actions"><label className={`secondary-button ${busy || !profile ? "disabled" : ""}`} htmlFor="avatar-upload"><Upload size={15} /><T>{"上传图片"}</T></label><input id="avatar-upload" className="account-file-input" type="file" accept="image/png,image/jpeg,image/gif" onChange={event => void changeAvatar(event)} disabled={busy || !profile} aria-label={t("上传头像图片")} />{profile?.avatar_url && <button type="button" className="secondary-button" disabled={busy} onClick={() => void recropAvatar()}><T>{"调整裁剪"}</T></button>}{profile?.avatar_url && <button type="button" className="text-button" disabled={busy} onClick={() => void run(async () => { const updated = await api<UserProfile>("/api/v1/me/avatar", { method: "DELETE" }); setProfile(updated); setUseDefaultAvatar(false); setNotice("已恢复内置头像。"); })}><T>{"恢复内置头像"}</T></button>}</div><p className="profile-help"><T>{"PNG、JPEG 或 GIF，小于 2 MB。上传后可拖动、缩放、旋转并预览圆形效果；GIF 使用首帧。确认裁剪后保存，取消不会更改头像。"}</T></p><div className="avatar-picker" role="radiogroup" aria-label={t("选择头像主题")}>{avatarOptions.map(option => {
        const selected = avatarKey === option.key && (!profile?.avatar_url || useDefaultAvatar);
        return <button key={option.key} type="button" role="radio" aria-checked={selected} className={selected ? "selected" : ""} disabled={busy || !profile} onClick={() => { setAvatarKey(option.key); setUseDefaultAvatar(true); setNotice(""); }}><UserAvatar displayName={displayName} avatarKey={option.key} size="large" /><span>{t(option.label)}</span>{selected ? <Check /> : null}</button>;
      })}</div></section>
      {error && <p className="settings-error" role="alert">{t(error)}</p>}
      {notice && <p className="account-feedback" role="status"><Check size={16} />{t(notice)}</p>}
      <footer className="profile-actions"><span>{dirty ? t("资料更改尚未保存") : ""}</span><button className="primary-button" disabled={busy || !profile || !displayName.trim() || !dirty}>{busy ? t("保存中…") : t("保存资料")}</button></footer>
    </form>
    {cropFile && <AvatarCropper key={`${cropFile.name}:${cropFile.lastModified}`} file={cropFile} onCancel={() => setCropFile(null)} onSave={saveCroppedAvatar} />}
    <AccountSecurity profile={profile} unsaved={dirty || busy} onPasswordSet={() => setProfile(value => value ? { ...value, has_password: true } : value)} onProviderAvatar={useProviderAvatar} />
  </SettingsShell>;
}
