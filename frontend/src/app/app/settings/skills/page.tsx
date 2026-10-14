"use client";
import { T, useT } from "@/components/i18n";


import { useEffect, useState } from "react";
import { Box, CheckCircle2 } from "lucide-react";
import { SettingsShell } from "@/components/settings-shell";
import { api, Skill } from "@/lib/api";

export default function SkillMarketplace() {
  const t = useT();

  const [skills, setSkills] = useState<Skill[]>([]);
  const [error, setError] = useState("");
  const [loading, setLoading] = useState(true);
  const [attempt, setAttempt] = useState(0);

  useEffect(() => {
    let active = true;
    api<{ skills: Skill[] }>("/api/v1/skills")
      .then((result) => { if (active) setSkills(result.skills); })
      .catch((reason: Error) => { if (active) setError(reason.message); })
      .finally(() => { if (active) setLoading(false); });
    return () => { active = false; };
  }, [attempt]);

  return <SettingsShell active="skills">
    <header className="settings-heading">
      <div><p className="eyebrow">Settings / Skills</p><h1><T>{"Skill 广场"}</T></h1><p><T>{"浏览可复用能力，并在具体会话的 Computer 面板中按需安装。"}</T></p></div>

    </header>
    {error ? <p className="settings-error" role="alert"><T>{"加载失败："}</T>{t(error)} <button type="button" className="text-button" onClick={() => { setError(""); setLoading(true); setAttempt(value => value + 1); }}>{t("重试")}</button></p> : null}
    {loading ? <p className="skill-market-notice" role="status">{t("正在加载 Skill")}</p> : !error && !skills.length ? <section className="skill-market-empty"><Box aria-hidden="true" /><h2>{t("还没有可用的 Skill")}</h2><p>{t("这里会展示工作区中可用的技能包。")}</p></section> : null}
    <section className="skill-market-grid" aria-busy={loading}>
      {skills.map((skill) => <article className="skill-market-card" key={skill.id}>
        <span className="card-icon"><Box /></span>
        <div className="skill-card-copy"><span className="skill-source">{skill.source === "builtin" ? t("官方内置") : t("工作区")}</span><h2>{skill.name}</h2><p>{skill.description}</p></div>
        <footer><span>v{skill.version}</span><span><CheckCircle2 /><T>{"会话级安装"}</T></span></footer>
      </article>)}
    </section>
  </SettingsShell>;
}
