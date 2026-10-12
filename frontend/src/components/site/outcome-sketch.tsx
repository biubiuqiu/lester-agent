import { Code2, FileText, Globe } from "lucide-react";
import { T } from "@/components/i18n";

// Native radios keep this illustrative interaction usable without JavaScript.
export function OutcomeSketch() {
  return <fieldset className="site-outcome-sketch">
    <legend><T>{"成果示例"}</T></legend>
    <div className="site-outcome-options">
      <label><input type="radio" name="site-outcome" value="web" defaultChecked /><span><Globe size={15} aria-hidden="true" /><T>{"网页"}</T></span></label>
      <label><input type="radio" name="site-outcome" value="document" /><span><FileText size={15} aria-hidden="true" /><T>{"文档"}</T></span></label>
      <label><input type="radio" name="site-outcome" value="code" /><span><Code2 size={15} aria-hidden="true" /><T>{"代码"}</T></span></label>
      <i className="site-outcome-indicator" aria-hidden="true" />
    </div>
    <div className="site-outcome-sample" data-kind="web">
      <div className="site-outcome-file"><Code2 size={14} aria-hidden="true" /><code>index.html</code></div>
      <div className="site-sample-web"><span>Lester</span><strong><T>{"让灵感"}</T>{" "}<T>{"落地。"}</T></strong><div className="site-sample-lines" aria-hidden="true"><i /><i /></div></div>
    </div>
    <div className="site-outcome-sample" data-kind="document">
      <div className="site-outcome-file"><FileText size={14} aria-hidden="true" /><code>brief.md</code></div>
      <div className="site-sample-document"><strong><T>{"产品需求报告"}</T></strong><span><T>{"一、项目背景"}</T></span><span><T>{"二、核心需求"}</T></span></div>
    </div>
    <div className="site-outcome-sample" data-kind="code">
      <div className="site-outcome-file"><Code2 size={14} aria-hidden="true" /><code>main.py</code></div>
      <pre className="site-sample-code"><code>{"def summarize(rows):\n    return {\"total\": len(rows)}"}</code></pre>
    </div>
  </fieldset>;
}
