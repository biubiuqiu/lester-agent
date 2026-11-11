"use client";
import { AssistantLauncherSlot } from "@/components/personal-assistant-dock";
import { LanguageSelect } from "@/components/i18n";
import { T, useT } from "@/components/i18n";


import { ReactNode, useEffect, useRef } from "react";
import { ChevronLeft } from "lucide-react";
import { ProductGlyph } from "./product-glyph";
import { useRouter } from "next/navigation";
import { Brand } from "./brand";
import { GuideLauncher } from "./user-guide";
import { workspaceReturnPath } from "@/lib/workspace-return";

export function SettingsShell({ active, children, returnTo = "/app" }: { active: "profile" | "memory" | "models" | "sandbox" | "skills" | "automations" | "connectors"; children: ReactNode; returnTo?: string }) {
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
        <button aria-current={active === "profile" ? "page" : undefined} className={active === "profile" ? "active" : ""} onClick={() => navigate("profile")}><ProductGlyph kind="profile" size={19} /><T>{"个人资料"}</T></button>
        <button aria-current={active === "memory" ? "page" : undefined} className={active === "memory" ? "active" : ""} onClick={() => navigate("memory")}><ProductGlyph kind="memory" size={19} /><T>{"记忆"}</T></button>
        <button aria-current={active === "models" ? "page" : undefined} className={active === "models" ? "active" : ""} onClick={() => navigate("models")}><ProductGlyph kind="model" size={19} /><T>{"模型"}</T></button>
        <button aria-current={active === "connectors" ? "page" : undefined} className={active === "connectors" ? "active" : ""} onClick={() => navigate("connectors")}><ProductGlyph kind="connections" size={19} /><T>{"应用连接"}</T></button>
        <button aria-current={active === "sandbox" ? "page" : undefined} className={active === "sandbox" ? "active" : ""} onClick={() => navigate("sandbox")}><ProductGlyph kind="computer" size={19} />Computer</button>
        <button aria-current={active === "skills" ? "page" : undefined} className={active === "skills" ? "active" : ""} onClick={() => navigate("skills")}><ProductGlyph kind="skill" size={19} /><T>{"Skill 广场"}</T></button>
        <button aria-current={active === "automations" ? "page" : undefined} className={active === "automations" ? "active" : ""} onClick={() => navigate("automations")}><ProductGlyph kind="automation" size={19} /><T>{"自动任务"}</T></button>
      </nav>
      <div className="settings-sidebar-utilities"><LanguageSelect />
      <AssistantLauncherSlot /><GuideLauncher />
      <button className="back-button" aria-label={t("返回工作区")} title={t("返回工作区")} onClick={() => router.push(returnPath())}><ChevronLeft /><span><T>{"返回工作区"}</T></span></button></div>
    </aside>
    <section className="settings-main">{children}</section>
  </main>;
}
