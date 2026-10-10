"use client";
import { T, useT } from "@/components/i18n";

import Link from "next/link";
import { ArrowLeft, ArrowRight, ArrowUpRight, BookOpen, FileText, Github, HelpCircle, MessageSquare, Package, Settings } from "lucide-react";
import { docURL, siteDocs, type SiteDoc } from "@/lib/site-docs";
import { issuesURL, repositoryURL } from "@/lib/site";

const docIcons = [BookOpen, FileText, Settings, Package, HelpCircle];

function DocNavigation({ slug }: { slug: string }) {
  const t = useT();

  return <nav className="site-docs-nav" aria-label={t("文档章节")}>{siteDocs.map((doc, index) => {
    const Icon = docIcons[index];
    return <Link href={docURL(doc.slug)} key={doc.slug} aria-current={doc.slug === slug ? "page" : undefined}><Icon size={19} aria-hidden="true" />{t(doc.navTitle)}</Link>;
  })}</nav>;
}

export function DocsArticle({ doc }: { doc: SiteDoc }) {
  const t = useT();

  const index = siteDocs.findIndex((item) => item.slug === doc.slug);
  const previous = siteDocs[index - 1];
  const next = siteDocs[index + 1];
  return <main className="site-docs" id="site-main">
    <aside className="site-docs-sidebar"><h2><T>{"帮助文档"}</T></h2><DocNavigation slug={doc.slug} /><div className="site-docs-external"><a href={repositoryURL} target="_blank" rel="noopener noreferrer"><Github size={18} aria-hidden="true" /><T>{"GitHub 仓库"}</T><ArrowUpRight size={13} aria-hidden="true" /></a><a href={issuesURL} target="_blank" rel="noopener noreferrer"><MessageSquare size={18} aria-hidden="true" /><T>{"报告问题"}</T><ArrowUpRight size={13} aria-hidden="true" /></a></div></aside>
    <article className="site-docs-article"><details className="site-docs-mobile-nav"><summary><T>{"浏览帮助文档 ·"}</T>{t(doc.navTitle)}</summary><DocNavigation slug={doc.slug} /></details><header><h1>{t(doc.title)}</h1><p>{t(doc.description)}</p>{doc.note ? <small>{t(doc.note)}</small> : null}</header>{doc.sections.map((section) => <section key={section.id} className="site-docs-section" aria-labelledby={section.id}><h2 id={section.id}>{t(section.title)}</h2>{section.content}</section>)}<nav className="site-docs-pagination" aria-label={t("相邻文档")}>{previous ? <Link href={docURL(previous.slug)}><ArrowLeft size={16} aria-hidden="true" />{t(previous.navTitle)}</Link> : <Link href="/"><T>{"返回首页"}</T></Link>}{next ? <Link href={docURL(next.slug)}>{t(next.navTitle)}<ArrowRight size={16} aria-hidden="true" /></Link> : <Link href={docURL("")}><T>{"返回快速开始"}</T><ArrowRight size={16} aria-hidden="true" /></Link>}</nav></article>
    <aside className="site-docs-toc"><strong><T>{"本页内容"}</T></strong><nav aria-label={t("本页内容")}>{doc.sections.map((section) => <a key={section.id} href={`#${section.id}`}>{t(section.title).replace(/^\d\. /, "")}</a>)}</nav></aside>
  </main>;
}
