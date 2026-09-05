export function FileError({ detail, onRetry }: { detail: string; onRetry: () => void }) {
  return <div className="file-error" role="alert">
    <strong>暂时无法读取文件</strong>
    <p>请重试。此操作只重新读取文件，不会重新执行任务。</p>
    <button type="button" onClick={onRetry}>重新读取</button>
    <details><summary>查看详情</summary><pre>{detail}</pre></details>
  </div>;
}
