"use client";
import { LanguageSelect } from "@/components/i18n";
import { T, useT } from "@/components/i18n";


import { useEffect, useRef, useState } from "react";
import { BookOpen, Bot, Boxes, Cpu, House, LogOut, MonitorCog, MoreHorizontal, ShieldCheck, UserRound } from "lucide-react";
import { useRouter } from "next/navigation";
import { api, UserProfile } from "@/lib/api";
import { UserAvatar } from "./user-avatar";
import { clearViewState } from "@/lib/conversation-view-state";
import { useGuide } from "./user-guide";

const menuItems = [
  { label: "Agent 管理", path: "/app/agents", icon: Bot },
  { label: "上下文库", path: "/app/contexts", icon: BookOpen },
  { label: "个人资料", path: "/app/settings/profile", icon: UserRound },
  { label: "模型", path: "/app/settings/models", icon: Cpu },
  { label: "Computer", path: "/app/settings/sandbox", icon: MonitorCog },
  { label: "Skill 广场", path: "/app/settings/skills", icon: Boxes },
  { label: "项目官网", path: "/", icon: House },
  { label: "帮助文档", path: "/docs", icon: BookOpen },
];

export function UserMenu({ user }: { user: UserProfile | null }) {
  const t = useT();

  const router = useRouter();
  const guide = useGuide();
  const root = useRef<HTMLDivElement>(null);
  const [open, setOpen] = useState(false);
  const [loggingOut, setLoggingOut] = useState(false);
  const [error, setError] = useState("");

  useEffect(() => {
    if (!open) return;
    const closeOutside = (event: PointerEvent) => {
      if (!root.current?.contains(event.target as Node)) setOpen(false);
    };
    const closeOnEscape = (event: KeyboardEvent) => {
      if (event.key === "Escape") setOpen(false);
    };
    document.addEventListener("pointerdown", closeOutside);
    document.addEventListener("keydown", closeOnEscape);
    return () => {
      document.removeEventListener("pointerdown", closeOutside);
      document.removeEventListener("keydown", closeOnEscape);
    };
  }, [open]);

  function navigate(path: string) {
    setOpen(false);
    router.push(path);
  }

  async function logout() {
    setLoggingOut(true);
    setError("");
    try {
      await api("/api/v1/auth/logout", { method: "POST" });
      clearViewState();
      router.replace("/login");
      router.refresh();
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : "退出失败");
    } finally {
      setLoggingOut(false);
    }
  }

  const name = user?.display_name || "Lester User";
  return <div className="user-menu" ref={root}>
    {open ? <div className="user-menu-popover" role="menu" aria-label={t("账户与设置")}>
      <header><UserAvatar displayName={name} avatarKey={user?.avatar_key} avatarURL={user?.avatar_url} /><span><strong>{name}</strong><small>{user?.email || t("正在加载账户…")}</small></span></header>
      <LanguageSelect /><div className="user-menu-items">
        {guide && <button type="button" role="menuitem" onClick={() => { setOpen(false); guide.open(); }}><BookOpen /><span><T>{"新手引导"}</T></span></button>}
        {user?.role === "admin" && <button type="button" role="menuitem" onClick={() => navigate("/admin/users")}><ShieldCheck /><span><T>{"管理后台"}</T></span></button>}
        {menuItems.map((item) => <button key={item.path} type="button" role="menuitem" onClick={() => navigate(item.path)}><item.icon /><span>{t(item.label)}</span></button>)}
      </div>
      {error ? <p className="settings-error" role="alert">{t(error)}</p> : null}
      <button type="button" className="user-menu-logout" role="menuitem" onClick={logout} disabled={loggingOut}><LogOut /><span>{loggingOut ? t("正在退出…") : t("退出登录")}</span></button>
    </div> : null}
    <button type="button" className="user-menu-trigger" aria-haspopup="menu" aria-expanded={open} onClick={() => setOpen((value) => !value)}>
      <UserAvatar displayName={name} avatarKey={user?.avatar_key} avatarURL={user?.avatar_url} />
      <span><strong>{name}</strong><small>{user?.email || t("个人账户")}</small></span>
      <MoreHorizontal />
    </button>
  </div>;
}
