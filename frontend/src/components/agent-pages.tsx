"use client";
import { T, useT } from "@/components/i18n";

import { GuideLauncher } from "./user-guide";

import { useEffect, useState } from "react";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { ArrowLeft, ArrowRight, Bot, Plus, Trash2 } from "lucide-react";
import { api, type Agent, type AgentFile } from "@/lib/api";

function Header({ title }: { title: string }) { return <header className="agent-page-header"><Link href="/app"><ArrowLeft size={17} /><T>{"工作区"}</T></Link><span><Bot size={18} />{title}</span><GuideLauncher /></header>; }

export function AgentCatalog() {
  const t = useT();

  const [items,setItems]=useState<Agent[]>([]); const [error,setError]=useState(""); const [loading,setLoading]=useState(true);
  useEffect(()=>{let active=true;api<{agents:Agent[]}>("/api/v1/agents").then(r=>{if(active)setItems(r.agents)}).catch(e=>{if(active)setError(e instanceof Error?e.message:"Agent 加载失败")}).finally(()=>{if(active)setLoading(false)});return()=>{active=false};},[]);
  return <main className="agent-page"><Header title={t("Agent 管理")}/><div className="agent-page-body"><div className="agent-page-title"><div><p className="eyebrow">Agents</p><h1><T>{"为不同工作，准备合适的 Agent"}</T></h1><p><T>{"先和智能体设计师聊需求，再一起确定提示词、Skill 和文件。"}</T></p></div><Link className="primary-button" href="/app/agents/new"><Plus size={16}/><T>{"与智能体设计师创建"}</T></Link></div>{error&&<p role="alert" className="settings-error">{t(error)}</p>}{loading?<p><T>{"正在加载…"}</T></p>:<div className="agent-card-grid">{items.map(a=><Link href={`/app/agents/${encodeURIComponent(a.slug)}`} className="agent-card" key={a.slug}><span className="agent-card-icon"><Bot size={22}/></span><div><small>{a.builtin?t("内置 Agent"):t("我的 Agent")}</small><h2>{a.builtin ? t(a.name) : a.name}</h2><p>{(a.builtin ? t(a.description) : a.description)||t("尚未填写简介")}</p></div><span className="agent-card-foot">{a.skill_slugs.length?t("{0} 个 Skill", [a.skill_slugs.length]):t("通用能力")}<ArrowRight size={16}/></span></Link>)}</div>}</div></main>;
}

export function AgentLanding({ slug }: { slug:string }) {
  const t = useT();

  const [item,setItem]=useState<Agent|null>(null);const [files,setFiles]=useState<AgentFile[]>([]);const [error,setError]=useState("");const [busy,setBusy]=useState(false);const [confirm,setConfirm]=useState(false);const router=useRouter();
  useEffect(()=>{let active=true;api<Agent>(`/api/v1/agents/${encodeURIComponent(slug)}`).then(async a=>{if(!active)return;setItem(a);if(!a.builtin){const result=await api<{files:AgentFile[]}>(`/api/v1/agents/${a.id}/files`);if(active)setFiles(result.files)}}).catch(e=>{if(active)setError(e instanceof Error?e.message:"Agent 不存在")});return()=>{active=false};},[slug]);
  async function remove(){if(!item||item.builtin||busy)return;setBusy(true);setError("");try{await api(`/api/v1/agents/${item.id}`,{method:"DELETE",body:JSON.stringify({version:item.version})});router.push("/app/agents");}catch(e){setError(e instanceof Error?e.message:"删除失败");setBusy(false)}}
  return <main className="agent-page"><Header title={t("Agent 详情")}/><div className="agent-page-body">{error&&<p role="alert" className="settings-error">{t(error)}</p>}{item?<div className="agent-landing"><span className="agent-landing-icon"><Bot size={34}/></span><small>{item.builtin?t("内置 Agent"):t("我的 Agent")}</small><h1>{item.builtin ? t(item.name) : item.name}</h1><p className="agent-landing-description">{item.builtin ? t(item.description) : item.description}</p><div className="agent-landing-actions"><Link className="primary-button" href={item.slug==="agent-designer"?"/app/agents/new":`/app?agent=${encodeURIComponent(item.slug)}`}>{item.slug==="agent-designer"?t("开始设计智能体"):t("和 {0} 开始会话", [item.builtin ? t(item.name) : item.name])} <ArrowRight size={16}/></Link>{!item.builtin&&<Link className="secondary-button" href={item.builder_conversation_id?`/app/c/${item.builder_conversation_id}`:`/app/agents/${encodeURIComponent(slug)}/edit`}><T>{"编辑 Agent"}</T></Link>}</div><section><h2><T>{"这个 Agent 如何工作"}</T></h2><p>{item.builtin?t(item.description):item.instructions}</p></section><section><h2><T>{"预设 Skill"}</T></h2><p>{item.skill_slugs.length?item.skill_slugs.join(" · "):t("没有预设 Skill；会话中仍可自行安装。")}</p></section>{!item.builtin&&<section><h2><T>{"Agent 文件"}</T></h2><p>{files.length?files.map(file=>file.name).join(" · "):t("尚未添加文件。可以在编辑页上传。")}</p></section>}{!item.builtin&&<div className="agent-danger">{confirm?<><span><T>{"删除后无法恢复 Agent，但已有会话保留原有设置。"}</T></span><button type="button" disabled={busy} onClick={()=>setConfirm(false)}><T>{"取消"}</T></button><button type="button" disabled={busy} onClick={()=>void remove()}><T>{"确认删除"}</T></button></>:<button type="button" onClick={()=>setConfirm(true)}><Trash2 size={15}/><T>{"删除 Agent"}</T></button>}</div>}</div>:!error?<p><T>{"正在加载…"}</T></p>:null}</div></main>;
}
