"use client";

import { FormEvent, useState } from "react";
import { useRouter } from "next/navigation";
import Link from "next/link";
import { Eye, EyeOff } from "lucide-react";
import { Brand } from "@/components/brand";
import { api } from "@/lib/api";

export default function Login() {
  const router = useRouter();
  const [mode, setMode] = useState<"login" | "register">("login");
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  const [showPassword, setShowPassword] = useState(false);

  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    setBusy(true);
    setError("");
    const data = new FormData(event.currentTarget);
    try {
      await api(`/api/v1/auth/${mode}`, {
        method: "POST",
        body: JSON.stringify({ email: data.get("email"), password: data.get("password"), displayName: data.get("displayName") }),
      });
      router.replace("/app");
      router.refresh();
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : "请求失败");
    } finally {
      setBusy(false);
    }
  }

  return (
    <main className="login-page">
      <section className="login-card" aria-labelledby="login-title">
        <Brand />
        <div className="login-copy">
          <p className="eyebrow">Agent Workspace</p>
          <h1 id="login-title">{mode === "login" ? "欢迎回来" : "开始你的第一项任务"}</h1>
          <p>描述目标，上传材料，让 Lester 帮你完成工作。文件与成果都在你的工作区里。</p>
        </div>
        <form onSubmit={submit} aria-busy={busy}>
          {mode === "register" && <label className="field">称呼<input name="displayName" required autoComplete="name" placeholder="我们该怎么称呼你？" disabled={busy} /></label>}
          <label className="field">邮箱<input name="email" type="email" required autoComplete="email" placeholder="you@example.com" disabled={busy} /></label>
          <div className="field">
            <label htmlFor="login-password">密码</label>
            <div className="password-field">
              <input id="login-password" name="password" type={showPassword ? "text" : "password"} minLength={mode === "register" ? 10 : undefined} required autoComplete={mode === "login" ? "current-password" : "new-password"} aria-describedby={mode === "register" ? "password-help" : undefined} disabled={busy} />
              <button type="button" aria-label={showPassword ? "隐藏密码" : "显示密码"} aria-pressed={showPassword} disabled={busy} onClick={() => setShowPassword(value => !value)}>{showPassword ? <EyeOff /> : <Eye />}</button>
            </div>
            {mode === "register" && <small id="password-help" className="field-help">至少 10 个字符，建议使用不重复的密码。</small>}
          </div>
          {error && <p className="form-error" role="alert">{error}</p>}
          <button className="primary-button" disabled={busy}>{busy ? "处理中…" : mode === "login" ? "登录" : "创建账号"}</button>
        </form>
        <button className="text-button" disabled={busy} onClick={() => { setMode(mode === "login" ? "register" : "login"); setShowPassword(false); setError(""); }}>{mode === "login" ? "没有账号？创建一个" : "已有账号？去登录"}</button>
        <nav className="login-public-links" aria-label="项目与帮助"><Link href="/">了解 Lester</Link><Link href="/docs">帮助文档</Link></nav>
      </section>
    </main>
  );
}
