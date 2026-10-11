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
    <div className="site-outcome-file" data-kind="web"><Code2 size={16} aria-hidden="true" /><code>index.html</code></div>
    <div className="site-outcome-file" data-kind="document"><FileText size={16} aria-hidden="true" /><code>brief.md</code></div>
    <div className="site-outcome-file" data-kind="code"><Code2 size={16} aria-hidden="true" /><code>main.py</code></div>
  </fieldset>;
}
