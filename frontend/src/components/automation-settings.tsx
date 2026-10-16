"use client";

import { useEffect, useId, useRef, useState, type FormEvent } from "react";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { Clock3, Copy, MoreHorizontal, Plus, RefreshCw, Webhook, X } from "lucide-react";
import { api, API, ApiError, type Agent, type Deployment, type Project } from "@/lib/api";
import { automations, type Automation, type AutomationConfig, type AutomationDelivery } from "@/lib/automation";
import { useI18n, useT } from "./i18n";
import { SettingsShell } from "./settings-shell";

function mergeVersions(current:Automation[],incoming:Automation[]){return incoming.map(item=>current.find(old=>old.id===item.id&&old.revision>item.revision)||item);}

export function AutomationSettings() {
 const t = useT(), { locale } = useI18n();
 const [items, setItems] = useState<Automation[]>([]), [projects, setProjects] = useState<Project[]>([]), [models, setModels] = useState<Deployment[]>([]), [agents, setAgents] = useState<Agent[]>([]);
 const [loading, setLoading] = useState(true), [error, setError] = useState(""), [attempt, setAttempt] = useState(0), [notice, setNotice] = useState("");
 const [editor, setEditor] = useState<Automation | "new" | null>(null), [history, setHistory] = useState<string | null>(null), [busy, setBusy] = useState<string | null>(null), [removing, setRemoving] = useState<Automation | null>(null);
 const mutation = useRef(false), receipts = useRef(new Map<string,string>()), mutationEpoch=useRef(0);
 const [historyRevision,setHistoryRevision]=useState(0);
 useEffect(() => {
  const controller = new AbortController(),epoch=mutationEpoch.current;
  Promise.all([automations.list(controller.signal), api<{projects:Project[]}>("/api/v1/projects",{signal:controller.signal}), api<{deployments:Deployment[]}>("/api/v1/model-deployments",{signal:controller.signal}), api<{agents:Agent[]}>("/api/v1/agents",{signal:controller.signal})])
   .then(([list,projectList,modelList,agentList])=>{if(!controller.signal.aborted){if(epoch===mutationEpoch.current)setItems(old=>mergeVersions(old,list.automations));setProjects(projectList.projects);setModels(modelList.deployments.filter(item=>item.enabled));setAgents(agentList.agents);setError("");}})
   .catch(()=>{if(!controller.signal.aborted)setError(t("自动任务加载失败"));}).finally(()=>{if(!controller.signal.aborted)setLoading(false);});
  return()=>controller.abort();
 },[attempt,t]);
 useEffect(()=>{
  let pending=false;const controller=new AbortController();
  const refresh=async()=>{if(document.hidden||pending)return;pending=true;const epoch=mutationEpoch.current;try{const list=await automations.list(controller.signal);if(!controller.signal.aborted&&epoch===mutationEpoch.current)setItems(old=>mergeVersions(old,list.automations));}catch{if(!controller.signal.aborted)setError(t("自动任务加载失败"));}finally{pending=false;}};
  const timer=setInterval(refresh,15000);document.addEventListener("visibilitychange",refresh);return()=>{controller.abort();clearInterval(timer);document.removeEventListener("visibilitychange",refresh);};
 },[t]);
 const update=(item:Automation)=>{mutationEpoch.current++;setItems(old=>old.some(value=>value.id===item.id&&value.revision>item.revision)?old:[item,...old.filter(value=>value.id!==item.id)]);};
 const run=async(item:Automation)=>{
  if(mutation.current)return;mutation.current=true;setBusy(item.id);setError("");setNotice("");
  const delivery=receipts.current.get(item.id)||crypto.randomUUID();receipts.current.set(item.id,delivery);
  try{
   try{await automations.trigger(item.id,delivery);}catch(reason){if(reason instanceof ApiError&&reason.status<500&&reason.status!==408)throw reason;try{await automations.receipt(item.id,delivery);}catch{throw new Error("unconfirmed");}}
   receipts.current.delete(item.id);setHistory(item.id);setHistoryRevision(value=>value+1);setNotice(t("已加入触发队列"));
  }catch(reason){setError(reason instanceof ApiError&&reason.status<500?t(reason.message):t("触发结果未确认，请检查记录后再操作。"));}
  finally{mutation.current=false;setBusy(null);}
 };
 const toggle=async(item:Automation)=>{if(mutation.current)return;mutation.current=true;setBusy(item.id);setError("");try{const result=await automations.save(item.id,{...item,enabled:!item.enabled},item.revision);update(result.automation);}catch(reason){setError(t(reason instanceof ApiError?reason.message:"操作结果未确认，请刷新后检查。"));}finally{mutation.current=false;setBusy(null);}};
 const remove=async()=>{if(!removing||mutation.current)return;mutation.current=true;setBusy(removing.id);setError("");try{await automations.remove(removing);mutationEpoch.current++;setItems(old=>old.filter(item=>item.id!==removing.id));setRemoving(null);}catch(reason){setError(t(reason instanceof ApiError?reason.message:"操作结果未确认，请刷新后检查。"));}finally{mutation.current=false;setBusy(null);}};
 const stamp=(value:string,zone?:string)=>new Intl.DateTimeFormat(locale,{dateStyle:"medium",timeStyle:"short",...(zone?{timeZone:zone}:{})}).format(new Date(value));
 return <SettingsShell active="automations">
  <header className="settings-heading"><div><p className="eyebrow">Settings / Automations</p><h1>{t("自动任务")}</h1><p>{t("按时间或外部事件，让 Lester 开始工作。")}</p></div><button type="button" className="primary-button" disabled={loading||Boolean(busy)||Boolean(error)&&!projects.length} onClick={()=>setEditor("new")}><Plus size={16}/>{t("新建自动任务")}</button></header>
  {error?<p className="settings-error" role="alert">{error} <button type="button" className="text-button" onClick={()=>{setLoading(true);setAttempt(value=>value+1);}}>{t("刷新")}</button></p>:null}
  {notice?<p className="automation-notice" role="status">{notice}</p>:null}
  {loading?<p role="status">{t("正在加载…")}</p>:null}
  {!loading&&!error&&!items.length?<section className="automation-empty"><Clock3 size={26}/><h2>{t("把下一次工作安排好")}</h2><p>{t("设置定时任务，或让外部系统通过 Webhook 触发。每次执行都会创建独立会话。")}</p></section>:null}
  <div className="automation-list">{items.map(item=><article key={item.id} className="automation-card">
   <header><span className="automation-icon">{item.kind==="schedule"?<Clock3 size={18}/>:<Webhook size={18}/>}</span><div><h2>{item.name}</h2><p>{t(item.kind==="schedule"?"定时触发":"Webhook 触发")}{item.kind==="schedule"?` · ${item.timezone}`:""}</p></div><button type="button" className={`automation-status ${item.enabled?"enabled":""}`} aria-label={t(item.enabled?"暂停自动任务：{0}":"启用自动任务：{0}",[item.name])} aria-pressed={item.enabled} disabled={Boolean(busy)} onClick={()=>void toggle(item)}>{t(item.enabled?"已启用":"已暂停")}</button></header>
   <p className="automation-prompt">{item.prompt}</p>
   <footer><span>{item.kind==="schedule"?(item.next_run_at?t("下次：{0}",[stamp(item.next_run_at,item.timezone)]):item.run_at?`${t("单次")} · ${stamp(item.run_at,item.timezone)}`:item.cron):t("凭证验证后接收 JSON 事件")}</span><div><button type="button" onClick={()=>setHistory(value=>value===item.id?null:item.id)} aria-expanded={history===item.id}>{t("触发记录")}</button><button type="button" disabled={Boolean(busy)} onClick={()=>void run(item)}>{t(busy===item.id?"正在处理…":"立即运行")}</button><details className="automation-more" onKeyDown={event=>{if(event.key==="Escape"){event.preventDefault();event.stopPropagation();event.currentTarget.open=false;event.currentTarget.querySelector<HTMLElement>("summary")?.focus();}}}><summary aria-label={t("更多操作：{0}",[item.name])}><MoreHorizontal size={17}/></summary><div><button type="button" disabled={Boolean(busy)} onClick={event=>{const menu=event.currentTarget.closest("details");menu?.removeAttribute("open");menu?.querySelector<HTMLElement>("summary")?.focus({preventScroll:true});setEditor(item);}}>{t("编辑")}</button><button type="button" disabled={Boolean(busy)} onClick={event=>{const menu=event.currentTarget.closest("details");menu?.removeAttribute("open");menu?.querySelector<HTMLElement>("summary")?.focus({preventScroll:true});setRemoving(item);}}>{t("删除")}</button></div></details></div></footer>
   {history===item.id?<TriggerHistory id={item.id} revision={historyRevision}/>:null}
  </article>)}</div>
  {editor?<AutomationEditor key={typeof editor==="string"?"new":editor.id} item={editor==="new"?null:editor} projects={projects} models={models} agents={agents} onSaved={update} onClose={()=>setEditor(null)}/>:null}
  {removing?<AutomationDialog title={t("删除自动任务")} onClose={()=>{if(!busy)setRemoving(null);}}><p>{t("删除会取消尚未开始的触发，不会停止已进入会话的任务。")}</p><div className="automation-actions"><button type="button" disabled={Boolean(busy)} onClick={()=>setRemoving(null)}>{t("取消")}</button><button type="button" disabled={Boolean(busy)} onClick={()=>void remove()}>{t("确认删除")}</button></div></AutomationDialog>:null}
 </SettingsShell>;
}

function AutomationDialog({title,onClose,children}:{title:string;onClose:()=>void;children:React.ReactNode}){
 const t=useT(),dialog=useRef<HTMLDialogElement>(null);const titleID=useId();
 useEffect(()=>{const previous=document.activeElement as HTMLElement|null;dialog.current?.showModal();return()=>{if(previous?.isConnected)previous.focus({preventScroll:true});else document.querySelector<HTMLElement>(".settings-heading .primary-button")?.focus({preventScroll:true});};},[]);
 return <dialog ref={dialog} className="automation-dialog" aria-labelledby={titleID} onCancel={event=>{event.preventDefault();onClose();}}><header><h2 id={titleID}>{title}</h2><button type="button" onClick={onClose} aria-label={t("关闭")}><X size={19}/></button></header>{children}</dialog>;
}

function AutomationEditor({item,projects,models,agents,onSaved,onClose}:{item:Automation|null;projects:Project[];models:Deployment[];agents:Agent[];onSaved:(item:Automation)=>void;onClose:()=>void}){
 const t=useT();const [id]=useState(()=>item?.id||crypto.randomUUID());const zone=Intl.DateTimeFormat().resolvedOptions().timeZone||"UTC";
 const [config,setConfig]=useState<AutomationConfig>(()=>item||{name:"",kind:"schedule",project_id:projects.find(value=>value.is_default)?.id||projects[0]?.id||"",agent_slug:"lester",model_deployment_id:models.find(value=>value.is_default)?.id||models[0]?.id||"",prompt:"",permission_mode:"standard",enabled:false,cron:"0 9 * * *",timezone:zone,run_at:null});
 const [saved,setSaved]=useState(item),[secret,setSecret]=useState(""),[pending,setPending]=useState(false),[error,setError]=useState(""),[confirmRotate,setConfirmRotate]=useState(false);
 const initialParts=config.cron.split(" ");const [cadence,setCadence]=useState(()=>config.run_at?"once":/^\d+ \d+ \* \* \*$/.test(config.cron)?"daily":/^\d+ \d+ \* \* [0-6]$/.test(config.cron)?"weekly":"cron");
 const [clock,setClock]=useState(()=>`${(/^\d+$/.test(initialParts[1]||"")?initialParts[1]:"9").padStart(2,"0")}:${(/^\d+$/.test(initialParts[0]||"")?initialParts[0]:"0").padStart(2,"0")}`),[weekday,setWeekday]=useState(/^[0-6]$/.test(initialParts[4]||"")?initialParts[4]:"1");
 const [date,setDate]=useState(()=>{if(!config.run_at)return "";const value=new Date(config.run_at);return new Date(value.getTime()-value.getTimezoneOffset()*60000).toISOString().slice(0,16);});
 const mutation=useRef(false),zoneID=useId();
 const change=<K extends keyof AutomationConfig>(key:K,value:AutomationConfig[K])=>setConfig(old=>({...old,[key]:value}));
 const submit=async(event:FormEvent)=>{
  event.preventDefault();if(mutation.current)return;mutation.current=true;setPending(true);setError("");
  try{const [hour,minute]=clock.split(":");const cron=cadence==="daily"?`${Number(minute)} ${Number(hour)} * * *`:cadence==="weekly"?`${Number(minute)} ${Number(hour)} * * ${weekday}`:config.cron;
   const payload={...config,cron:config.kind==="webhook"||cadence==="once"?"":cron,run_at:config.kind==="schedule"&&cadence==="once"?new Date(date).toISOString():null,timezone:cadence==="once"?zone:config.timezone};
   const result=await automations.save(id,payload,saved?.revision||0);setSaved(result.automation);onSaved(result.automation);if(result.secret){setSecret(result.secret);}else onClose();
  }catch(reason){setError(t(reason instanceof ApiError?reason.message:"操作结果未确认，请刷新后检查。"));}finally{mutation.current=false;setPending(false);}
 };
 const rotate=async()=>{if(!saved||mutation.current)return;mutation.current=true;setPending(true);setError("");try{const result=await automations.rotate(saved);setSaved(result.automation);onSaved(result.automation);setSecret(result.secret);setConfirmRotate(false);}catch(reason){setError(t(reason instanceof ApiError?reason.message:"操作结果未确认，请刷新后检查。"));}finally{mutation.current=false;setPending(false);}};
 return <AutomationDialog title={t(item?"编辑自动任务":"新建自动任务")} onClose={()=>{if(!pending)onClose();}}>
  {secret&&saved?<><WebhookInstructions item={saved} secret={secret}/><div className="automation-actions"><button type="button" className="primary" onClick={onClose}>{t("完成")}</button></div></>:<form onSubmit={event=>void submit(event)}>
   <fieldset disabled={pending}>
    <label>{t("名称")}<input autoFocus required maxLength={120} value={config.name} onChange={event=>change("name",event.target.value)}/></label>
    <div className="automation-field-row"><label>{t("触发方式")}<select disabled={Boolean(saved)} value={config.kind} onChange={event=>change("kind",event.target.value as AutomationConfig["kind"])}><option value="schedule">{t("定时触发")}</option><option value="webhook">{t("Webhook 触发")}</option></select></label><label>{t("项目")}<select required value={config.project_id} onChange={event=>change("project_id",event.target.value)}>{projects.map(project=><option key={project.id} value={project.id}>{project.name}</option>)}</select></label></div>
    <label>{t("任务内容")}<textarea required rows={4} maxLength={16000} value={config.prompt} onChange={event=>change("prompt",event.target.value)}/></label>
    <label>{t("模型")}<select required value={config.model_deployment_id} onChange={event=>change("model_deployment_id",event.target.value)}><option value="" disabled>{t("选择模型")}</option>{models.map(model=><option key={model.id} value={model.id}>{model.name}</option>)}</select></label>
    {!models.length?<p><Link href="/app/settings/models">{t("先配置一个可用模型")}</Link></p>:null}
    {config.kind==="schedule"?<><label>{t("重复")}<select value={cadence} onChange={event=>setCadence(event.target.value)}><option value="once">{t("单次")}</option><option value="daily">{t("每天")}</option><option value="weekly">{t("每周")}</option><option value="cron">{t("高级 Cron")}</option></select></label>
     {cadence==="once"?<label>{t("执行时间（本地时间）")}<input type="datetime-local" required value={date} onChange={event=>setDate(event.target.value)}/></label>:<>
      <div className="automation-field-row">{cadence==="cron"?<label>{t("Cron 表达式")}<input required maxLength={100} value={config.cron} placeholder="0 9 * * 1-5" onChange={event=>change("cron",event.target.value)}/></label>:<label>{t("时间")}<input type="time" required value={clock} onChange={event=>setClock(event.target.value)}/></label>}
       {cadence==="weekly"?<label>{t("星期")}<select value={weekday==="*"?"1":weekday} onChange={event=>setWeekday(event.target.value)}>{["星期日","星期一","星期二","星期三","星期四","星期五","星期六"].map((day,index)=><option key={day} value={index}>{t(day)}</option>)}</select></label>:null}
      </div><label>{t("时区")}<input required list={zoneID} maxLength={100} value={config.timezone} onChange={event=>change("timezone",event.target.value)}/><datalist id={zoneID}>{[zone,"UTC","Asia/Shanghai","Asia/Tokyo","Asia/Seoul","Europe/Paris","Europe/Madrid","America/New_York"].filter((value,index,all)=>all.indexOf(value)===index).map(value=><option key={value} value={value}/>)}</datalist></label>
      {cadence==="cron"?<small>{t("五段格式：分钟 小时 日 月 星期；最小间隔一分钟。")}</small>:null}</>}</>:null}
    <details className="automation-advanced"><summary>{t("Agent 和权限")}</summary><label>Agent<select value={config.agent_slug} onChange={event=>change("agent_slug",event.target.value)}>{agents.map(agent=><option key={agent.slug} value={agent.slug}>{agent.name}</option>)}</select></label><label>{t("权限设置")}<select value={config.permission_mode} onChange={event=>change("permission_mode",event.target.value as AutomationConfig["permission_mode"])}><option value="standard">{t("默认权限")}</option><option value="auto">{t("自动审批")}</option><option value="full">{t("完全访问权限")}</option></select></label><small>{t(config.permission_mode==="full"?"在已授权的 Computer 内执行，无需逐次确认。":"需要确认或回答时，任务会等待你在会话中处理。")}</small></details>
    <label className="automation-checkbox"><input type="checkbox" checked={config.enabled} onChange={event=>change("enabled",event.target.checked)}/>{t("启用此自动任务")}</label>
    <small>{t("暂停会取消尚未开始的触发。已进入会话的任务需要在会话中停止。")}</small>
   </fieldset>
   {saved?.kind==="webhook"?<details className="automation-connection"><summary>{t("连接说明")}</summary><WebhookInstructions item={saved}/>{confirmRotate?<div><p>{t("重置后旧密钥立即失效，已接收的事件保留。")}</p><button type="button" disabled={pending} onClick={()=>void rotate()}>{t("确认重置密钥")}</button><button type="button" disabled={pending} onClick={()=>setConfirmRotate(false)}>{t("取消")}</button></div>:<button type="button" disabled={pending} onClick={()=>setConfirmRotate(true)}>{t("重置密钥")}</button>}</details>:null}
   <div className="automation-actions"><button type="button" disabled={pending} onClick={onClose}>{t("取消")}</button><button type="submit" className="primary" disabled={pending||!models.length}>{t(pending?"正在保存…":"保存")}</button></div>
  </form>}
  {error?<p role="alert" className="settings-error">{error}</p>:null}
 </AutomationDialog>;
}

function WebhookInstructions({item,secret}:{item:Automation;secret?:string}){
 const t=useT();const [notice,setNotice]=useState("");const base=API||window.location.origin;const endpoint=`${base}/api/v1/hooks/${item.id}`;
 const copy=async(value:string)=>{try{await navigator.clipboard.writeText(value);setNotice(t("已复制"));}catch{setNotice(t("复制失败，请手动复制。"));}};
 const example=`curl -X POST '${endpoint}' \\\n  -H "Authorization: Bearer $LESTER_WEBHOOK_SECRET" \\\n  -H 'X-Lester-Event-ID: issue-123-created' \\\n  -H 'Content-Type: application/json' \\\n  -d '{"issue":123}'`;
 return <section className="automation-webhook"><h3>{t("Webhook 接收地址")}</h3><code>{endpoint}</code><button type="button" onClick={()=>void copy(endpoint)}><Copy size={14}/>{t("复制地址")}</button>
  {secret?<><p>{t("密钥仅本次显示，请保存到发送方的密钥配置中。")}</p><code className="automation-secret">{secret}</code><button type="button" onClick={()=>void copy(secret)}>{t("复制密钥")}</button></>:null}
  <p>{t("发送 JSON 对象，并提供 Bearer 密钥和唯一事件 ID。同一事件 ID 重发不会启动第二次任务。")}</p><pre>{example}</pre><button type="button" onClick={()=>void copy(example)}>{t("复制示例")}</button>{notice?<p role="status">{notice}</p>:null}
 </section>;
}

function TriggerHistory({id,revision}:{id:string;revision:number}){
 const t=useT(),{locale}=useI18n(),router=useRouter();const [items,setItems]=useState<AutomationDelivery[]|null>(null),[error,setError]=useState(false),[attempt,setAttempt]=useState(0);
 useEffect(()=>{let pending=false;const controller=new AbortController();const load=async()=>{if(pending||document.hidden)return;pending=true;try{const result=await automations.history(id,controller.signal);if(!controller.signal.aborted){setItems(result.deliveries);setError(false);}}catch{if(!controller.signal.aborted)setError(true);}finally{pending=false;}};void load();const timer=setInterval(load,10000);document.addEventListener("visibilitychange",load);return()=>{controller.abort();clearInterval(timer);document.removeEventListener("visibilitychange",load);};},[id,attempt,revision]);
 const states:Record<string,string>={pending:"等待触发",dispatching:"正在创建任务",accepted:"已进入会话",failed:"触发失败",unknown:"触发结果待确认",skipped:"已跳过",cancelled:"已取消",running:"正在工作",cancelling:"正在停止",completed:"已完成",cancelled_run:"已停止",failed_run:"需要检查"};
 const codes:Record<string,string>={paused:"自动任务已暂停，未开始的触发已取消。",deleted:"自动任务已删除。",busy:"上一任务或后台命令仍在运行，本次定时触发已跳过。",receipt_unknown:"未找到已确认的执行回执，请检查会话；系统不会自动重试。",authorization_changed:"账户或工作区权限已变化，未启动执行。"};
 return <section className="automation-history" aria-label={t("触发记录")}><header><h3>{t("最近 20 次触发")}</h3><button type="button" aria-label={t("刷新触发记录")} onClick={()=>setAttempt(value=>value+1)}><RefreshCw size={14}/></button></header>
  {error?<p role="alert">{t("触发记录加载失败")} <button type="button" onClick={()=>setAttempt(value=>value+1)}>{t("重试")}</button></p>:null}
  {!items&&!error?<p role="status">{t("正在加载…")}</p>:null}{items?.length===0&&!error?<p>{t("尚无触发记录")}</p>:null}
  {items?.map(item=><article key={item.id}><div><strong>{t(states[item.state==="accepted"&&item.run_status?(item.run_status==="failed"?"failed_run":item.run_status==="cancelled"?"cancelled_run":item.run_status):item.state]||"状态尚未确认")}</strong><span>{t(item.source==="schedule"?"定时触发":item.source==="webhook"?"Webhook 触发":"手动触发")} · {new Intl.DateTimeFormat(locale,{dateStyle:"short",timeStyle:"short"}).format(new Date(item.created_at))}</span>{item.error_code?<p>{t(codes[item.error_code]||"状态尚未确认")}</p>:null}</div>{item.has_conversation?<button type="button" onClick={()=>router.push(`/app/c/${item.conversation_id}`)}>{t("打开会话")}</button>:null}</article>)}
 </section>;
}
