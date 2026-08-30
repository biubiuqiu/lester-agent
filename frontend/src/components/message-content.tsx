import ReactMarkdown, { type Components } from "react-markdown";
import remarkGfm from "remark-gfm";

const remarkPlugins = [remarkGfm];

const components: Components = {
  a: ({ children, ...props }) => (
    <a {...props} target="_blank" rel="noreferrer">
      {children}
    </a>
  ),
};

export function MessageContent({ content }: { content: string }) {
  return (
    <div className="markdown-content">
      <ReactMarkdown remarkPlugins={remarkPlugins} components={components}>
        {content}
      </ReactMarkdown>
    </div>
  );
}
