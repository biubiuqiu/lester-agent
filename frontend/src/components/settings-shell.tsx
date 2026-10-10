"use client";
import { LanguageSelect } from "@/components/i18n";
import { T } from "@/components/i18n";


import { ReactNode } from "react";
import { Box, ChevronLeft, Database, ServerCog, UserRound } from "lucide-react";
import { useRouter } from "next/navigation";
import { Brand } from "./brand";
import { GuideLauncher } from "./user-guide";

export function SettingsShell({ active, children, returnTo = "/app" }: { active: "profile" | "models" | "sandbox" | "skills"; children: ReactNode; returnTo?: string }) {
  const router = useRouter();
  const returnPath = /^\/app(?:\/p\/[A-Za-z0-9_-]+)?$/.test(returnTo) ? returnTo : "/app";
  return <main className="settings-shell">
    <aside className="settings-sidebar">
      <Brand />
      <nav>
        <button className={active === "profile" ? "active" : ""} onClick={() => router.push("/app/settings/profile")}><UserRound /><T>{"个人资料"}</T></button>
        <button className={active === "models" ? "active" : ""} onClick={() => router.push("/app/settings/models")}><Database /><T>{"模型"}</T></button>
        <button className={active === "sandbox" ? "active" : ""} onClick={() => router.push("/app/settings/sandbox")}><ServerCog />Computer</button>
        <button className={active === "skills" ? "active" : ""} onClick={() => router.push("/app/settings/skills")}><Box /><T>{"Skill 广场"}</T></button>
      </nav>
      <LanguageSelect />
      <GuideLauncher />
      <button className="back-button" onClick={() => router.push(returnPath)}><ChevronLeft /><T>{"返回工作区"}</T></button>
    </aside>
    <section className="settings-main">{children}</section>
  </main>;
}
