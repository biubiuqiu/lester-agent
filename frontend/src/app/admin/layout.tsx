"use client";
import { T, useT } from "@/components/i18n";


import { useEffect, useState, type ReactNode } from "react";
import Link from "next/link";
import { usePathname } from "next/navigation";
import { ArrowLeft, Cpu, ShieldCheck, Users } from "lucide-react";
import { Brand } from "@/components/brand";
import { api, type UserProfile } from "@/lib/api";

import { AuthSessionKeeper } from "@/components/auth-session-keeper";

export default function AdminLayout({ children }: { children: ReactNode }) {
  const t = useT();

  const path = usePathname();
  const [user, setUser] = useState<UserProfile | null>(null);
  const [error, setError] = useState("");
  useEffect(() => {
    let active = true;
    api<UserProfile>("/api/v1/me").then((profile) => {
      if (!active) return;
      if (profile.role !== "admin") setError("此页面仅对管理员开放。请联系系统管理员。");
      else setUser(profile);
    }).catch((reason: unknown) => { if (active) setError(reason instanceof Error ? reason.message : "无法验证管理员权限"); });
    return () => { active = false; };
  }, []);
  return <main className="settings-shell admin-shell"><AuthSessionKeeper />
    <aside className="settings-sidebar admin-sidebar">
      <Brand /><div className="admin-label"><ShieldCheck size={15} /><T>{"管理后台"}</T></div>
      <nav aria-label={t("后台导航")}>
        <Link href="/admin/users" className={path === "/admin/users" ? "active" : ""} aria-current={path === "/admin/users" ? "page" : undefined}><Users /><T>{"人员管理"}</T></Link>
        <Link href="/admin/models" className={path === "/admin/models" ? "active" : ""} aria-current={path === "/admin/models" ? "page" : undefined}><Cpu /><T>{"共享模型"}</T></Link>
      </nav>
      <Link className="back-button" href="/app"><ArrowLeft size={16} /><T>{"返回工作区"}</T></Link>
      {user && <small className="admin-identity">{user.display_name}<br /><T>{"系统管理员"}</T></small>}
    </aside>
    <section className="settings-main">
      {error ? <p className="settings-error" role="alert">{t(error)}</p> : user ? children : <p role="status"><T>{"正在验证权限…"}</T></p>}
    </section>
  </main>;
}
