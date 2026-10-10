import { getI18n } from "@/lib/i18n/server";

import { T } from "@/components/i18n";
import type { Metadata } from "next";
import Link from "next/link";
import { ArrowUpRight } from "lucide-react";

const description = "一个开源、可自托管的个人 AI 工作区。把目标交给 Lester，在你的 Computer 中执行任务，留下网页、文档与代码。";
export async function generateMetadata(): Promise<Metadata> {
  const { locale, t } = await getI18n();
  const title = t("Lester Agent · 想清楚。做出来。");
  return { title, description: t(description), openGraph: { title, description: t(description), type: "website", locale: locale.replace("-", "_") } };
}

export default function Home() {
  return (
    <main id="site-main" className="site-home" tabIndex={-1}>
      <section className="site-home-hero site-container" aria-labelledby="home-heading">
        <div className="site-home-copy">
          <h1 id="home-heading"><T>{"想清楚。"}</T><br /><T>{"做出来。"}</T></h1>
          <p><T>{"一个开源、可自托管的个人 AI 工作区。"}</T><br className="site-wide-break" /><span className="site-sentence-space">{" "}</span><T>{"把目标交给 Lester，留下网页、文档与代码。"}</T></p>
          <div className="site-home-actions">
            <Link className="site-button site-button-dark" href="/app" prefetch={false}><T>{"开始使用"}</T><ArrowUpRight size={20} aria-hidden="true" /></Link>
          </div>
        </div>
      </section>
      <section id="why-lester" className="site-home-story site-container" aria-labelledby="story-heading">
        <h2 id="story-heading"><T>{"为什么叫 Lester？"}</T></h2>
        <div className="site-home-story-copy">
          <p><T>{"名字来自 GTA V 的 Lester Crest。"}</T><br className="site-wide-break" /><span className="site-sentence-space">{" "}</span><T>{"那个冷静、机敏，总能找到办法的幕后高手。"}</T></p>
          <p><T>{"你说目标，Lester 想办法，让结果落在文件里。"}</T></p>
        </div>
      </section>
    </main>
  );
}
