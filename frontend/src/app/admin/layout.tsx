"use client";

import { useEffect, useState, type ReactNode } from "react";
import Link from "next/link";
import { usePathname } from "next/navigation";
import { ArrowLeft, Cpu, ShieldCheck, Users } from "lucide-react";
import { Brand } from "@/components/brand";
import { api, type UserProfile } from "@/lib/api";

export default function AdminLayout({ children }: { children: ReactNode }) {
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
  return <main className="settings-shell admin-shell">
    <aside className="settings-sidebar admin-sidebar">
      <Brand /><div className="admin-label"><ShieldCheck size={15} />管理后台</div>
      <nav aria-label="后台导航">
        <Link href="/admin/users" className={path === "/admin/users" ? "active" : ""} aria-current={path === "/admin/users" ? "page" : undefined}><Users />人员管理</Link>
        <Link href="/admin/models" className={path === "/admin/models" ? "active" : ""} aria-current={path === "/admin/models" ? "page" : undefined}><Cpu />共享模型</Link>
      </nav>
      <Link className="back-button" href="/app"><ArrowLeft size={16} />返回工作区</Link>
      {user && <small className="admin-identity">{user.display_name}<br />系统管理员</small>}
    </aside>
    <section className="settings-main">
      {error ? <p className="settings-error" role="alert">{error}</p> : user ? children : <p role="status">正在验证权限…</p>}
    </section>
  </main>;
}
