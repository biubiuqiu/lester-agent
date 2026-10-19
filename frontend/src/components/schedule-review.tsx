"use client";

import { CalendarClock } from "lucide-react";
import Link from "next/link";
import { useI18n } from "./i18n";

export type SchedulePreview = {
  id: string; revision: number; name: string; prompt: string;
  project_id: string; project_name: string; model_deployment_id: string; model_name: string;
  agent_slug: string; permission_mode: "standard" | "auto" | "full";
  enabled: boolean; cron: string; timezone: string; run_at: string | null; next_run_at: string | null;
};
export type ScheduleAction = "create" | "update" | "pause" | "resume";
export const scheduleActionLabels = { create: "启用定时任务", update: "确认修改", pause: "暂停定时任务", resume: "恢复定时任务" };

export function ScheduleReview({ preview, action, receipt = false }: { preview: SchedulePreview; action: ScheduleAction; receipt?: boolean }) {
  const { t, locale } = useI18n();
  const stamp = (value: string) => new Intl.DateTimeFormat(locale, { dateStyle: "medium", timeStyle: "short", timeZone: preview.timezone }).format(new Date(value));
  const parts = preview.cron.trim().split(/\s+/);
  const simple = parts.length === 5 && /^\d+$/.test(parts[0]) && /^\d+$/.test(parts[1]) && parts[2] === "*" && parts[3] === "*";
  const clock = simple ? `${parts[1].padStart(2, "0")}:${parts[0].padStart(2, "0")}` : "";
  const weekdays = ["周日", "周一", "周二", "周三", "周四", "周五", "周六"];
  const cadence = preview.run_at ? t("单次：{0}", [stamp(preview.run_at)]) : simple && parts[4] === "*" ? t("每天 {0}", [clock]) : simple && /^[0-6]$/.test(parts[4]) ? t("每{0} {1}", [t(weekdays[Number(parts[4])]), clock]) : t("Cron：{0}", [preview.cron]);
  return <div className="schedule-review">
    {!receipt ? <header><CalendarClock size={17} aria-hidden="true" /><span>{t(scheduleActionLabels[action])}</span></header> : null}
    {!receipt ? <h3>{preview.name}</h3> : null}
    <p className="schedule-timing">{cadence}<span>{preview.timezone}</span></p>
    <p className="schedule-project">{t("项目")} · {preview.project_name}</p>
    {!receipt && (preview.next_run_at || !preview.enabled) ? <p className="schedule-next">{preview.enabled ? t("下次：{0}", [stamp(preview.next_run_at!)]) : t("已暂停")}</p> : null}
    <details className="schedule-config"><summary>{t("任务内容与配置")}</summary><p>{preview.prompt}</p><dl>
      <dt>{t("模型")}</dt><dd>{preview.model_name}</dd>
      <dt>Agent</dt><dd>{preview.agent_slug}</dd>
      <dt>{t("权限")}</dt><dd>{t(({ standard: "默认权限", auto: "自动审批", full: "完全访问权限" })[preview.permission_mode])}</dd>
      <dt>{t("确认后状态")}</dt><dd>{t(preview.enabled ? "已启用" : "已暂停")}</dd>
      {preview.cron ? <><dt>Cron</dt><dd><code>{preview.cron}</code></dd></> : null}
    </dl></details>
    {!receipt ? <p className="schedule-note">{t(action === "pause" ? "暂停会取消尚未开始的触发，已启动的任务继续执行。" : !preview.enabled ? "任务将保持暂停，启用后才会触发。" : "到时在独立会话中执行；不会继承当前聊天的文件、附件或引用。")}</p> : <Link href="/app/settings/automations">{t("管理定时任务")}</Link>}
  </div>;
}
