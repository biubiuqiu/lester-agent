"use client";
import { T, useT } from "@/components/i18n";


import { useRef, useState } from "react";
import Link from "next/link";
import { ArrowRight, Code2, Eye, FileText, Folder, Globe, MessageSquare, Pencil, Terminal } from "lucide-react";
import { Brand } from "@/components/brand";

type Example = "website" | "document";

function WebsiteExample({ miniature = false }: { miniature?: boolean }) {
  return <div className={`site-example-website ${miniature ? "miniature" : ""}`}>
    <div className="site-example-brand"><span className="site-example-mark" aria-hidden="true" /><strong>Lester Agent</strong></div>
    <div className="site-example-website-copy"><h3><T>{"让灵感"}</T><span><T>{"落地。"}</T></span></h3><p><T>{"在你的 Computer 中，和 AI Agent 一起"}</T><br /><T>{"把想法变成真实可用的网页、文档与代码。"}</T></p>{miniature ? <span className="site-example-cta"><T>{"开始使用"}</T><ArrowRight size={13} aria-hidden="true" /></span> : <Link href="/docs" className="site-example-cta"><T>{"开始使用"}</T><ArrowRight size={13} aria-hidden="true" /></Link>}</div>
    <div className="site-example-sheet" aria-hidden="true"><div><i /><i /><i /></div><section><span /><p><i /><i /><i /></p></section></div>
  </div>;
}

function DocumentExample({ miniature = false }: { miniature?: boolean }) {
  return <div className={`site-example-document ${miniature ? "miniature" : ""}`}>
    <h3><T>{"产品需求报告"}</T></h3>
    <p><T>{"让任务执行与成果交付，留在同一个工作区。"}</T></p>
    <h4><T>{"一、项目背景"}</T></h4><p><T>{"为产品制作介绍网站，并整理一份可以继续完善的需求文档。"}</T></p>
    <h4><T>{"二、核心需求"}</T></h4><ul><li><T>{"用清晰的页面介绍产品"}</T></li><li><T>{"预览网页与文档成果"}</T></li><li><T>{"围绕文件继续修改"}</T></li></ul>
    <h4><T>{"三、待检查项目"}</T></h4><p><T>{"复核内容、检查页面，再决定是否发布。"}</T></p>
  </div>;
}

export function ProductDemo() {
  const t = useT();

  const [selected, setSelected] = useState<Example>("website");
  return <section className="site-product-demo" aria-label={t("Lester 工作区示例")}>
    <div className="site-demo-window-bar" aria-hidden="true"><i /><i /><i /></div>
    <div className="site-demo-toolbar"><Brand /><span><T>{"示例工作区"}</T></span><div aria-hidden="true"><Terminal size={16} /><Folder size={16} /></div></div>
    <div className="site-demo-workspace">
      <div className="site-demo-sidebar" aria-hidden="true"><strong><T>{"产品发布"}</T></strong><span className="active"><MessageSquare size={13} /><T>{"制作产品介绍"}</T></span><span><MessageSquare size={13} /><T>{"改进文档"}</T></span><span><MessageSquare size={13} /><T>{"生成演示页面"}</T></span></div>
      <div className="site-demo-conversation"><div className="site-demo-speaker"><span><T>{"我"}</T></span><T>{"我"}</T></div><p className="site-demo-message"><T>{"做一个简洁的产品介绍网站。"}</T></p><div className="site-demo-speaker agent"><span>L</span>Lester</div><p className="site-demo-message"><T>{"页面已生成，可以预览或继续修改。"}</T></p><div className="site-demo-file"><Code2 size={18} /><div><strong>index.html</strong><small><T>{"网页文件"}</T></small></div></div><div className="site-demo-file"><FileText size={18} /><div><strong>report.md</strong><small><T>{"文档文件"}</T></small></div></div></div>
      <div className="site-demo-preview"><div className="site-demo-preview-label"><Eye size={14} aria-hidden="true" />{selected === "website" ? t("网页预览") : t("文档预览")}</div><div id="hero-example-output" role="tabpanel" aria-labelledby={`hero-example-${selected}`}>{selected === "website" ? <WebsiteExample /> : <DocumentExample />}</div></div>
    </div>
    <div className="site-demo-bottom"><span><T>{"示例任务"}</T></span><div role="tablist" aria-label={t("示例成果类型")}>{(["website", "document"] as const).map((value) => <button key={value} id={`hero-example-${value}`} type="button" role="tab" aria-controls="hero-example-output" aria-selected={selected === value} tabIndex={selected === value ? 0 : -1} onClick={() => setSelected(value)} onKeyDown={(event) => { if (event.key === "ArrowRight" || event.key === "ArrowLeft") { event.preventDefault(); const next = selected === "website" ? "document" : "website"; setSelected(next); document.getElementById(`hero-example-${next}`)?.focus(); } }}>{value === "website" ? t("网页") : t("文档")}</button>)}</div></div>
  </section>;
}

export function ResultsExample() {
  const t = useT();

  const [preview, setPreview] = useState<Example | null>(null);
  const [reference, setReference] = useState("index.html");
  const [draft, setDraft] = useState(t("把首屏做得更简洁，保留已有文字。"));
  const input = useRef<HTMLTextAreaElement>(null);
  return <section className="site-results-example" aria-label={t("成果与修改示例")}>
    <header><h3><T>{"会话成果"}</T></h3><span><T>{"示例 · 当前文件"}</T></span></header>
    <div className="site-result-cards">{(["website", "document"] as const).map((kind) => {
      const website = kind === "website";
      return <article key={kind}>
        <header>{website ? <Globe size={23} aria-hidden="true" /> : <FileText size={23} aria-hidden="true" />}<div><h4>{website ? t("产品介绍网站") : t("产品需求报告")}</h4><p>{website ? "index.html" : "report.md"}</p></div></header>
        <button className="site-result-thumbnail" type="button" onClick={() => setPreview(kind)} aria-label={t("预览示例 {0}", [website ? "index.html" : "report.md"])}><div inert>{website ? <WebsiteExample miniature /> : <DocumentExample miniature />}</div></button>
        <div className="site-result-actions"><button type="button" onClick={() => setPreview(kind)}><Eye size={15} aria-hidden="true" /><T>{"预览"}</T></button><button type="button" onClick={() => { setReference(website ? "index.html" : "report.md"); input.current?.focus(); }}><Pencil size={15} aria-hidden="true" /><T>{"继续修改"}</T></button></div>
      </article>;
    })}</div>
    {preview ? <div className="site-result-open-preview"><div><strong>{preview === "website" ? "index.html" : "report.md"} <T>{"· 示例预览"}</T></strong><button type="button" onClick={() => setPreview(null)}><T>{"收起预览"}</T></button></div>{preview === "website" ? <WebsiteExample /> : <DocumentExample />}</div> : null}
    <div className="site-example-draft"><div><FileText size={15} aria-hidden="true" /><span><T>{"引用文件："}</T><strong>{reference}</strong></span><small><T>{"示例草稿"}</T></small></div><textarea ref={input} value={draft} onChange={(event) => setDraft(event.target.value)} aria-label={t("示例修改草稿")} rows={2} /></div>
  </section>;
}
