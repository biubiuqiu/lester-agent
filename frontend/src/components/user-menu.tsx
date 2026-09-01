"use client";

import { useEffect, useRef, useState } from "react";
import { Boxes, Cpu, LogOut, MonitorCog, MoreHorizontal, UserRound } from "lucide-react";
import { useRouter } from "next/navigation";
import { api, UserProfile } from "@/lib/api";
import { UserAvatar } from "./user-avatar";

const menuItems = [
  { label: "个人资料", path: "/app/settings/profile", icon: UserRound },
  { label: "模型", path: "/app/settings/models", icon: Cpu },
  { label: "Computer", path: "/app/settings/sandbox", icon: MonitorCog },
  { label: "Skill 广场", path: "/app/settings/skills", icon: Boxes },
];

export function UserMenu({ user }: { user: UserProfile | null }) {
  const router = useRouter();
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
    {open ? <div className="user-menu-popover" role="menu" aria-label="账户与设置">
      <header><UserAvatar displayName={name} avatarKey={user?.avatar_key} /><span><strong>{name}</strong><small>{user?.email || "正在加载账户…"}</small></span></header>
      <div className="user-menu-items">
        {menuItems.map((item) => <button key={item.path} type="button" role="menuitem" onClick={() => navigate(item.path)}><item.icon /><span>{item.label}</span></button>)}
      </div>
      {error ? <p className="settings-error" role="alert">{error}</p> : null}
      <button type="button" className="user-menu-logout" role="menuitem" onClick={logout} disabled={loggingOut}><LogOut /><span>{loggingOut ? "正在退出…" : "退出登录"}</span></button>
    </div> : null}
    <button type="button" className="user-menu-trigger" aria-haspopup="menu" aria-expanded={open} onClick={() => setOpen((value) => !value)}>
      <UserAvatar displayName={name} avatarKey={user?.avatar_key} />
      <span><strong>{name}</strong><small>{user?.email || "个人账户"}</small></span>
      <MoreHorizontal />
    </button>
  </div>;
}
