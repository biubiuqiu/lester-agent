import { readFileSync } from "node:fs";

const source = new URL("../src/design/tokens.json", import.meta.url);
const namePattern = /^[a-z][a-z0-9-]*$/;
const hexPattern = /^#[\da-f]{6}(?:[\da-f]{2})?$/i;
const record = value => value !== null && typeof value === "object" && !Array.isArray(value);
const fail = message => { throw new Error(`Invalid design tokens: ${message}`); };
const cssValue = value => (typeof value === "string" && value.trim() && !/[;{}<>\u0000-\u001f]|url\s*\(|\/\*|\*\//i.test(value)) || (typeof value === "number" && Number.isFinite(value));

export function validateTokens(data) {
  if (!record(data) || data.version !== 1) fail("expected version 1");
  if (!record(data.tokens) || !Object.keys(data.tokens).length) fail("tokens must be a nonempty object");
  if (!record(data.themes) || !Object.keys(data.themes).length) fail("themes must be a nonempty object");
  if (!Object.hasOwn(data.themes, data.defaultTheme)) fail("defaultTheme does not name an existing theme");
  for (const [name, token] of Object.entries(data.tokens)) {
    if (!namePattern.test(name) || !record(token) || typeof token.type !== "string" || !token.type.trim() || typeof token.description !== "string" || !token.description.trim() || !cssValue(token.value)) fail(`invalid token ${name}`);
  }
  const base = data.themes[data.defaultTheme];
  if (!record(base) || !record(base.values) || !Object.keys(base.values).length) fail("default theme must define nonempty values");
  const themeNames = Object.keys(base.values).sort();
  for (const [name, theme] of Object.entries(data.themes)) {
    if (!namePattern.test(name) || !record(theme) || !record(theme.values) || ![theme.name, theme.label, theme.description].every(value => typeof value === "string" && value.trim())) fail(`invalid theme ${name}`);
    if (JSON.stringify(Object.keys(theme.values).sort()) !== JSON.stringify(themeNames)) fail(`theme ${name} has a different role set`);
    for (const [key, value] of Object.entries(theme.values)) if (!namePattern.test(key) || Object.hasOwn(data.tokens, key) || typeof value !== "string" || !hexPattern.test(value)) fail(`invalid theme value ${name}.${key}`);
  }
  if (!record(data.aliases) || !record(data.roles)) fail("aliases and roles must be objects");
  const available = new Set([...Object.keys(data.tokens), ...themeNames]);
  for (const [name, target] of Object.entries(data.aliases)) if (!namePattern.test(name) || name.startsWith("lester-") || typeof target !== "string" || !available.has(target)) fail(`invalid alias ${name}`);
  const references = new Map();
  for (const [name, token] of Object.entries(data.tokens)) {
    const variables = typeof token.value === "string" ? [...token.value.matchAll(/var\(\s*(--[a-z0-9_-]+)/gi)].map(match => match[1]) : [];
    for (const variable of variables) if (!variable.startsWith("--lester-") || !available.has(variable.slice(9))) fail(`unknown reference ${name} -> ${variable.startsWith("--lester-") ? variable.slice(9) : variable}`);
    const targets = variables.map(variable => variable.slice(9));
    for (const target of targets) if (!available.has(target)) fail(`unknown reference ${name} -> ${target}`);
    references.set(name, targets.filter(target => Object.hasOwn(data.tokens, target)));
  }
  const visiting = new Set();
  const visited = new Set();
  const visit = name => {
    if (visiting.has(name)) fail(`cyclic token reference ${name}`);
    if (visited.has(name)) return;
    visiting.add(name);
    for (const target of references.get(name)) visit(target);
    visiting.delete(name);
    visited.add(name);
  };
  for (const name of references.keys()) visit(name);
  for (const key of themeNames) if (typeof data.roles[key] !== "string" || !data.roles[key].trim()) fail(`missing role description for ${key}`);
  return data;
}

export function readTokens(filePath = source) {
  return validateTokens(JSON.parse(readFileSync(filePath, "utf8")));
}

export function tokenCSS(data) {
  validateTokens(data);
  const entries = values => Object.entries(values).map(([key, value]) => `  --lester-${key}: ${value};`);
  const aliases = Object.entries(data.aliases).map(([key, target]) => `  --${key}: var(--lester-${target});`);
  const global = Object.fromEntries(Object.entries(data.tokens).map(([key, token]) => [key, token.value]));
  // References resolve where a custom property is declared; redeclare derived
  // globals in nested palettes so focus rings follow that palette as well.
  const dependent = Object.fromEntries(Object.entries(global).filter(([, value]) => typeof value === "string" && /var\(/i.test(value)));
  const blocks = [`/* Generated from src/design/tokens.json. Run pnpm design:build to update. */\n:root {\n${[...entries(global), ...entries(data.themes[data.defaultTheme].values), ...aliases].join("\n")}\n}`];
  for (const [name, theme] of Object.entries(data.themes)) blocks.push(`[data-design-theme="${name}"] {\n${[...entries(theme.values), ...entries(dependent), ...aliases].join("\n")}\n}`);
  return `${blocks.join("\n\n")}\n`;
}
