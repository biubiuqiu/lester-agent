"use client";

import { FormEvent, useEffect, useRef, useState } from "react";
import { useRouter } from "next/navigation";
import Link from "next/link";
import { ArrowLeft, CheckCircle2, Eye, EyeOff, Mail } from "lucide-react";
import { Brand } from "@/components/brand";
import { AuthProviderIcon } from "@/components/auth-provider-icon";
import { oauthErrors } from "@/lib/auth-ui";
import { API, api, AuthOptions } from "@/lib/api";

import { safeLoginReturn } from "@/lib/auth-client";

type Mode = "login" | "register" | "forgot" | "reset" | "verify";

function loginURL(initial: URL | null, requestedMode?: string | null) {
  const params = new URLSearchParams();
  if (requestedMode === "reset" || requestedMode === "verify") params.set("mode", requestedMode);
  const returnTo = safeLoginReturn(initial?.searchParams.get("returnTo") ?? null);
  if (returnTo !== "/app") params.set("returnTo", returnTo);
  return `/login${params.size ? `?${params}` : ""}`;
}

export default function Login() {
  const router = useRouter();
  const [mode, setMode] = useState<Mode>("login");
  const [options, setOptions] = useState<AuthOptions | null>(null);
  const [optionsError, setOptionsError] = useState(false);
  const [email, setEmail] = useState("");
  const [name, setName] = useState("");
  const [password, setPassword] = useState("");
  const [confirmation, setConfirmation] = useState("");
  const [token, setToken] = useState("");
  const [error, setError] = useState("");
  const [notice, setNotice] = useState("");
  const [busy, setBusy] = useState(false);
  const inFlight = useRef(false);
  const [showPassword, setShowPassword] = useState(false);
  const initialURL = useRef<URL | null>(null);
  const [returnTo, setReturnTo] = useState("/app");

  useEffect(() => {
    let active = true;
    const url = initialURL.current ??= new URL(window.location.href);
    const requestedMode = url.searchParams.get("mode");
    const authError = url.searchParams.get("auth_error");
    if (url.hash || authError) window.history.replaceState(null, "", loginURL(initialURL.current, requestedMode));
    const applyURL = () => {
      if (!active) return;
      setReturnTo(safeLoginReturn(url.searchParams.get("returnTo")));
      if (requestedMode === "reset" || requestedMode === "verify") {
        setMode(requestedMode);
        setToken(new URLSearchParams(url.hash.slice(1)).get("token") || "");
      }
      if (authError) setError(oauthErrors[authError] || oauthErrors.provider_error);
    };
    api<AuthOptions>("/api/v1/auth/options").then(value => { if (active) setOptions(value); applyURL(); }).catch(() => { if (active) setOptionsError(true); applyURL(); });
    return () => { active = false; };
  }, []);

  function changeMode(next: Mode) {
    if (busy) return;
    setMode(next); setPassword(""); setConfirmation(""); setShowPassword(false); setError(""); setNotice(""); setToken("");
    window.history.replaceState(null, "", loginURL(initialURL.current));
  }
  async function act(action: () => Promise<void>) {
    if (inFlight.current) return;
    inFlight.current = true; setBusy(true); setError(""); setNotice("");
    try { await action(); } catch (reason) { setError(reason instanceof Error ? reason.message : "请求失败，请稍后重试"); }
    finally { inFlight.current = false; setBusy(false); }
  }
  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if ((mode === "register" || mode === "reset") && password !== confirmation) { setError("两次输入的密码不一致"); return; }
    await act(async () => {
      if (mode === "forgot") {
        const result = await api<{ message: string }>("/api/v1/auth/forgot-password", { method: "POST", body: JSON.stringify({ email }) }); setNotice(result.message); return;
      }
      if (mode === "verify") {
        if (!token) throw new Error("验证链接缺少令牌，请重新发送验证邮件。");
        await api("/api/v1/auth/verify-email", { method: "POST", body: JSON.stringify({ token }) });
        setMode("login"); setToken(""); window.history.replaceState(null, "", loginURL(initialURL.current)); setNotice("邮箱已验证，现在可以登录。"); return;
      }
      if (mode === "reset") {
        if (!token) throw new Error("重设链接无效，请重新申请。");
        await api("/api/v1/auth/reset-password", { method: "POST", body: JSON.stringify({ token, password }) });
        setMode("login"); setToken(""); setPassword(""); setConfirmation(""); window.history.replaceState(null, "", loginURL(initialURL.current)); setNotice("密码已重设，其他登录已退出。请用新密码登录。"); return;
      }
      const result = await api<{ verification_required?: boolean }>(`/api/v1/auth/${mode}`, { method: "POST", body: JSON.stringify({ email, password, displayName: name }) });
      if (result?.verification_required) { setMode("login"); setPassword(""); setConfirmation(""); setNotice("验证邮件已发送，请验证邮箱后再登录。"); return; }
      router.replace(safeLoginReturn(initialURL.current?.searchParams.get("returnTo") ?? null)); router.refresh();
    });
  }
  const passwordMode = mode === "login" || mode === "register" || mode === "reset";
  const title = { login: "欢迎回来", register: "创建你的 Lester 账号", forgot: "找回密码", reset: "设置新密码", verify: "验证你的邮箱" }[mode];
  return <main className="login-page">
    <section className="login-card" aria-labelledby="login-title">
      <Brand />
      <div className="login-copy"><p className="eyebrow">Agent Workspace</p><h1 id="login-title">{title}</h1><p>{mode === "forgot" ? "输入账号邮箱，我们会发送密码重设链接。" : mode === "reset" ? "新密码保存后，其他设备的登录将失效。" : mode === "verify" ? "确认邮箱后，即可进入你的个人工作区。" : "描述目标，让 Lester 完成工作。文件与成果都留在你的工作区。"}</p></div>
      {(mode === "login" || mode === "register") && options && options.providers.length > 0 && <div className="auth-social"><div aria-label="第三方登录">{options.providers.map(provider => <a className="auth-provider-button" key={provider} href={`${API}/api/v1/auth/oauth/${provider}/start?returnTo=${encodeURIComponent(returnTo)}`} aria-disabled={busy} onClick={event => { if (busy) event.preventDefault(); }}><AuthProviderIcon provider={provider} />使用 {provider === "google" ? "Google" : "GitHub"} 继续</a>)}</div><p>或使用邮箱{mode === "register" ? "注册" : "登录"}</p></div>}
      <form onSubmit={submit} aria-busy={busy}>
        {mode === "register" && <label className="field">称呼<input name="displayName" value={name} onChange={event => setName(event.target.value)} required maxLength={60} autoComplete="name" placeholder="我们该怎么称呼你？" disabled={busy} /></label>}
        {mode !== "reset" && mode !== "verify" && <label className="field">邮箱<input name="email" value={email} onChange={event => setEmail(event.target.value)} type="email" required maxLength={254} autoComplete="email" placeholder="you@example.com" disabled={busy} /></label>}
        {passwordMode && <div className="field"><label htmlFor="login-password">{mode === "reset" ? "新密码" : "密码"}</label><div className="password-field"><input id="login-password" name="password" value={password} onChange={event => setPassword(event.target.value)} type={showPassword ? "text" : "password"} minLength={mode === "login" ? undefined : 10} maxLength={1024} required autoComplete={mode === "login" ? "current-password" : "new-password"} aria-describedby={mode !== "login" ? "password-help" : undefined} disabled={busy} /><button type="button" aria-label={showPassword ? "隐藏密码" : "显示密码"} aria-pressed={showPassword} disabled={busy} onClick={() => setShowPassword(value => !value)}>{showPassword ? <EyeOff /> : <Eye />}</button></div>{mode !== "login" && <small id="password-help" className="field-help">至少 10 个字符，建议使用不重复的密码。</small>}</div>}
        {(mode === "register" || mode === "reset") && <label className="field">确认密码<input name="confirmation" type={showPassword ? "text" : "password"} value={confirmation} onChange={event => setConfirmation(event.target.value)} required autoComplete="new-password" disabled={busy} /></label>}
        {error && <p className="form-error" role="alert">{error}</p>}
        {notice && <p className="auth-notice" role="status"><CheckCircle2 size={16} />{notice}</p>}
        <button className="primary-button" disabled={busy || ((mode === "reset" || mode === "verify") && !token) || (mode === "register" && !options?.registration_enabled)}>{busy ? "处理中…" : { login: "登录", register: "创建账号", forgot: "发送重设邮件", reset: "保存新密码", verify: "确认并验证邮箱" }[mode]}</button>
      </form>
      {mode === "register" && options?.email_verification_required && <p className="auth-registration-note"><Mail size={14} />注册后需验证邮箱，才会进入工作区。</p>}
      {mode === "login" && options?.password_reset_enabled && <button type="button" className="text-button auth-forgot" disabled={busy} onClick={() => changeMode("forgot")}>忘记密码？</button>}
      {mode === "login" || mode === "register" ? <>{options?.registration_enabled && <button type="button" className="text-button" disabled={busy} onClick={() => changeMode(mode === "login" ? "register" : "login")}>{mode === "login" ? "没有账号？创建一个" : "已有账号？去登录"}</button>}{options && !options.registration_enabled && <p className="auth-registration-note">此部署已关闭新账号注册，现有账号仍可登录。</p>}</> : <button type="button" className="text-button" disabled={busy} onClick={() => changeMode("login")}><ArrowLeft size={14} />返回登录</button>}
      {options?.email_verification_required && mode === "login" && <button type="button" className="text-button auth-resend" disabled={busy || !email.trim()} onClick={() => void act(async () => { const result = await api<{ message: string }>("/api/v1/auth/resend-verification", { method: "POST", body: JSON.stringify({ email }) }); setNotice(result.message); })}>重新发送验证邮件</button>}
      {optionsError && <p className="auth-config-error" role="alert">登录选项暂时无法读取，请刷新重试。邮箱登录仍可尝试。</p>}
      <nav className="login-public-links" aria-label="项目与帮助"><Link href="/">了解 Lester</Link><Link href="/docs">帮助文档</Link></nav>
    </section>
  </main>;
}
