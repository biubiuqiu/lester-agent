"use client";
import { useT } from "@/components/i18n";


import { Children, isValidElement, useRef, type ReactNode } from "react";
import ReactMarkdown, { type Components } from "react-markdown";
import remarkGfm from "remark-gfm";
import { useOptionalFileWorkspace } from "./file-workspace";
import { relativeFilePath } from "@/lib/file-inventory";
import { CopyButton } from "./copy-button";
import { SelectionQuote } from "./selection-quote";

const remarkPlugins = [remarkGfm];

const components: Components = {
  pre: ({ children }) => <MarkdownCodeBlock>{children}</MarkdownCodeBlock>,
  a: ({ children, ...props }) => (
    <a {...props} target="_blank" rel="noreferrer">
      {children}
    </a>
  ),
};

export function MessageContent({ content, linkFiles = false, actions = false, quoteSource }: { content: string; linkFiles?: boolean; actions?: boolean; quoteSource?: string }) {
  const t = useT();
  const rendered = useRef<HTMLDivElement>(null);
  const workspace = useOptionalFileWorkspace();
  const messageComponents: Components = linkFiles && workspace ? {
    ...components,
    code: ({ children, className }) => {
      const path = typeof children === "string" && !children.includes("\n") ? relativeFilePath(workspace.conversationId, children) : "";
      const file = !className && path ? workspace.files.find(item => item.path === path) : undefined;
      return file ? <button type="button" className="message-file-link" aria-label={t("查看文件 {0}", [file.name])} onClick={() => workspace.open(file)}><code>{children}</code></button> : <code className={className}>{children}</code>;
    },
  } : components;
  function formattedHTML() {
    const copy = rendered.current?.cloneNode(true) as HTMLElement | undefined;
    if (!copy) throw new Error("Content unavailable");
    copy.querySelectorAll("[data-copy-exclude]").forEach(node => node.remove());
    copy.querySelectorAll("button").forEach(node => node.replaceWith(...node.childNodes));
    return copy.innerHTML;
  }
  return <SelectionQuote enabled={Boolean(quoteSource)} source={quoteSource || t("引用回答")}>
    <div className="markdown-content" ref={rendered}>
      <ReactMarkdown remarkPlugins={remarkPlugins} components={messageComponents}>
        {content}
      </ReactMarkdown>
    </div>
    {actions ? <div className="message-actions"><CopyButton text={content} label={t("复制回答")} compact /><CopyButton text={content} html={formattedHTML} label={t("保留格式复制")} /></div> : null}
  </SelectionQuote>;
}

function textContent(node: ReactNode): string {
  return Children.toArray(node).map(item => typeof item === "string" || typeof item === "number" ? String(item) : isValidElement<{ children?: ReactNode }>(item) ? textContent(item.props.children) : "").join("");
}
function MarkdownCodeBlock({ children }: { children: ReactNode }) {
  const t = useT();
  const code = Children.toArray(children).find(item => isValidElement(item));
  const language = isValidElement<{ className?: string }>(code) ? code.props.className?.replace(/^language-/, "") : "";
  return <div className="message-code-block"><header data-copy-exclude="true"><span>{language || t("代码")}</span><CopyButton text={textContent(children)} label={t("复制代码")} /></header><pre tabIndex={0}>{children}</pre></div>;
}
