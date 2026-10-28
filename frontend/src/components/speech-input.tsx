"use client";

import { useEffect, useId, useState, type ReactNode, type RefObject } from "react";
import { Check, Copy, LoaderCircle, Mic, Square, X } from "lucide-react";
import { useT } from "./i18n";
import { useSpeech } from "./speech-provider";

// Keeping the microphone/status in this small component avoids rendering the
// full conversation or personal transcript every time the elapsed second ticks.
export function SpeechComposer({ draftKey, inputRef, onText, disabled = false, active = true, maxLength = 100_000, leading, children, className = "compose-actions" }: {
  draftKey: string; inputRef: RefObject<HTMLTextAreaElement | null>; onText: (text: string) => void;
  disabled?: boolean; active?: boolean; maxLength?: number; leading?: ReactNode; children?: ReactNode; className?: string;
}) {
  const t = useT(), owner = useId();
  const speech = useSpeech();
  const register = speech?.register, cancel = speech?.cancel;
  const [copyNotice, setCopyNotice] = useState("");
  useEffect(() => {
    if (active) register?.(draftKey);
    return () => cancel?.(owner);
  }, [active, draftKey, register, cancel, owner]);
  useEffect(() => { if (disabled || !active) cancel?.(owner); }, [disabled, active, cancel, owner]);
  useEffect(() => {
    const form = inputRef.current?.form;
    // An explicit ordinary send abandons an unfinished recording, rather than
    // letting its delayed result populate a new, already-cleared draft.
    const send = () => cancel?.(owner);
    form?.addEventListener("submit", send, true);
    return () => form?.removeEventListener("submit", send, true);
  }, [inputRef, cancel, owner]);

  const view = speech?.view;
  const own = view?.owner === owner;
  const recording = own && view.phase === "recording";
  const capturing = own && (view.phase === "recording" || view.phase === "permission");
  const transcribing = own && view.phase === "transcribing";
  const occupied = Boolean(view && view.owner && view.owner !== owner && (view.phase === "recording" || view.phase === "permission" || view.phase === "transcribing"));
  const shown = active && speech?.capabilities?.enabled;

  return <>
    {shown && own && view.phase !== "idle" ? <div className={`speech-feedback speech-${view.phase}`} aria-label={t("语音输入")}>
      <div className="speech-status-row">
        {view.phase === "completed" ? <Check size={15} aria-hidden="true" /> : transcribing || view.phase === "permission" ? <LoaderCircle size={15} className="speech-spinner" aria-hidden="true" /> : recording ? <span className="speech-recording-dot" aria-hidden="true" /> : <Mic size={15} aria-hidden="true" />}
        <span role={view.phase === "error" ? "alert" : "status"}>{t(view.phase === "permission" ? "正在请求麦克风…" : recording ? "正在录音" : transcribing ? "正在转写…" : view.message)}</span>
        {recording ? <span className="speech-elapsed" aria-hidden="true">{Math.floor(view.seconds / 60)}:{String(view.seconds % 60).padStart(2, "0")}</span> : null}
        <div className="speech-status-actions">
          {recording ? <button type="button" onClick={() => speech.stop(owner)} aria-label={t("停止并转写")}><Square size={13} />{t("停止并转写")}</button> : null}
          {view.recovery === "check" ? <button type="button" onClick={() => speech.recover(owner)}>{t("检查转写状态")}</button> : view.recovery === "retry" ? <button type="button" onClick={() => speech.retry(owner)}>{t("重试转写")}</button> : null}
          <button type="button" className="speech-dismiss" aria-label={t(capturing ? "取消录音" : transcribing ? "取消转写" : "关闭语音提示")} title={t(capturing ? "取消录音" : transcribing ? "取消转写" : "关闭语音提示")} onClick={() => { speech.cancel(owner); setCopyNotice(""); }}><X size={15} /></button>
        </div>
      </div>
      {capturing ? <p className="speech-privacy">{t("音频将发送给管理员配置的语音服务，仅用于转写。")}</p> : null}
      {view.recognized ? <div className="speech-overflow"><textarea aria-label={t("识别文字")} readOnly rows={3} value={view.recognized} /><button type="button" onClick={async () => {
        try { await navigator.clipboard.writeText(view.recognized!); setCopyNotice("已复制识别文字"); }
        catch { setCopyNotice("复制失败，请手动选择文字。"); }
      }}><Copy size={13} />{t("复制识别文字")}</button>{copyNotice ? <p role="status">{t(copyNotice)}</p> : null}</div> : null}
    </div> : null}
    <div className={className}>{leading}
      {shown ? <button type="button" className={`speech-microphone${capturing ? " is-recording" : ""}`} aria-label={t("语音输入")} title={t(occupied ? "另一个输入框正在使用语音。" : "语音输入")} aria-pressed={capturing} disabled={disabled || occupied || transcribing || own && view.phase === "permission"} onClick={() => {
        setCopyNotice("");
        if (recording) speech.stop(owner);
        else speech.start({ owner, draftKey, input: inputRef, onText, limit: maxLength });
      }}><Mic size={18} /></button> : null}
      {children}
    </div>
  </>;
}
