"use client";

import { ReactNode } from "react";
import { Box, ChevronLeft, Database, ServerCog, UserRound } from "lucide-react";
import { useRouter } from "next/navigation";
import { Brand } from "./brand";

export function SettingsShell({ active, children }: { active: "profile" | "models" | "sandbox" | "skills"; children: ReactNode }) {
  const router = useRouter();
  return <main className="settings-shell">
    <aside className="settings-sidebar">
      <Brand />
      <nav>
        <button className={active === "profile" ? "active" : ""} onClick={() => router.push("/app/settings/profile")}><UserRound />个人资料</button>
        <button className={active === "models" ? "active" : ""} onClick={() => router.push("/app/settings/models")}><Database />模型</button>
        <button className={active === "sandbox" ? "active" : ""} onClick={() => router.push("/app/settings/sandbox")}><ServerCog />Computer</button>
        <button className={active === "skills" ? "active" : ""} onClick={() => router.push("/app/settings/skills")}><Box />Skill 广场</button>
      </nav>
      <button className="back-button" onClick={() => router.push("/app")}><ChevronLeft />返回工作区</button>
    </aside>
    <section className="settings-main">{children}</section>
  </main>;
}
