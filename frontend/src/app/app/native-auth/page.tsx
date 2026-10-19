"use client";

import { useEffect, useRef, useState } from "react";
import { api, type UserProfile } from "@/lib/api";
import { useT } from "@/components/i18n";
import { Brand } from "@/components/brand";

export default function NativeSignIn() {
  const t = useT();
  const [profile, setProfile] = useState<UserProfile | null>(null), [error, setError] = useState(""), [busy, setBusy] = useState(false);
  const request = useRef(false);
  useEffect(() => { let alive = true; void api<UserProfile>("/api/v1/me").then(value => { if (alive) setProfile(value); }).catch(() => { if (alive) setError("登录状态读取失败，请重新登录。"); }); return () => { alive = false; }; }, []);
  async function connect() {
    if (request.current || !profile) return;
    const params = new URLSearchParams(location.search), challenge = params.get("challenge") ?? "", state = params.get("state") ?? "";
    if (!/^[A-Za-z0-9_-]{43}$/.test(challenge) || !/^[A-Za-z0-9_-]{43}$/.test(state)) { setError("App 登录请求无效，请从 App 重新登录。"); return; }
    request.current = true; setBusy(true); setError("");
    try {
      const result = await api<{ code: string }>("/api/v1/auth/native/handoff", { method: "POST", body: JSON.stringify({ challenge }) });
      const callback = new URL("lester://auth/callback"); callback.searchParams.set("code", result.code); callback.searchParams.set("state", state);
      location.assign(callback.href);
    } catch { setError("App 登录未完成，请回到 App 重新登录。"); }
    finally { request.current = false; setBusy(false); }
  }
  return <main className="login-page"><section className="login-card"><div className="login-brand"><Brand /></div><div className="login-copy"><h1>{t("在 Lester App 中继续")}</h1>
    <p>{t("确认将当前账号登录到发起请求的 Lester App。")}</p>{profile ? <p>{profile.display_name} · {profile.email}</p> : null}
    </div>{error ? <p className="form-error" role="alert">{t(error)}</p> : null}
    <button type="button" className="primary-button" disabled={!profile || busy} onClick={() => void connect()}>{t(busy ? "正在连接…" : "登录到 App")}</button>
  </section></main>;
}
