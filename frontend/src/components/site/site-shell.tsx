import Link from "next/link";
import { ArrowUpRight } from "lucide-react";
import { Brand } from "@/components/brand";
import { issuesURL, repositoryURL } from "@/lib/site";

export function SiteBrand() {
  return <Link className="site-brand" href="/" aria-label="Lester Agent 首页"><Brand /><span>Agent</span></Link>;
}

export function SiteHeader() {
  return <header className="site-header site-container">
    <SiteBrand />
    <nav aria-label="官网导航">
      <Link href="/#why-lester">关于 Lester</Link>
      <Link href="/docs">文档</Link>
      <a href={repositoryURL} target="_blank" rel="noopener noreferrer">GitHub<ArrowUpRight size={15} aria-hidden="true" /><span className="site-sr-only">（新窗口）</span></a>
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
        <a href={repositoryURL} target="_blank" rel="noopener noreferrer">GitHub<ArrowUpRight size={14} aria-hidden="true" /><span className="site-sr-only">（新窗口）</span></a>
        <a href={issuesURL} target="_blank" rel="noopener noreferrer">报告问题<ArrowUpRight size={14} aria-hidden="true" /><span className="site-sr-only">（新窗口）</span></a>
      </nav>
    </div>
  </footer>;
}
