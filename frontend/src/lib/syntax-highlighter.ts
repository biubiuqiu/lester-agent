import {
  createBundledHighlighter,
  createSingletonShorthands,
  type ThemedToken,
} from "@shikijs/core";
import { createJavaScriptRegexEngine } from "@shikijs/engine-javascript";

const languages = {
  bash: () => import("@shikijs/langs/bash"),
  c: () => import("@shikijs/langs/c"),
  cpp: () => import("@shikijs/langs/cpp"),
  css: () => import("@shikijs/langs/css"),
  dotenv: () => import("@shikijs/langs/dotenv"),
  go: () => import("@shikijs/langs/go"),
  html: () => import("@shikijs/langs/html"),
  ini: () => import("@shikijs/langs/ini"),
  java: () => import("@shikijs/langs/java"),
  javascript: () => import("@shikijs/langs/javascript"),
  json: () => import("@shikijs/langs/json"),
  jsx: () => import("@shikijs/langs/jsx"),
  markdown: () => import("@shikijs/langs/markdown"),
  php: () => import("@shikijs/langs/php"),
  properties: () => import("@shikijs/langs/properties"),
  python: () => import("@shikijs/langs/python"),
  ruby: () => import("@shikijs/langs/ruby"),
  rust: () => import("@shikijs/langs/rust"),
  scss: () => import("@shikijs/langs/scss"),
  sql: () => import("@shikijs/langs/sql"),
  toml: () => import("@shikijs/langs/toml"),
  tsx: () => import("@shikijs/langs/tsx"),
  typescript: () => import("@shikijs/langs/typescript"),
  xml: () => import("@shikijs/langs/xml"),
  yaml: () => import("@shikijs/langs/yaml"),
};

const themes = {
  "github-dark-default": () => import("@shikijs/themes/github-dark-default"),
};

const createHighlighter = createBundledHighlighter({
  langs: languages,
  themes,
  engine: () => createJavaScriptRegexEngine({ forgiving: true }),
});
const { codeToTokens } = createSingletonShorthands(createHighlighter);

export type SyntaxLanguage = keyof typeof languages;

export async function highlightSource(code: string, language: SyntaxLanguage): Promise<ThemedToken[][]> {
  const result = await codeToTokens(code, {
    lang: language,
    theme: "github-dark-default",
  });
  return result.tokens;
}
