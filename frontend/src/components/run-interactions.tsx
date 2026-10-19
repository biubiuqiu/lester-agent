"use client";
import { useEffect, useRef, useState } from "react";
import { createPortal } from "react-dom";
import { useT } from "./i18n";
import { api, ApiError } from "@/lib/api";
import { readView, updateView } from "@/lib/conversation-view-state";
import { sameInteractionResponse } from "@/lib/interaction-response";
import type { QuestionAnswer, UserQuestion } from "@/lib/user-question";
import { StructuredQuestions } from "./structured-questions";
import { ScheduleReview, scheduleActionLabels, type SchedulePreview, type ScheduleAction } from "./schedule-review";
import { ConversationRecord } from "./conversation-presentation";

type Answer = QuestionAnswer;
type Response = { request_digest: string; answers?: Record<string, Answer>; decision?: string; feedback?: string };
type Interaction = {
  id: string; run_id: string; kind: "question" | "plan" | "automation"; request_digest: string;
  status: "pending" | "answered" | "approved" | "rejected" | "changes_requested" | "cancelled";
  request: { title?: string; steps?: string[]; questions?: UserQuestion[]; schedule?: {action: ScheduleAction}; schedule_preview?: SchedulePreview };
  response?: Response;
};
type Interactions = { interactions: Interaction[]; execution_mode: "plan" | "execute" };

export function RunInteractions({ conversationId, storageKey, active, revision, runId, questionTargetId }: { conversationId: string; storageKey: string; active: boolean; revision: number; runId?: string; questionTargetId?: string }) {
  const t = useT();
  const [data, setData] = useState<Interactions>({ interactions: [], execution_mode: "execute" });
  const [error, setError] = useState("");
  const refresh = useRef<() => void>(() => {});
  const currentRun = useRef(runId);
  useEffect(() => { currentRun.current = runId; }, [runId]);
  useEffect(() => {
    let alive = true, pending = false;
    const load = async () => {
      if (pending) return;
      pending = true;
      try {
        const next = await api<Interactions>(`/api/v1/conversations/${conversationId}/interactions`);
        if (alive) { setData(previous => ({ ...next, execution_mode: previous.execution_mode === "execute" && previous.interactions.some(item => item.kind === "plan" && item.run_id === currentRun.current && item.status === "approved") ? "execute" : next.execution_mode, interactions: next.interactions.map(item => {
          const confirmed = previous.interactions.find(old => old.id === item.id && old.status !== "pending");
          return item.status === "pending" && confirmed ? confirmed : item;
        }) })); setError(""); }
      } catch { if (alive) setError(t("待答请求加载失败，请重试。")); }
      finally { pending = false; }
    };
    refresh.current = () => { void load(); };
    void load();
    const visible = () => { if (!document.hidden) void load(); };
    const timer = active ? window.setInterval(visible, 5000) : undefined;
    document.addEventListener("visibilitychange", visible);
    return () => { alive = false; if (timer) window.clearInterval(timer); document.removeEventListener("visibilitychange", visible); };
  }, [conversationId, active, revision, runId, t]);
  const confirmed = (item: Interaction) => {
    setData(previous => ({ ...previous, execution_mode: item.kind === "plan" && item.status === "approved" && item.run_id === currentRun.current ? "execute" : previous.execution_mode, interactions: previous.interactions.map(old => old.id === item.id ? item : old) }));
  };
  if (!error && !data.interactions.length && (!active || data.execution_mode !== "plan")) return null;
  const target = questionTargetId && typeof document !== "undefined" ? document.getElementById(questionTargetId) : null;
  const waiting = data.interactions.filter(item => item.status === "pending");
  const scheduleReceipt = data.interactions.find(item => item.status !== "pending" && item.kind === "automation");
 const history = data.interactions.filter(item => item.status !== "pending" && item.id !== scheduleReceipt?.id);
  return <>
 {scheduleReceipt ? <div className="schedule-receipts"><InteractionCard key={scheduleReceipt.id} item={scheduleReceipt} conversationId={conversationId} storageKey={storageKey} onConfirmed={confirmed}/></div>:null}
    {history.length ? <ConversationRecord slot="questions"><div className="interaction-history">{history.toReversed().map(item => <InteractionCard key={item.id} item={item} conversationId={conversationId} storageKey={storageKey} onConfirmed={confirmed} />)}</div></ConversationRecord> : null}
    {error || waiting.length || active && data.execution_mode === "plan" ? <section className="run-interactions" aria-label={t("任务协作")}>
    {active && data.execution_mode === "plan" ? <p className="interaction-mode">{t("Plan · 仅查看与规划")}</p> : null}
    {error ? <p role="alert">{error} <button type="button" onClick={() => refresh.current()}>{t("重试")}</button></p> : null}
    {waiting.toReversed().map(item => {const card=<InteractionCard key={item.id} item={item} conversationId={conversationId} storageKey={storageKey} onConfirmed={confirmed}/>;return (item.kind==="question"||item.kind==="automation")&&item.status==="pending"&&target?createPortal(card,target,item.id):card;})}
  </section> : null}</>;
}

function InteractionCard({ item, conversationId, storageKey, onConfirmed }: { item: Interaction; conversationId: string; storageKey: string; onConfirmed: (item: Interaction) => void }) {
  const t = useT();
  const initial = readView(storageKey).interactionDrafts[item.id];
  const [answers, setAnswers] = useState<Record<string, Answer>>(initial?.answers || {});
  const [feedback, setFeedback] = useState(initial?.feedback || "");
  const [pending, setPending] = useState(false), [error, setError] = useState("");
  const submitted = useRef(false);
  const save = (nextAnswers: Record<string, Answer>, nextFeedback: string) => {
    const drafts = { ...readView(storageKey).interactionDrafts, [item.id]: { answers: nextAnswers, feedback: nextFeedback } };
    updateView(storageKey, { interactionDrafts: Object.fromEntries(Object.entries(drafts).slice(-8)) });
  };
  const changeAnswer = (id: string, answer: Answer) => { const next = { ...answers, [id]: answer }; setAnswers(next); save(next, feedback); };
  async function respond(response: Response) {
    if (submitted.current || item.status !== "pending") return;
    submitted.current = true; setPending(true); setError("");
    try {
      let received: Interaction;
      try { received = await api<Interaction>(`/api/v1/conversations/${conversationId}/interactions/${item.id}/respond`, { method: "POST", body: JSON.stringify(response) }); }
      catch (reason) {
        if (reason instanceof ApiError && reason.status < 500 && reason.status !== 408) throw reason;
        received = await api<Interaction>(`/api/v1/conversations/${conversationId}/interactions/${item.id}`);
        if (received.status === "pending" || received.status === "cancelled" || !received.response || !sameInteractionResponse(received.response, response)) throw new Error("unconfirmed");
      }
      onConfirmed(received);
      const drafts = { ...readView(storageKey).interactionDrafts }; delete drafts[item.id]; updateView(storageKey, { interactionDrafts: drafts });
    } catch (reason) { setError(t(item.kind === "automation" && reason instanceof ApiError && reason.status === 409 ? "配置已变化或时间已过期，请暂不应用并重新提出配置。" : "提交结果未确认，回答已保留。请检查状态后重试。")); }
    finally { submitted.current = false; setPending(false); }
  }
  const schedule = item.kind === "automation" && item.request.schedule_preview && item.request.schedule;
 const status = item.kind === "automation" ? ({pending:"定时任务等待确认",answered:"已回答",approved:"定时任务配置已应用",rejected:"未应用定时任务",changes_requested:"请调整定时任务",cancelled:"已取消"})[item.status] : ({ pending: item.kind === "plan" ? "计划等待批准" : "等待你的回答", answered: "已回答", approved: "计划已批准", rejected: "计划已拒绝", changes_requested: "请调整计划", cancelled: "已取消" })[item.status];
  if (item.status !== "pending") return <details className={`interaction-receipt ${item.status}`}><summary>{t(status)}{item.request.title || item.request.schedule_preview?.name ? ` · ${item.request.title || item.request.schedule_preview?.name}` : ""}</summary><InteractionContent item={item} /></details>;
  return <article className={`interaction-card ${item.kind==="question"?"question-card":item.kind==="automation"?"schedule-card":""}`} aria-busy={pending} aria-label={item.kind==="question"?t("等待你的回答"):item.kind==="automation"?t("定时任务等待确认"):undefined}>
    {item.kind==="plan"?<h3>{item.request.title}</h3>:null}
    {schedule ? <>
 <div className="schedule-card-body"><ScheduleReview preview={item.request.schedule_preview!} action={item.request.schedule!.action}/>
 <details className="schedule-feedback"><summary>{t("调整意见")}</summary><label><span className="sr-only">{t("调整意见")}</span><textarea rows={2} maxLength={6000} value={feedback} disabled={pending} onChange={event=>{setFeedback(event.target.value);save(answers,event.target.value);}}/></label></details></div>
 <div className="interaction-actions"><button type="button" className="primary" disabled={pending} onClick={()=>void respond(feedback.trim()?{request_digest:item.request_digest,decision:"request_changes",feedback}:{request_digest:item.request_digest,decision:"approve"})}>{t(feedback.trim()?"提交调整":scheduleActionLabels[item.request.schedule!.action])}</button>
 <button type="button" disabled={pending} onClick={()=>void respond({request_digest:item.request_digest,decision:"reject"})}>{t("暂不应用")}</button></div>
 </> : item.kind === "plan" ? <>
      <ol>{item.request.steps?.map((step, index) => <li key={index}>{step}</li>)}</ol>
      <p className="interaction-hint">{t("批准仅适用于本次任务和当前计划版本，随后可修改文件或执行命令。")}</p>
      <label>{t("调整意见")}<textarea rows={2} maxLength={6000} value={feedback} disabled={pending} onChange={event => { setFeedback(event.target.value); save(answers, event.target.value); }} /></label>
      <div className="interaction-actions">
        <button type="button" className="primary" disabled={pending} onClick={() => void respond({ request_digest: item.request_digest, decision: "approve", ...(feedback ? { feedback } : {}) })}>{t("批准并执行")}</button>
        <button type="button" disabled={pending || !feedback.trim()} onClick={() => void respond({ request_digest: item.request_digest, decision: "request_changes", feedback })}>{t("调整计划")}</button>
        <button type="button" disabled={pending} onClick={() => void respond({ request_digest: item.request_digest, decision: "reject", ...(feedback ? { feedback } : {}) })}>{t("暂不执行")}</button>
      </div>
    </> : <StructuredQuestions id={item.id} questions={item.request.questions||[]} answers={answers} pending={pending} onChange={changeAnswer} onSubmit={()=>void respond({request_digest:item.request_digest,answers})}/>}
    {error ? <p role="alert">{error} <button type="button" disabled={pending} onClick={async () => { try { const current = await api<Interaction>(`/api/v1/conversations/${conversationId}/interactions/${item.id}`); onConfirmed(current); setError(""); } catch { setError(t("待答请求加载失败，请重试。")); } }}>{t("检查状态")}</button></p> : null}
  </article>;
}

function InteractionContent({ item }: { item: Interaction }) {
  const t = useT();
  return <div className="interaction-content">
 {item.kind === "automation" && item.request.schedule_preview && item.request.schedule ? <ScheduleReview preview={item.request.schedule_preview} action={item.request.schedule.action} receipt/> : null}
    {item.request.steps?.length ? <ol>{item.request.steps.map((step, index) => <li key={index}>{step}</li>)}</ol> : null}
    {item.request.questions?.map(question => { const answer = item.response?.answers?.[question.id]; const options = question.options?.filter(value => value.id === answer?.option_id || answer?.option_ids?.includes(value.id)); return <div key={question.id}><strong>{question.prompt}</strong>{options?.length ? <p>{options.map(option=>option.label).join(" · ")}</p> : null}{answer?.text ? <p>{answer.text}</p> : null}</div>; })}
    {item.response?.feedback ? <p>{item.response.feedback}</p> : null}
    {item.status === "cancelled" ? <p>{t("此请求已撤销，旧回答或批准不会启动任务。")}</p> : null}
  </div>;
}
