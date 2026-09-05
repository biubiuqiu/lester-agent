"use client";

import { startTransition, useEffect, useMemo, useState, type CSSProperties } from "react";
import type { ThemedToken } from "@shikijs/core";
import { highlightSource, type SyntaxLanguage } from "@/lib/syntax-highlighter";
import { usePreviewScroll } from "./use-preview-scroll";

const maxRenderedLines = 3000;
const languageByExtension: Record<string, SyntaxLanguage> = {
  c: "c",
  cc: "cpp",
  conf: "ini",
  cpp: "cpp",
  css: "css",
  env: "dotenv",
  go: "go",
  h: "c",
  hpp: "cpp",
  htm: "html",
  html: "html",
  ini: "ini",
  java: "java",
  js: "javascript",
  jsx: "jsx",
  json: "json",
  md: "markdown",
  mjs: "javascript",
  php: "php",
  properties: "properties",
  py: "python",
  rb: "ruby",
  rs: "rust",
  scss: "scss",
  sh: "bash",
  sql: "sql",
  toml: "toml",
  ts: "typescript",
  tsx: "tsx",
  xml: "xml",
  yaml: "yaml",
  yml: "yaml",
};

export function SourcePreview({ content, fileName, storageKey = "", filePath = fileName }: { content: string; fileName: string; storageKey?: string; filePath?: string }) {
  const scroll = usePreviewScroll(storageKey, `source:${filePath}`);
  const lines = useMemo(() => content.split("\n"), [content]);
  const source = useMemo(() => lines.slice(0, maxRenderedLines).join("\n"), [lines]);
  const fallbackLines = useMemo<ThemedToken[][]>(() => source.split("\n").map((line) => [{ content: line, offset: 0 }]), [source]);
  const language = languageByExtension[fileExtension(fileName)];
  const [highlighted, setHighlighted] = useState<{ language: SyntaxLanguage; source: string; lines: ThemedToken[][] } | null>(null);

  useEffect(() => {
    let active = true;
    if (!language) return () => { active = false; };

    const timer = window.setTimeout(() => {
      void highlightSource(source, language)
        .then((tokens) => {
          if (active) startTransition(() => setHighlighted({ language, source, lines: tokens }));
        })
        .catch(() => {
          // Plain text remains visible when a grammar cannot be loaded or parsed.
        });
    }, 0);

    return () => {
      active = false;
      window.clearTimeout(timer);
    };
  }, [language, source]);

  const renderedLines = highlighted?.language === language && highlighted.source === source ? highlighted.lines : fallbackLines;
  return <div ref={scroll} className="file-source-scroll" aria-label={`${fileName} 源代码`}>
    <div className="file-source-code">
      {renderedLines.map((tokens, lineIndex) => <div className="file-source-line" key={lineIndex}>
        <span>{lineIndex + 1}</span>
        <code>{tokens.length === 0
          ? " "
          : tokens.map((token, tokenIndex) => <span key={`${lineIndex}-${tokenIndex}`} style={tokenStyle(token)}>{token.content || " "}</span>)}</code>
      </div>)}
    </div>
    {lines.length > maxRenderedLines ? <p className="file-source-truncated">仅显示前 {maxRenderedLines} 行，共 {lines.length} 行。</p> : null}
  </div>;
}

function tokenStyle(token: ThemedToken): CSSProperties {
  const decorations: string[] = [];
  if (token.fontStyle && (token.fontStyle & 4) !== 0) decorations.push("underline");
  if (token.fontStyle && (token.fontStyle & 8) !== 0) decorations.push("line-through");
  return {
    color: token.color,
    backgroundColor: token.bgColor,
    fontStyle: token.fontStyle && (token.fontStyle & 1) !== 0 ? "italic" : undefined,
    fontWeight: token.fontStyle && (token.fontStyle & 2) !== 0 ? 700 : undefined,
    textDecoration: decorations.length > 0 ? decorations.join(" ") : undefined,
  };
}

function fileExtension(path: string) {
  const name = path.split("/").at(-1) || "";
  const dot = name.lastIndexOf(".");
  return dot > 0 ? name.slice(dot + 1).toLowerCase() : "";
}
