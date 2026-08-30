"use client";

import { useEffect, useState } from "react";
import { Box, CheckCircle2, HardDrive } from "lucide-react";
import { SettingsShell } from "@/components/settings-shell";
import { api, Skill } from "@/lib/api";

export default function SkillMarketplace() {
  const [skills, setSkills] = useState<Skill[]>([]);
  const [error, setError] = useState("");

  useEffect(() => {
    let active = true;
    api<{ skills: Skill[] }>("/api/v1/skills")
      .then((result) => { if (active) setSkills(result.skills); })
      .catch((reason: Error) => { if (active) setError(reason.message); });
    return () => { active = false; };
  }, []);

  return <SettingsShell active="skills">
    <header className="settings-heading">
      <div><p className="eyebrow">Settings / Skills</p><h1>Skill 广场</h1><p>浏览可复用能力，并在具体会话的 Computer 面板中按需安装。</p></div>
      <span className="secure-badge"><HardDrive />MinIO · S3 compatible</span>
    </header>
    {error ? <p className="settings-error">加载失败：{error}</p> : null}
    <section className="skill-market-grid">
      {skills.map((skill) => <article className="skill-market-card" key={skill.id}>
        <span className="card-icon"><Box /></span>
        <div className="skill-card-copy"><span className="skill-source">{skill.source === "builtin" ? "官方内置" : "工作区"}</span><h2>{skill.name}</h2><p>{skill.description}</p></div>
        <footer><span>v{skill.version}</span><span><CheckCircle2 />会话级安装</span></footer>
      </article>)}
    </section>
  </SettingsShell>;
}
