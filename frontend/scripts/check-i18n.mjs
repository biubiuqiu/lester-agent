import { readFileSync, readdirSync } from "node:fs";
import { join } from "node:path";
import ts from "typescript";

const root = join(process.cwd(), "src");
const directory = join(root, "lib/i18n/messages");
const source = JSON.parse(readFileSync(join(directory, "zh-CN.json"), "utf8"));
const han = /[\u3400-\u9fff]/;
const missing = new Set();
function files(path) { return readdirSync(path, { withFileTypes: true }).flatMap(entry => entry.isDirectory() ? files(join(path, entry.name)) : /\.tsx?$/.test(entry.name) && !entry.name.endsWith(".test.ts") ? [join(path, entry.name)] : []); }
for (const path of files(root)) {
  // Native language names and deployment command literals stay unchanged.
  if (path.endsWith("lib/i18n.ts") || path.endsWith("lib/site.ts")) continue;
  const file = ts.createSourceFile(path, readFileSync(path, "utf8"), ts.ScriptTarget.Latest, true, path.endsWith("x") ? ts.ScriptKind.TSX : ts.ScriptKind.TS);
  function visit(node) {
    let key;
    if ((ts.isStringLiteral(node) || ts.isNoSubstitutionTemplateLiteral(node)) && han.test(node.text)) key = node.text;
    if (ts.isJsxText(node) && han.test(node.text)) throw new Error(`Unlocalized JSX text in ${path}: ${node.text.trim()}`);
    if (ts.isTemplateExpression(node) && han.test(node.getText(file))) key = node.head.text + node.templateSpans.map((span, index) => `{${index}}${span.literal.text}`).join("");
    if (key && !Object.hasOwn(source, key)) missing.add(`${path.slice(root.length + 1)}: ${key}`);
    ts.forEachChild(node, visit);
  }
  visit(file);
}
if (missing.size) throw new Error(`Missing source messages:\n${[...missing].join("\n")}`);
const parameters = value => [...value.matchAll(/\{\d+\}/g)].map(match => match[0]).sort().join(",");
for (const locale of ["en", "ja", "ko", "fr", "es"]) {
  const messages = JSON.parse(readFileSync(join(directory, `${locale}.json`), "utf8"));
  if (JSON.stringify(Object.keys(messages).sort()) !== JSON.stringify(Object.keys(source).sort())) throw new Error(`${locale}: message keys differ`);
  for (const [key, value] of Object.entries(messages)) {
    if (typeof value !== "string" || !value.trim()) throw new Error(`${locale}: empty message ${key}`);
    if (parameters(key) !== parameters(value)) throw new Error(`${locale}: interpolation mismatch ${key}`);
    if (["en", "ko", "fr", "es"].includes(locale) && han.test(value)) throw new Error(`${locale}: untranslated Chinese in ${key}`);
  }
}
console.log(`All ${Object.keys(source).length} source messages have complete translations in five languages.`);
