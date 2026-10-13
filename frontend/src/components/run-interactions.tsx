"use client";
import { useEffect, useRef, useState, type FormEvent } from "react";
import { useT } from "./i18n";
import { api, ApiError } from "@/lib/api";
import { readView, updateView } from "@/lib/conversation-view-state";
import { sameInteractionResponse } from "@/lib/interaction-response";

type Answer = { option_id?: string; text?: string };
type Response = { request_digest: string; answers?: Record<string, Answer>; decision?: string; feedback?: string };
type Interaction = {
  id: string; run_id: string; kind: "question" | "plan"; request_digest: string;
  status: "pending" | "answered" | "approved" | "rejected" | "changes_requested" | "cancelled";
  request: { title?: string; steps?: string[]; questions?: { id: string; prompt: string; options?: { id: string; label: string; description?: string }[] }[] };
  response?: Response;
};
type Interactions = { interactions: Interaction[]; execution_mode: "plan" | "execute" };

export function RunInteractions({ conversationId, storageKey, active, revision, runId }: { conversationId: string; storageKey: string; active: boolean; revision: number; runId?: string }) {
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
        if (alive) { setData(previous => ({ ...next, execution_mode: previous.interactions.some(item => item.run_id === currentRun.current && item.status === "approved") ? "execute" : next.execution_mode, interactions: next.interactions.map(item => {
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
    setData(previous => ({ ...previous, execution_mode: item.status === "approved" && item.run_id === currentRun.current ? "execute" : previous.execution_mode, interactions: previous.interactions.map(old => old.id === item.id ? item : old) }));
  };
  if (!error && !data.interactions.length && (!active || data.execution_mode !== "plan")) return null;
  return <section className="run-interactions" aria-label={t("任务协作")}>
    {active && data.execution_mode === "plan" ? <p className="interaction-mode">{t("Plan · 仅查看与规划")}</p> : null}
    {error ? <p role="alert">{error} <button type="button" onClick={() => refresh.current()}>{t("重试")}</button></p> : null}
    {data.interactions.toReversed().map(item => <InteractionCard key={item.id} item={item} conversationId={conversationId} storageKey={storageKey} onConfirmed={confirmed} />)}
  </section>;
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
    } catch { setError(t("提交结果未确认，回答已保留。请检查状态后重试。")); }
    finally { submitted.current = false; setPending(false); }
  }
  const status = ({ pending: item.kind === "plan" ? "计划等待批准" : "等待你的回答", answered: "已回答", approved: "计划已批准", rejected: "计划已拒绝", changes_requested: "请调整计划", cancelled: "已取消" })[item.status];
  if (item.status !== "pending") return <details className={`interaction-receipt ${item.status}`}><summary>{t(status)}{item.request.title ? ` · ${item.request.title}` : ""}</summary><InteractionContent item={item} /></details>;
  const submitAnswers = (event: FormEvent) => { event.preventDefault(); void respond({ request_digest: item.request_digest, answers }); };
  const validAnswers = (item.request.questions || []).every(question => Boolean(answers[question.id]?.option_id || answers[question.id]?.text?.trim()));
  return <article className="interaction-card" aria-busy={pending}>
    <h3>{item.kind === "plan" ? item.request.title : t("需要你的选择")}</h3>
    {item.kind === "plan" ? <>
      <ol>{item.request.steps?.map((step, index) => <li key={index}>{step}</li>)}</ol>
      <p className="interaction-hint">{t("批准仅适用于本次任务和当前计划版本，随后可修改文件或执行命令。")}</p>
      <label>{t("调整意见")}<textarea rows={2} maxLength={6000} value={feedback} disabled={pending} onChange={event => { setFeedback(event.target.value); save(answers, event.target.value); }} /></label>
      <div className="interaction-actions">
        <button type="button" className="primary" disabled={pending} onClick={() => void respond({ request_digest: item.request_digest, decision: "approve", ...(feedback ? { feedback } : {}) })}>{t("批准并执行")}</button>
        <button type="button" disabled={pending || !feedback.trim()} onClick={() => void respond({ request_digest: item.request_digest, decision: "request_changes", feedback })}>{t("调整计划")}</button>
        <button type="button" disabled={pending} onClick={() => void respond({ request_digest: item.request_digest, decision: "reject", ...(feedback ? { feedback } : {}) })}>{t("暂不执行")}</button>
      </div>
    </> : <form onSubmit={submitAnswers}>
      {item.request.questions?.map(question => <fieldset key={question.id} disabled={pending}>
        <legend>{question.prompt}</legend>
        {question.options?.map(option => <label className="interaction-option" key={option.id}><input type="radio" name={`${item.id}-${question.id}`} value={option.id} checked={answers[question.id]?.option_id === option.id} onChange={() => changeAnswer(question.id, { ...answers[question.id], option_id: option.id })} /><span><strong>{option.label}</strong>{option.description ? <small>{option.description}</small> : null}</span></label>)}
        {question.options?.length ? <label className="interaction-option"><input type="radio" name={`${item.id}-${question.id}`} value="" checked={answers[question.id]?.option_id === ""} onChange={() => changeAnswer(question.id, { ...answers[question.id], option_id: "" })} /><span>{t("自由回答")}</span></label> : null}
        <label>{t("补充回答")}<textarea rows={2} maxLength={6000} value={answers[question.id]?.text || ""} onChange={event => changeAnswer(question.id, { ...answers[question.id], text: event.target.value })} /></label>
      </fieldset>)}
      <p className="interaction-hint">{t("回答用于澄清需求，不会批准修改或执行操作。")}</p>
      <button className="primary" disabled={pending || !validAnswers}>{t("提交回答")}</button>
    </form>}
    {error ? <p role="alert">{error} <button type="button" disabled={pending} onClick={async () => { try { const current = await api<Interaction>(`/api/v1/conversations/${conversationId}/interactions/${item.id}`); onConfirmed(current); setError(""); } catch { setError(t("待答请求加载失败，请重试。")); } }}>{t("检查状态")}</button></p> : null}
  </article>;
}

function InteractionContent({ item }: { item: Interaction }) {
  const t = useT();
  return <div className="interaction-content">
    {item.request.steps?.length ? <ol>{item.request.steps.map((step, index) => <li key={index}>{step}</li>)}</ol> : null}
    {item.request.questions?.map(question => { const answer = item.response?.answers?.[question.id]; const option = question.options?.find(value => value.id === answer?.option_id); return <div key={question.id}><strong>{question.prompt}</strong>{option ? <p>{option.label}</p> : null}{answer?.text ? <p>{answer.text}</p> : null}</div>; })}
    {item.response?.feedback ? <p>{item.response.feedback}</p> : null}
    {item.status === "cancelled" ? <p>{t("此请求已撤销，旧回答或批准不会启动任务。")}</p> : null}
  </div>;
}
