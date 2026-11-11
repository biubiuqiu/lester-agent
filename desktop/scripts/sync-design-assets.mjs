import { readFile, writeFile, mkdir, copyFile } from 'node:fs/promises';
import { fileURLToPath } from 'node:url';
import { dirname, resolve } from 'node:path';

const desktop = resolve(dirname(fileURLToPath(import.meta.url)), '..');
const frontend = resolve(desktop, '../frontend');
const target = resolve(desktop, 'renderer/assets');
await mkdir(target, { recursive: true });
const tokens = JSON.parse(await readFile(resolve(frontend, 'src/design/tokens.json'), 'utf8'));
const values = { ...Object.fromEntries(Object.entries(tokens.tokens).map(([key, token]) => [key, token.value])), ...tokens.themes.graphite.values };
const css = `/* Generated from frontend/src/design/tokens.json. */\n:root {\n${Object.entries(values).map(([key, value]) => `  --lester-${key}: ${value};`).join('\n')}\n}\n`;
await writeFile(resolve(target, 'tokens.css'), css);
await copyFile(resolve(frontend, 'src/app/icon.svg'), resolve(target, 'lester.svg'));
// The production design gallery is built from ProductGlyph, not a second drawing set.
// Standalone SVGs cannot inherit application variables, so resolve their paint tokens.
const gallery = await readFile(resolve(frontend, 'public/design-system.html'), 'utf8');
const resolvePaint = svg => svg.replace(/var\(--lester-([\w-]+)(?:,\s*([^)]*))?\)/g, (_, key, fallback) => {
  if (values[key] === undefined && fallback === undefined) throw new Error(`Unknown design paint: ${key}`);
  return values[key] ?? fallback;
});
let count = 0;
for (const match of gallery.matchAll(/<svg\b[^>]*data-product-glyph="([\w-]+)"[^>]*>[\s\S]*?<\/svg>/g)) {
  if (!match[0].includes('data-size="24"')) continue;
  const svg = resolvePaint(match[0]).replace(/^<svg\b/, '<svg xmlns="http://www.w3.org/2000/svg"');
  await writeFile(resolve(target, `${match[1]}.svg`), svg);
  count++;
}
if (count !== 24) throw new Error(`Expected 24 production glyphs; found ${count}. Rebuild the frontend design gallery.`);
for (const [source, name] of [
  ['@xterm/xterm/lib/xterm.js', 'xterm.js'], ['@xterm/xterm/css/xterm.css', 'xterm.css'],
  ['@xterm/addon-fit/lib/addon-fit.js', 'addon-fit.js'],
]) await copyFile(resolve(desktop, 'node_modules', source), resolve(target, name));
const notices = [];
for (const packageName of ['@xterm/xterm', '@xterm/addon-fit']) {
  const directory = resolve(desktop, 'node_modules', packageName);
  const metadata = JSON.parse(await readFile(resolve(directory, 'package.json'), 'utf8'));
  notices.push(`${packageName} ${metadata.version}\n\n${await readFile(resolve(directory, 'LICENSE'), 'utf8')}`);
}
await writeFile(resolve(target, 'THIRD_PARTY_NOTICES.txt'), notices.join('\n\n---\n\n'));
process.stdout.write(`Synced Graphite tokens, canonical Lester mark, ${count} production glyphs and terminal assets.\n`);
