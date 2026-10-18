"use client";
import { CornerDownLeft } from "lucide-react";
import { useT } from "./i18n";
export function ActiveTaskActions({ disabled, textOnly, onAccept }: { disabled: boolean; textOnly: boolean; onAccept: () => Promise<void> }) {
  const t = useT();
  return <button
    type="button"
    className="current-task-supplement"
    disabled={disabled || !textOnly}
    aria-label={t("补充当前任务")}
    title={t(textOnly ? "在当前步骤结束后补充这段文字，不停止任务。" : "附件和 @ 引用请使用下一任务队列。")}
    onClick={() => void onAccept()}
  ><CornerDownLeft size={16} aria-hidden="true" /><span>{t("补充当前任务")}</span></button>;
}
