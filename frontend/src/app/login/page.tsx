"use client";

import { FormEvent, useState } from "react";
import { useRouter } from "next/navigation";
import { Brand } from "@/components/brand";
import { api } from "@/lib/api";

export default function Login() {
  const router = useRouter();
  const [mode, setMode] = useState<"login" | "register">("login");
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);

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

  return <main className="login-page"><section className="login-card"><Brand /><div className="login-copy"><p className="eyebrow">Agent Workspace</p><h1>{mode === "login" ? "欢迎回来" : "创建你的 Workspace"}</h1><p>一个对话，一台独立 Computer。把工作交给 Agent，而不是搭一张流程图。</p></div><form onSubmit={submit}>{mode === "register" && <label>称呼<input name="displayName" required autoComplete="name" /></label>}<label>邮箱<input name="email" type="email" required autoComplete="email" /></label><label>密码<input name="password" type="password" minLength={10} required autoComplete={mode === "login" ? "current-password" : "new-password"} /></label>{error && <p className="form-error" role="alert">{error}</p>}<button className="primary-button" disabled={busy}>{busy ? "处理中…" : mode === "login" ? "登录" : "注册"}</button></form><button className="text-button" disabled={busy} onClick={() => { setMode(mode === "login" ? "register" : "login"); setError(""); }}>{mode === "login" ? "没有账号？创建一个" : "已有账号？去登录"}</button></section></main>;
}
