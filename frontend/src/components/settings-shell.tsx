"use client";
import { AssistantLauncherSlot } from "@/components/personal-assistant-dock";
import { LanguageSelect } from "@/components/i18n";
import { T, useT } from "@/components/i18n";


import { ReactNode, useEffect, useRef } from "react";
import { Box, ChevronLeft, Clock3, Database, ServerCog, UserRound } from "lucide-react";
import { useRouter } from "next/navigation";
import { Brand } from "./brand";
import { GuideLauncher } from "./user-guide";
import { workspaceReturnPath } from "@/lib/workspace-return";

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
  const returnPath = () => {
    const requested = new URLSearchParams(window.location.search).get("returnTo") || returnTo;
    return workspaceReturnPath(requested);
  };
  const navigate = (section: string) => {
    const back = returnPath();
    router.push(`/app/settings/${section}${back === "/app" ? "" : `?returnTo=${encodeURIComponent(back)}`}`);
  };
  return <main className="settings-shell">
    <aside className="settings-sidebar">
      <div className="settings-sidebar-brand"><Brand /></div>
      <nav ref={navigation}>
        <button aria-current={active === "profile" ? "page" : undefined} className={active === "profile" ? "active" : ""} onClick={() => navigate("profile")}><UserRound /><T>{"个人资料"}</T></button>
        <button aria-current={active === "models" ? "page" : undefined} className={active === "models" ? "active" : ""} onClick={() => navigate("models")}><Database /><T>{"模型"}</T></button>
        <button aria-current={active === "sandbox" ? "page" : undefined} className={active === "sandbox" ? "active" : ""} onClick={() => navigate("sandbox")}><ServerCog />Computer</button>
        <button aria-current={active === "skills" ? "page" : undefined} className={active === "skills" ? "active" : ""} onClick={() => navigate("skills")}><Box /><T>{"Skill 广场"}</T></button>
        <button aria-current={active === "automations" ? "page" : undefined} className={active === "automations" ? "active" : ""} onClick={() => navigate("automations")}><Clock3 /><T>{"自动任务"}</T></button>
      </nav>
      <div className="settings-sidebar-utilities"><LanguageSelect />
      <AssistantLauncherSlot /><GuideLauncher />
      <button className="back-button" aria-label={t("返回工作区")} title={t("返回工作区")} onClick={() => router.push(returnPath())}><ChevronLeft /><span><T>{"返回工作区"}</T></span></button></div>
    </aside>
    <section className="settings-main">{children}</section>
  </main>;
}
