"use client";

import { useEffect, useState } from "react";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { ArrowLeft, ArrowRight, Bot, Plus, Trash2 } from "lucide-react";
import { api, type Agent, type AgentFile } from "@/lib/api";

function Header({ title }: { title: string }) { return <header className="agent-page-header"><Link href="/app"><ArrowLeft size={17} />工作区</Link><span><Bot size={18} />{title}</span></header>; }

export function AgentCatalog() {
  const [items,setItems]=useState<Agent[]>([]); const [error,setError]=useState(""); const [loading,setLoading]=useState(true);
  useEffect(()=>{let active=true;api<{agents:Agent[]}>("/api/v1/agents").then(r=>{if(active)setItems(r.agents)}).catch(e=>{if(active)setError(e instanceof Error?e.message:"Agent 加载失败")}).finally(()=>{if(active)setLoading(false)});return()=>{active=false};},[]);
  return <main className="agent-page"><Header title="Agent 管理"/><div className="agent-page-body"><div className="agent-page-title"><div><p className="eyebrow">Agents</p><h1>为不同工作，准备合适的 Agent</h1><p>先和智能体设计师聊需求，再一起确定提示词、Skill 和文件。</p></div><Link className="primary-button" href="/app/agents/new"><Plus size={16}/>与智能体设计师创建</Link></div>{error&&<p role="alert" className="settings-error">{error}</p>}{loading?<p>正在加载…</p>:<div className="agent-card-grid">{items.map(a=><Link href={`/app/agents/${encodeURIComponent(a.slug)}`} className="agent-card" key={a.slug}><span className="agent-card-icon"><Bot size={22}/></span><div><small>{a.builtin?"内置 Agent":"我的 Agent"}</small><h2>{a.name}</h2><p>{a.description||"尚未填写简介"}</p></div><span className="agent-card-foot">{a.skill_slugs.length?`${a.skill_slugs.length} 个 Skill`:"通用能力"}<ArrowRight size={16}/></span></Link>)}</div>}</div></main>;
}

export function AgentLanding({ slug }: { slug:string }) {
  const [item,setItem]=useState<Agent|null>(null);const [files,setFiles]=useState<AgentFile[]>([]);const [error,setError]=useState("");const [busy,setBusy]=useState(false);const [confirm,setConfirm]=useState(false);const router=useRouter();
  useEffect(()=>{let active=true;api<Agent>(`/api/v1/agents/${encodeURIComponent(slug)}`).then(async a=>{if(!active)return;setItem(a);if(!a.builtin){const result=await api<{files:AgentFile[]}>(`/api/v1/agents/${a.id}/files`);if(active)setFiles(result.files)}}).catch(e=>{if(active)setError(e instanceof Error?e.message:"Agent 不存在")});return()=>{active=false};},[slug]);
  async function remove(){if(!item||item.builtin||busy)return;setBusy(true);setError("");try{await api(`/api/v1/agents/${item.id}`,{method:"DELETE",body:JSON.stringify({version:item.version})});router.push("/app/agents");}catch(e){setError(e instanceof Error?e.message:"删除失败");setBusy(false)}}
  return <main className="agent-page"><Header title="Agent 详情"/><div className="agent-page-body">{error&&<p role="alert" className="settings-error">{error}</p>}{item?<div className="agent-landing"><span className="agent-landing-icon"><Bot size={34}/></span><small>{item.builtin?"内置 Agent":"我的 Agent"}</small><h1>{item.name}</h1><p className="agent-landing-description">{item.description}</p><div className="agent-landing-actions"><Link className="primary-button" href={item.slug==="agent-designer"?"/app/agents/new":`/app?agent=${encodeURIComponent(item.slug)}`}>{item.slug==="agent-designer"?"开始设计智能体":`和 ${item.name} 开始会话`} <ArrowRight size={16}/></Link>{!item.builtin&&<Link className="secondary-button" href={item.builder_conversation_id?`/app/c/${item.builder_conversation_id}`:`/app/agents/${encodeURIComponent(slug)}/edit`}>编辑 Agent</Link>}</div><section><h2>这个 Agent 如何工作</h2><p>{item.builtin?item.description:item.instructions}</p></section><section><h2>预设 Skill</h2><p>{item.skill_slugs.length?item.skill_slugs.join(" · "):"没有预设 Skill；会话中仍可自行安装。"}</p></section>{!item.builtin&&<section><h2>Agent 文件</h2><p>{files.length?files.map(file=>file.name).join(" · "):"尚未添加文件。可以在编辑页上传。"}</p></section>}{!item.builtin&&<div className="agent-danger">{confirm?<><span>删除后无法恢复 Agent，但已有会话保留原有设置。</span><button type="button" disabled={busy} onClick={()=>setConfirm(false)}>取消</button><button type="button" disabled={busy} onClick={()=>void remove()}>确认删除</button></>:<button type="button" onClick={()=>setConfirm(true)}><Trash2 size={15}/>删除 Agent</button>}</div>}</div>:!error?<p>正在加载…</p>:null}</div></main>;
}
