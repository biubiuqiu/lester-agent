"use client";
import { useRef, useState } from "react";
import { ChevronDown } from "lucide-react";
import { FloatingPopover } from "./floating-popover";
import { useT } from "./i18n";
export function ActiveTaskActions({ disabled, textOnly, onAccept }: { disabled: boolean; textOnly: boolean; onAccept: (kind: "adjust" | "next") => Promise<void> }) {
  const t = useT();
  const anchor = useRef<HTMLButtonElement>(null);
  const [open, setOpen] = useState(false);
  return <><button ref={anchor} type="button" className="execution-mode" disabled={disabled} aria-expanded={open} aria-label={t("运行中输入")} onClick={() => setOpen(value => !value)}>{t("运行中输入")}<ChevronDown size={13} /></button>{open ? <FloatingPopover anchor={anchor} align="end" width={300} className="active-task-menu" role="group" aria-label={t("运行中输入")} onClose={() => { setOpen(false); anchor.current?.focus(); }}>
    <button type="button" disabled={disabled || !textOnly} onClick={() => { setOpen(false); void onAccept("adjust"); }}>{t("调整当前任务")}</button>
    <small>{t("在当前工具步骤结束后加入指令；只接受文字。")}</small>
    {!textOnly ? <small>{t("附件和 @ 引用请使用下一任务队列。")}</small> : null}
    <button type="button" disabled={disabled} onClick={() => { setOpen(false); void onAccept("next"); }}>{t("排队下一任务")}</button>
    <small>{t("当前任务完成后开始；失败或停止时暂停队列。")}</small>
  </FloatingPopover> : null}</>;
}
