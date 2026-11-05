"use client";
import { T, useT } from "@/components/i18n";

import Link from "next/link";
import { ArrowLeft, ArrowRight, ArrowUpRight, Github, HelpCircle, MessageSquare } from "lucide-react";
import { docURL, siteDocGroups, siteDocNavigation } from "@/lib/site-doc-navigation";
import type { SiteDoc } from "@/lib/site-docs";
import { issuesURL, repositoryURL } from "@/lib/site";
import { ProductGlyph } from "../product-glyph";
import { DocLegacyNavigation } from "./doc-legacy-navigation";

function DocNavigation({ slug }: { slug: string }) {
  const t = useT();

  return <nav className="site-docs-nav" aria-label={t("文档章节")}>{siteDocGroups.map((group) => <div className="site-docs-nav-group" role="group" aria-label={t(group.title)} key={group.id}>
    <p className="site-docs-nav-label" aria-hidden="true">{t(group.title)}</p>
    {siteDocNavigation.filter((doc) => doc.group === group.id).map((doc) => <Link href={docURL(doc.slug)} prefetch={false} key={doc.slug} aria-current={doc.slug === slug ? "page" : undefined}>{doc.glyph ? <ProductGlyph kind={doc.glyph} size={19} /> : <HelpCircle size={19} aria-hidden="true" />}<span>{t(doc.navTitle)}</span></Link>)}
  </div>)}</nav>;
}

function revealMobileChapter(details: HTMLDetailsElement) {
  if (!details.open) return;
  const nav = details.querySelector<HTMLElement>(".site-docs-nav");
  const current = nav?.querySelector<HTMLElement>('[aria-current="page"]');
  if (!nav || !current) return;
  const bounds = nav.getBoundingClientRect(), selected = current.getBoundingClientRect();
  if (selected.top < bounds.top) nav.scrollTop -= bounds.top - selected.top;
  else if (selected.bottom > bounds.bottom) nav.scrollTop += selected.bottom - bounds.bottom;
}

export function DocsArticle({ doc }: { doc: SiteDoc }) {
  const t = useT();

  const index = siteDocNavigation.findIndex((item) => item.slug === doc.slug);
  const previous = siteDocNavigation[index - 1];
  const next = siteDocNavigation[index + 1];
  return <main className="site-docs" id="site-main">
    <aside className="site-docs-sidebar"><h2><T>{"帮助文档"}</T></h2><DocNavigation slug={doc.slug} /><div className="site-docs-external"><a href={repositoryURL} target="_blank" rel="noopener noreferrer"><Github size={18} aria-hidden="true" /><T>{"GitHub 仓库"}</T><ArrowUpRight size={13} aria-hidden="true" /></a><a href={issuesURL} target="_blank" rel="noopener noreferrer"><MessageSquare size={18} aria-hidden="true" /><T>{"报告问题"}</T><ArrowUpRight size={13} aria-hidden="true" /></a></div></aside>
    <article className="site-docs-article"><details key={doc.slug} className="site-docs-mobile-nav" onToggle={(event) => revealMobileChapter(event.currentTarget)}><summary><T>{"浏览帮助文档 ·"}</T>{t(doc.navTitle)}</summary><DocNavigation slug={doc.slug} /></details><header><h1>{t(doc.title)}</h1><p>{t(doc.description)}</p>{doc.note ? <small>{t(doc.note)}</small> : null}</header>{doc.slug === "usage" ? <DocLegacyNavigation /> : null}{doc.sections.map((section) => <section key={section.id} className="site-docs-section" aria-labelledby={section.id}><h2 id={section.id}>{t(section.title)}</h2>{section.content}</section>)}<nav className="site-docs-pagination" aria-label={t("相邻文档")}>{previous ? <Link href={docURL(previous.slug)} prefetch={false}><ArrowLeft size={16} aria-hidden="true" />{t(previous.navTitle)}</Link> : <Link href="/"><T>{"返回首页"}</T></Link>}{next ? <Link href={docURL(next.slug)} prefetch={false}>{t(next.navTitle)}<ArrowRight size={16} aria-hidden="true" /></Link> : <Link href={docURL("")} prefetch={false}><T>{"返回快速开始"}</T><ArrowRight size={16} aria-hidden="true" /></Link>}</nav></article>
    <aside className="site-docs-toc"><strong><T>{"本页内容"}</T></strong><nav aria-label={t("本页内容")}>{doc.sections.map((section) => <a key={section.id} href={`#${section.id}`}>{t(section.title).replace(/^\d\. /, "")}</a>)}</nav></aside>
  </main>;
}
