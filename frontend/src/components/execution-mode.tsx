"use client";
import { useT } from "./i18n";

export function ExecutionMode({ mode, disabled, onChange }: { mode: "execute" | "plan"; disabled: boolean; onChange: (mode: "execute" | "plan") => void }) {
  const t = useT();
  return <button type="button" className={`execution-mode ${mode}`} aria-pressed={mode === "plan"} aria-label={t("先规划，再执行")} title={t("Plan 模式只读；批准当前计划后才能修改文件和执行命令。")} disabled={disabled} onClick={() => onChange(mode === "plan" ? "execute" : "plan")}>{mode === "plan" ? "Plan" : t("执行")}</button>;
}
