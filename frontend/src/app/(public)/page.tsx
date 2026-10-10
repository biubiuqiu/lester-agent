import type { Metadata } from "next";
import Link from "next/link";
import { ArrowUpRight } from "lucide-react";

const description = "一个开源、可自托管的个人 AI 工作区。把目标交给 Lester，在你的 Computer 中执行任务，留下网页、文档与代码。";
export const metadata: Metadata = {
  title: "Lester Agent · 想清楚。做出来。",
  description,
  openGraph: { title: "Lester Agent · 想清楚。做出来。", description, type: "website", locale: "zh_CN" },
};

export default function Home() {
  return (
    <main id="site-main" className="site-home" tabIndex={-1}>
      <section className="site-home-hero site-container" aria-labelledby="home-heading">
        <div className="site-home-copy">
          <h1 id="home-heading">想清楚。<br />做出来。</h1>
          <p>一个开源、可自托管的个人 AI 工作区。<br className="site-wide-break" />把目标交给 Lester，留下网页、文档与代码。</p>
          <div className="site-home-actions">
            <Link className="site-button site-button-dark" href="/app" prefetch={false}>开始使用<ArrowUpRight size={20} aria-hidden="true" /></Link>
          </div>
        </div>
      </section>
      <section id="why-lester" className="site-home-story site-container" aria-labelledby="story-heading">
        <h2 id="story-heading">为什么叫 Lester？</h2>
        <div className="site-home-story-copy">
          <p>名字来自 GTA V 的 Lester Crest。<br className="site-wide-break" />那个冷静、机敏，总能找到办法的幕后高手。</p>
          <p>你说目标，Lester 想办法，让结果落在文件里。</p>
        </div>
      </section>
    </main>
  );
}
