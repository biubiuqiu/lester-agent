import Link from "next/link";
import { ArrowUpRight, Github } from "lucide-react";
import { Brand } from "@/components/brand";
import { issuesURL, repositoryURL } from "@/lib/site";

export function SiteBrand() {
  return <Link className="site-brand" href="/" aria-label="Lester Agent 首页"><Brand /><span>Agent</span></Link>;
}

export function SiteHeader() {
  return <header className="site-header site-container">
    <SiteBrand />
    <nav aria-label="官网导航">
      <Link href="/#features">功能</Link>
      <Link href="/#getting-started">快速开始</Link>
      <Link href="/docs">帮助文档</Link>
      <a href={repositoryURL} target="_blank" rel="noopener noreferrer"><Github size={18} aria-hidden="true" />GitHub<span className="site-sr-only">（新窗口）</span></a>
    </nav>
    <Link className="site-button site-button-dark site-workspace-link" href="/app" prefetch={false}>进入工作区</Link>
  </header>;
}

export function SiteFooter() {
  return <footer className="site-footer">
    <div className="site-container site-footer-inner">
      <div><SiteBrand /><p>开源，自托管，为你的工作而造。</p></div>
      <nav aria-label="页脚导航">
        <Link href="/docs">帮助文档</Link>
        <a href={repositoryURL} target="_blank" rel="noopener noreferrer">GitHub<ArrowUpRight size={14} aria-hidden="true" /></a>
        <a href={issuesURL} target="_blank" rel="noopener noreferrer">报告问题<ArrowUpRight size={14} aria-hidden="true" /></a>
      </nav>
    </div>
  </footer>;
}
