"use client";

import { T, useT } from "@/components/i18n";
export function FileError({ detail, onRetry }: { detail: string; onRetry: () => void }) {
  const t = useT();
  return <div className="file-error" role="alert">
    <strong><T>{"暂时无法读取文件"}</T></strong>
    <p><T>{"请重试。此操作只重新读取文件，不会重新执行任务。"}</T></p>
    <button type="button" onClick={onRetry}><T>{"重新读取"}</T></button>
    <details><summary><T>{"查看详情"}</T></summary><pre>{t(detail)}</pre></details>
  </div>;
}
