"use client";
import { useRef, useState, type FormEvent, type KeyboardEvent, type MouseEvent } from "react";
import { Check, ChevronDown, ChevronLeft, ChevronRight, ChevronUp } from "lucide-react";
import { validQuestionAnswer, type QuestionAnswer, type UserQuestion } from "@/lib/user-question";
import { useT } from "./i18n";

export function StructuredQuestions({id,questions,answers,pending,onChange,onSubmit}:{id:string;questions:UserQuestion[];answers:Record<string,QuestionAnswer>;pending:boolean;onChange:(id:string,answer:QuestionAnswer)=>void;onSubmit:()=>void}){
 const t=useT(),[step,setStep]=useState(()=>{if(questions.length<=1)return 0;const unanswered=questions.findIndex(item=>!validQuestionAnswer(item,answers[item.id]));return unanswered<0?questions.length:unanswered;}),heading=useRef<HTMLHeadingElement>(null),form=useRef<HTMLFormElement>(null);
 const question=questions[step],review=step===questions.length,multiple=questions.length>1;
 const answered=questions.filter(item=>validQuestionAnswer(item,answers[item.id])).length;
 // Keep disclosure intent local to each question; answer text stays in the
 // existing parent-owned draft even when its input is collapsed.
 const [supplements,setSupplements]=useState<Record<string,boolean>>({});
 const supplement=useRef<HTMLTextAreaElement>(null),body=useRef<HTMLDivElement>(null),hasOptions=Boolean(question?.options?.length),currentAnswer=question?answers[question.id]:undefined;
 const supplementOpen=!hasOptions||Boolean(question&&(supplements[question.id]??Boolean(currentAnswer?.text||currentAnswer?.option_id==="")));
 const supplementID=question?`${id}-supplement-${question.id}`:undefined;
 const toggleSupplement=(event:MouseEvent<HTMLButtonElement>)=>{if(!question||pending)return;const trigger=event.currentTarget,previousFocus=document.activeElement;setSupplements(previous=>({...previous,[question.id]:!supplementOpen}));if(!supplementOpen)requestAnimationFrame(()=>{const field=supplement.current,container=body.current;if(field?.dataset.questionId===question.id&&container&&(document.activeElement===trigger||document.activeElement===previousFocus)){field.focus({preventScroll:true});const bounds=field.getBoundingClientRect(),visible=container.getBoundingClientRect();if(bounds.bottom>visible.bottom)container.scrollTop+=bounds.bottom-visible.bottom;else if(bounds.top<visible.top)container.scrollTop+=bounds.top-visible.top;}});};
 const navigate=(next:number)=>{setStep(next);requestAnimationFrame(()=>{if(body.current)body.current.scrollTop=0;heading.current?.focus({preventScroll:true});});};
 const tabKeys=(event:KeyboardEvent,index:number)=>{if(!["ArrowRight","ArrowLeft","Home","End"].includes(event.key))return;event.preventDefault();const total=questions.length+1;const next=event.key==="Home"?0:event.key==="End"?total-1:(index+(event.key==="ArrowRight"?1:total-1))%total;setStep(next);if(body.current)body.current.scrollTop=0;document.getElementById(`${id}-tab-${next===questions.length?"review":next}`)?.focus();};
 const submit=(event:FormEvent)=>{event.preventDefault();if(pending)return;if(multiple&&!review){if(validQuestionAnswer(question,answers[question.id]))navigate(step+1);}else if(answered===questions.length)onSubmit();};
 const choose=(index:number)=>{
  const option=question?.options?.[index];if(!option)return;
  const answer=answers[question.id]||{};
  if(question.multi_select){const selected=new Set(answer.option_ids||[]);if(selected.has(option.id))selected.delete(option.id);else selected.add(option.id);onChange(question.id,{...answer,option_id:undefined,option_ids:question.options!.filter(value=>selected.has(value.id)).map(value=>value.id)});}
  else onChange(question.id,{...answer,option_ids:undefined,option_id:option.id});
 };
 const shortcuts=(event:KeyboardEvent)=>{
  if(pending||event.nativeEvent.isComposing||event.ctrlKey||event.metaKey||event.altKey||event.target instanceof HTMLTextAreaElement)return;
  if(/^[1-6]$/.test(event.key)&&question?.options?.[Number(event.key)-1]){event.preventDefault();choose(Number(event.key)-1);form.current?.querySelector<HTMLInputElement>(`[data-choice="${Number(event.key)-1}"]`)?.focus();}
 };
 return <form ref={form} className="structured-questions" onSubmit={submit} onKeyDown={shortcuts}>
  <header className="question-header"><span>{t("需要你的选择")}</span>{multiple?<small aria-live="polite">{t("已回答 {0}/{1}",[answered,questions.length])}</small>:null}</header>
  {multiple?<div className="question-tabs" role="tablist" aria-label={t("问题导航")}>{questions.map((item,index)=><button key={item.id} type="button" role="tab" id={`${id}-tab-${index}`} aria-selected={step===index} aria-controls={`${id}-panel`} tabIndex={step===index?0:-1} disabled={pending} onClick={()=>navigate(index)} onKeyDown={event=>tabKeys(event,index)}>{validQuestionAnswer(item,answers[item.id])?<Check size={12} aria-hidden="true"/>:null}{item.header||t("问题 {0}",[index+1])}</button>)}<button type="button" role="tab" id={`${id}-tab-review`} aria-controls={`${id}-panel`} aria-selected={review} tabIndex={review?0:-1} disabled={pending} onKeyDown={event=>tabKeys(event,questions.length)} onClick={()=>navigate(questions.length)}>{t("核对回答")}</button></div>:null}
  <div className="question-body" ref={body} id={`${id}-panel`} role={multiple?"tabpanel":undefined} aria-labelledby={multiple?`${id}-tab-${review?"review":step}`:undefined}>
   {review?<><h3 ref={heading} tabIndex={-1}>{t("确认后发送这些回答")}</h3><div className="question-review">{questions.map((item,index)=>{const answer=answers[item.id];const labels=item.options?.filter(option=>option.id===answer?.option_id||answer?.option_ids?.includes(option.id)).map(option=>option.label);return <article key={item.id}><header><strong>{item.prompt}</strong><button type="button" disabled={pending} onClick={()=>navigate(index)}>{t("修改回答")}</button></header>{labels?.length?<p>{labels.join(" · ")}</p>:null}{answer?.text?<p>{answer.text}</p>:null}{!validQuestionAnswer(item,answer)?<small>{t("尚未回答")}</small>:null}</article>;})}</div></>:question?<fieldset disabled={pending} aria-labelledby={`${id}-question-heading`}><h3 id={`${id}-question-heading`} ref={heading} tabIndex={-1}>{question.prompt}</h3>{question.multi_select&&hasOptions?<p className="question-choice-hint">{t("可多选，也可以补充自己的想法。")}</p>:null}
    {question.options?.map((option,index)=><label className="interaction-option" key={option.id}><input type={question.multi_select?"checkbox":"radio"} name={`${id}-${question.id}`} value={option.id} data-choice={index} checked={question.multi_select?Boolean(answers[question.id]?.option_ids?.includes(option.id)):answers[question.id]?.option_id===option.id} onChange={()=>choose(index)}/><span><strong>{option.label}{option.recommended?<em>{t("推荐")}</em>:null}</strong>{option.description?<small>{option.description}</small>:null}</span><kbd aria-hidden="true">{index+1}</kbd></label>)}
    {!question.multi_select&&hasOptions?<label className="interaction-option"><input type="radio" name={`${id}-${question.id}`} value="" checked={answers[question.id]?.option_id===""} onChange={()=>{setSupplements(previous=>({...previous,[question.id]:true}));onChange(question.id,{...answers[question.id],option_id:"",option_ids:undefined});}}/><span>{t("自由回答")}</span></label>:null}
    {hasOptions?<button type="button" className="question-supplement-toggle" aria-expanded={supplementOpen} aria-controls={supplementID} onClick={toggleSupplement}>{supplementOpen?<ChevronUp size={13} aria-hidden="true"/>:<ChevronDown size={13} aria-hidden="true"/>}{t(supplementOpen?"收起补充":"添加补充")}</button>:null}
    <div className="question-supplement" id={supplementID} hidden={!supplementOpen}><label>{t("补充回答")}<textarea ref={supplement} data-question-id={question.id} rows={2} maxLength={6000} value={answers[question.id]?.text||""} onChange={event=>onChange(question.id,{...answers[question.id],text:event.target.value})}/></label></div>
   </fieldset>:null}
  </div>
  <footer className="question-footer"><p>{t("回答用于澄清需求，不会批准修改或执行操作。")}</p><div>{multiple&&step>0?<button type="button" disabled={pending} onClick={()=>navigate(step-1)}><ChevronLeft size={14}/>{t("上一题")}</button>:null}{multiple&&!review?<button type="submit" className="primary" disabled={pending||!validQuestionAnswer(question,answers[question.id])}>{t(step===questions.length-1?"核对回答":"下一题")}<ChevronRight size={14}/></button>:<button type="submit" className="primary" disabled={pending||answered!==questions.length}>{t(pending?"正在提交…":"提交回答")}</button>}</div></footer>
 </form>;
}
