import ReactMarkdown, { type Components } from "react-markdown";
import remarkGfm from "remark-gfm";
import { useOptionalFileWorkspace } from "./file-workspace";
import { relativeFilePath } from "@/lib/file-inventory";

const remarkPlugins = [remarkGfm];

const components: Components = {
  a: ({ children, ...props }) => (
    <a {...props} target="_blank" rel="noreferrer">
      {children}
    </a>
  ),
};

export function MessageContent({ content, linkFiles = false }: { content: string; linkFiles?: boolean }) {
  const workspace = useOptionalFileWorkspace();
  const messageComponents: Components = linkFiles && workspace ? {
    ...components,
    code: ({ children, className }) => {
      const path = typeof children === "string" && !children.includes("\n") ? relativeFilePath(workspace.conversationId, children) : "";
      const file = !className && path ? workspace.files.find(item => item.path === path) : undefined;
      return file ? <button type="button" className="message-file-link" aria-label={`查看文件 ${file.name}`} onClick={() => workspace.open(file)}><code>{children}</code></button> : <code className={className}>{children}</code>;
    },
  } : components;
  return (
    <div className="markdown-content">
      <ReactMarkdown remarkPlugins={remarkPlugins} components={messageComponents}>
        {content}
      </ReactMarkdown>
    </div>
  );
}
