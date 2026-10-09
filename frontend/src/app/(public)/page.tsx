import type { Metadata } from "next";
import Link from "next/link";
import { ArrowRight, BookOpen, FileText, Folder, Github, MessageSquare } from "lucide-react";
import { CodeBlock } from "@/components/site/code-block";
import { ProductDemo, ResultsExample } from "@/components/site/product-demo";
import { repositoryURL, setupCommands } from "@/lib/site";

const description = "Lester 是一个开源、可自托管的 AI Agent 工作区。描述目标，让 Agent 在你的 Computer 中执行任务，产出网页、文档与代码，并围绕成果继续修改。";
export const metadata: Metadata = { title: "Lester Agent · 把想法变成看得见的成果", description, openGraph: { title: "Lester Agent", description, type: "website", locale: "zh_CN" } };

export default function Home() {
  return <main id="site-main">
    <section className="site-hero-band">
      <div className="site-container site-hero">
        <div className="site-hero-copy"><h1>把想法，变成<span>看得见的成果。</span></h1><p>一个开源、可自托管的 AI Agent 工作区。让 Lester 执行任务，在你的 Computer 中产出网页、文档与代码，接着一起把它做好。</p><div className="site-hero-actions"><Link className="site-button site-button-dark" href="/app" prefetch={false}>开始使用<ArrowRight size={19} aria-hidden="true" /></Link><a className="site-button site-button-outline" href={repositoryURL} target="_blank" rel="noopener noreferrer"><Github size={21} aria-hidden="true" />查看 GitHub<span className="site-sr-only">（新窗口）</span></a></div><small>从一项任务开始，所有成果留在你的工作区。</small></div>
        <ProductDemo />
      </div>
      <div className="site-container site-feature-strip">{[
        { icon: MessageSquare, title: "描述目标，交给 Agent", text: "用自然语言说清任务，跟随执行过程查看实际输出。" },
        { icon: Folder, title: "文件与终端，在你的 Computer", text: "在自己的持久工作区里，执行命令、整理与修改文件。" },
        { icon: FileText, title: "预览成果，接着修改", text: "打开网页与文档，围绕具体成果继续把它做好。" },
      ].map(({ icon: Icon, title, text }) => <div key={title}><span><Icon size={25} aria-hidden="true" /></span><div><h2>{title}</h2><p>{text}</p></div></div>)}</div>
    </section>
    <section id="features" className="site-features-band"><div className="site-features site-container">
      <div className="site-feature-content"><h2>从一句话，到一份成果。</h2><p className="site-section-intro">让任务、执行过程和最终文件，留在同一个工作区。</p><ol>{[
        ["交代目标，看见执行", "选择模型、上传材料，跟随真实的工具活动，也可以随时停止。"],
        ["一个属于你的 Computer", "在会话目录里执行命令、整理文件。文件与终端就在手边。"],
        ["围绕成果，继续工作", "预览网页与文档，下载文件，引用具体成果继续修改。发布 HTML 时，由你明确决定。"],
      ].map(([title, text], index) => <li key={title}><span>{String(index + 1).padStart(2, "0")}</span><div><h3>{title}</h3><p>{text}</p></div></li>)}</ol></div>
      <ResultsExample />
      <div className="site-capability-rail"><Link href="/docs/models">自由选择模型<ArrowRight size={14} aria-hidden="true" /></Link><Link href="/docs/usage#skills">安装会话 Skills<ArrowRight size={14} aria-hidden="true" /></Link><Link href="/docs/usage#agents">自定义 Agent<ArrowRight size={14} aria-hidden="true" /></Link><Link href="/docs/deployment">可自托管<ArrowRight size={14} aria-hidden="true" /></Link></div>
    </div></section>
    <section id="getting-started" className="site-start-band"><div className="site-container site-start"><div><h2>在自己的环境里，<br />开始第一项任务。</h2><p>用 Docker Compose 部署 Lester，连接你选择的模型，打开工作区开始使用。</p><Link className="site-button site-button-dark" href="/docs">阅读部署指南<ArrowRight size={19} aria-hidden="true" /></Link></div><CodeBlock label="Docker Compose">{setupCommands}</CodeBlock></div></section>
    <section className="site-features-band"><div className="site-resources site-container"><h2>需要帮助？从这里开始。</h2><div>{[
      { title: "快速开始", text: "部署、配置模型，完成你的第一项任务。", href: "/docs", icon: FileText },
      { title: "使用指南", text: "了解会话、Computer、成果与 Skills。", href: "/docs/usage", icon: BookOpen },
      { title: "参与项目", text: "阅读源码、提交问题，和我们一起改进 Lester。", href: repositoryURL, icon: Github },
    ].map(({ title, text, href, icon: Icon }) => <a key={title} href={href} {...(href.startsWith("https:") ? { target: "_blank", rel: "noopener noreferrer" } : {})}><span className="site-resource-icon"><Icon size={25} aria-hidden="true" /></span><h3>{title}</h3><p>{text}</p><ArrowRight size={23} aria-hidden="true" />{href.startsWith("https:") ? <span className="site-sr-only">（新窗口）</span> : null}</a>)}</div></div></section>
  </main>;
}
