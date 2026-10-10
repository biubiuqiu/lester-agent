"use client";
import { LanguageSelect } from "@/components/i18n";
import { T, useT } from "@/components/i18n";

import Link from "next/link";
import { ArrowUpRight } from "lucide-react";
import { Brand } from "@/components/brand";
import { issuesURL, repositoryURL } from "@/lib/site";

export function SiteBrand() {
  const t = useT();

  return <Link className="site-brand" href="/" aria-label={t("Lester Agent 首页")}><Brand /><span>Agent</span></Link>;
}

export function SiteHeader() {
  const t = useT();

  return <header className="site-header site-container">
    <SiteBrand />
    <nav aria-label={t("官网导航")}>
      <Link href="/#why-lester"><T>{"关于 Lester"}</T></Link>
      <Link href="/docs"><T>{"帮助文档"}</T></Link>
      <a href={repositoryURL} target="_blank" rel="noopener noreferrer">GitHub<ArrowUpRight size={15} aria-hidden="true" /><span className="site-sr-only"><T>{"（新窗口）"}</T></span></a>
    </nav>
    <LanguageSelect compact /><Link className="site-button site-button-dark site-workspace-link" href="/app" prefetch={false}><T>{"进入工作区"}</T></Link>
  </header>;
}

export function SiteFooter() {
  const t = useT();

  return <footer className="site-footer">
    <div className="site-container site-footer-inner">
      <div><SiteBrand /><p><T>{"开源，自托管。"}</T></p></div>
      <nav aria-label={t("页脚导航")}>
        <Link href="/docs"><T>{"帮助文档"}</T></Link>
        <a href={repositoryURL} target="_blank" rel="noopener noreferrer">GitHub<ArrowUpRight size={14} aria-hidden="true" /><span className="site-sr-only"><T>{"（新窗口）"}</T></span></a>
        <a href={issuesURL} target="_blank" rel="noopener noreferrer"><T>{"报告问题"}</T><ArrowUpRight size={14} aria-hidden="true" /><span className="site-sr-only"><T>{"（新窗口）"}</T></span></a>
      </nav>
    </div>
  </footer>;
}
