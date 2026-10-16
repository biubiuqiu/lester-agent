"use client";
import { LanguageSelect } from "@/components/i18n";
import { T, useT } from "@/components/i18n";


import { ReactNode, useEffect, useRef } from "react";
import { Box, ChevronLeft, Clock3, Database, ServerCog, UserRound } from "lucide-react";
import { useRouter } from "next/navigation";
import { Brand } from "./brand";
import { GuideLauncher } from "./user-guide";

export function SettingsShell({ active, children, returnTo = "/app" }: { active: "profile" | "models" | "sandbox" | "skills" | "automations"; children: ReactNode; returnTo?: string }) {
  const router = useRouter();
  const t = useT();
  const navigation = useRef<HTMLElement>(null);
  useEffect(() => {
    const node = navigation.current;
    if (!node) return;
    const reveal = () => {
      if (node.scrollWidth > node.clientWidth) node.querySelector('[aria-current="page"]')?.scrollIntoView({ block: "nearest", inline: "nearest" });
    };
    const observer = new ResizeObserver(reveal);
    observer.observe(node);
    reveal();
    return () => observer.disconnect();
  }, [active, t]);
  const returnPath = /^\/app(?:\/p\/[A-Za-z0-9_-]+)?$/.test(returnTo) ? returnTo : "/app";
  return <main className="settings-shell">
    <aside className="settings-sidebar">
      <div className="settings-sidebar-brand"><Brand /></div>
      <nav ref={navigation}>
        <button aria-current={active === "profile" ? "page" : undefined} className={active === "profile" ? "active" : ""} onClick={() => router.push("/app/settings/profile")}><UserRound /><T>{"个人资料"}</T></button>
        <button aria-current={active === "models" ? "page" : undefined} className={active === "models" ? "active" : ""} onClick={() => router.push("/app/settings/models")}><Database /><T>{"模型"}</T></button>
        <button aria-current={active === "sandbox" ? "page" : undefined} className={active === "sandbox" ? "active" : ""} onClick={() => router.push("/app/settings/sandbox")}><ServerCog />Computer</button>
        <button aria-current={active === "skills" ? "page" : undefined} className={active === "skills" ? "active" : ""} onClick={() => router.push("/app/settings/skills")}><Box /><T>{"Skill 广场"}</T></button>
        <button aria-current={active === "automations" ? "page" : undefined} className={active === "automations" ? "active" : ""} onClick={() => router.push("/app/settings/automations")}><Clock3 /><T>{"自动任务"}</T></button>
      </nav>
      <div className="settings-sidebar-utilities"><LanguageSelect />
      <GuideLauncher />
      <button className="back-button" aria-label={t("返回工作区")} title={t("返回工作区")} onClick={() => router.push(returnPath)}><ChevronLeft /><span><T>{"返回工作区"}</T></span></button></div>
    </aside>
    <section className="settings-main">{children}</section>
  </main>;
}
