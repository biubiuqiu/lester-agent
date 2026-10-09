"use client";

import { useRef, useState } from "react";
import Link from "next/link";
import { ArrowRight, Code2, Eye, FileText, Folder, Globe, MessageSquare, Pencil, Terminal } from "lucide-react";
import { Brand } from "@/components/brand";

type Example = "website" | "document";

function WebsiteExample({ miniature = false }: { miniature?: boolean }) {
  return <div className={`site-example-website ${miniature ? "miniature" : ""}`}>
    <div className="site-example-brand"><span className="site-example-mark" aria-hidden="true" /><strong>Lester Agent</strong></div>
    <div className="site-example-website-copy"><h3>让灵感<span>落地。</span></h3><p>在你的 Computer 中，和 AI Agent 一起<br />把想法变成真实可用的网页、文档与代码。</p>{miniature ? <span className="site-example-cta">开始使用<ArrowRight size={13} aria-hidden="true" /></span> : <Link href="/docs" className="site-example-cta">开始使用<ArrowRight size={13} aria-hidden="true" /></Link>}</div>
    <div className="site-example-sheet" aria-hidden="true"><div><i /><i /><i /></div><section><span /><p><i /><i /><i /></p></section></div>
  </div>;
}

function DocumentExample({ miniature = false }: { miniature?: boolean }) {
  return <div className={`site-example-document ${miniature ? "miniature" : ""}`}>
    <h3>产品需求报告</h3>
    <p>让任务执行与成果交付，留在同一个工作区。</p>
    <h4>一、项目背景</h4><p>为产品制作介绍网站，并整理一份可以继续完善的需求文档。</p>
    <h4>二、核心需求</h4><ul><li>用清晰的页面介绍产品</li><li>预览网页与文档成果</li><li>围绕文件继续修改</li></ul>
    <h4>三、待检查项目</h4><p>复核内容、检查页面，再决定是否发布。</p>
  </div>;
}

export function ProductDemo() {
  const [selected, setSelected] = useState<Example>("website");
  return <section className="site-product-demo" aria-label="Lester 工作区示例">
    <div className="site-demo-window-bar" aria-hidden="true"><i /><i /><i /></div>
    <div className="site-demo-toolbar"><Brand /><span>示例工作区</span><div aria-hidden="true"><Terminal size={16} /><Folder size={16} /></div></div>
    <div className="site-demo-workspace">
      <div className="site-demo-sidebar" aria-hidden="true"><strong>产品发布</strong><span className="active"><MessageSquare size={13} />制作产品介绍</span><span><MessageSquare size={13} />改进文档</span><span><MessageSquare size={13} />生成演示页面</span></div>
      <div className="site-demo-conversation"><div className="site-demo-speaker"><span>我</span>我</div><p className="site-demo-message">做一个简洁的产品介绍网站。</p><div className="site-demo-speaker agent"><span>L</span>Lester</div><p className="site-demo-message">页面已生成，可以预览或继续修改。</p><div className="site-demo-file"><Code2 size={18} /><div><strong>index.html</strong><small>网页文件</small></div></div><div className="site-demo-file"><FileText size={18} /><div><strong>report.md</strong><small>文档文件</small></div></div></div>
      <div className="site-demo-preview"><div className="site-demo-preview-label"><Eye size={14} aria-hidden="true" />{selected === "website" ? "网页预览" : "文档预览"}</div><div id="hero-example-output" role="tabpanel" aria-labelledby={`hero-example-${selected}`}>{selected === "website" ? <WebsiteExample /> : <DocumentExample />}</div></div>
    </div>
    <div className="site-demo-bottom"><span>示例任务</span><div role="tablist" aria-label="示例成果类型">{(["website", "document"] as const).map((value) => <button key={value} id={`hero-example-${value}`} type="button" role="tab" aria-controls="hero-example-output" aria-selected={selected === value} tabIndex={selected === value ? 0 : -1} onClick={() => setSelected(value)} onKeyDown={(event) => { if (event.key === "ArrowRight" || event.key === "ArrowLeft") { event.preventDefault(); const next = selected === "website" ? "document" : "website"; setSelected(next); document.getElementById(`hero-example-${next}`)?.focus(); } }}>{value === "website" ? "网页" : "文档"}</button>)}</div></div>
  </section>;
}

export function ResultsExample() {
  const [preview, setPreview] = useState<Example | null>(null);
  const [reference, setReference] = useState("index.html");
  const [draft, setDraft] = useState("把首屏做得更简洁，保留已有文字。");
  const input = useRef<HTMLTextAreaElement>(null);
  return <section className="site-results-example" aria-label="成果与修改示例">
    <header><h3>会话成果</h3><span>示例 · 当前文件</span></header>
    <div className="site-result-cards">{(["website", "document"] as const).map((kind) => {
      const website = kind === "website";
      return <article key={kind}>
        <header>{website ? <Globe size={23} aria-hidden="true" /> : <FileText size={23} aria-hidden="true" />}<div><h4>{website ? "产品介绍网站" : "产品需求报告"}</h4><p>{website ? "index.html" : "report.md"}</p></div></header>
        <button className="site-result-thumbnail" type="button" onClick={() => setPreview(kind)} aria-label={`预览示例 ${website ? "index.html" : "report.md"}`}><div inert>{website ? <WebsiteExample miniature /> : <DocumentExample miniature />}</div></button>
        <div className="site-result-actions"><button type="button" onClick={() => setPreview(kind)}><Eye size={15} aria-hidden="true" />预览</button><button type="button" onClick={() => { setReference(website ? "index.html" : "report.md"); input.current?.focus(); }}><Pencil size={15} aria-hidden="true" />继续修改</button></div>
      </article>;
    })}</div>
    {preview ? <div className="site-result-open-preview"><div><strong>{preview === "website" ? "index.html" : "report.md"} · 示例预览</strong><button type="button" onClick={() => setPreview(null)}>收起预览</button></div>{preview === "website" ? <WebsiteExample /> : <DocumentExample />}</div> : null}
    <div className="site-example-draft"><div><FileText size={15} aria-hidden="true" /><span>引用文件：<strong>{reference}</strong></span><small>示例草稿</small></div><textarea ref={input} value={draft} onChange={(event) => setDraft(event.target.value)} aria-label="示例修改草稿" rows={2} /></div>
  </section>;
}
