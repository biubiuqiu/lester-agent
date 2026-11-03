import assert from "node:assert/strict";
import { mkdtempSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import test from "node:test";
import { readTokens, tokenCSS, validateTokens } from "./design-token-core.mjs";

// A small independent fixture keeps failures attributable to the boundary being
// exercised rather than to unrelated changes in Lester's production palette.
function fixture() {
  return {
    version: 1,
    defaultTheme: "jade",
    tokens: {
      "space-2": { type: "dimension", value: "8px", description: "Control spacing" },
      "font-weight-medium": { type: "number", value: 500, description: "Medium text" },
    },
    themes: {
      jade: { name: "Jade", label: "青绿", description: "Default palette", values: { "color-text": "#202E27", "color-canvas": "#FFFFFF" } },
      indigo: { name: "Indigo", label: "靛蓝", description: "Alternative palette", values: { "color-canvas": "#FFFFFF", "color-text": "#252E4A" } },
    },
    aliases: { ink: "color-text", gap: "space-2" },
    roles: { "color-text": "Readable text", "color-canvas": "Page background" },
  };
}

test("production token source validates and resolves to an existing default palette", () => {
  const data = readTokens();
  assert.ok(data.themes[data.defaultTheme]);
  assert.ok(Object.keys(data.tokens).length > 0);
  assert.ok(Object.keys(data.themes[data.defaultTheme].values).length > 0);
});

test("theme key ordering is irrelevant, while every theme must supply the same roles", () => {
  assert.doesNotThrow(() => validateTokens(fixture()));
  for (const change of [
    data => { delete data.themes.indigo.values["color-text"]; },
    data => { data.themes.indigo.values["color-extra"] = "#123456"; },
  ]) {
    const data = fixture();
    change(data);
    assert.throws(() => validateTokens(data), /different role set/);
  }
});

test("malformed token, theme, role, and alias names cannot become CSS selectors or properties", () => {
  for (const name of ["bad name", "BadName", "x]body", "--unsafe", "a;display", ""] ) {
    const tokenData = fixture();
    tokenData.tokens[name] = { type: "dimension", value: "2px", description: "Unsafe key" };
    assert.throws(() => validateTokens(tokenData), /invalid token/);

    const themeData = fixture();
    themeData.themes[name] = structuredClone(themeData.themes.jade);
    assert.throws(() => validateTokens(themeData), /invalid theme/);

    const roleData = fixture();
    for (const theme of Object.values(roleData.themes)) theme.values[name] = "#123456";
    roleData.roles[name] = "Unsafe role";
    assert.throws(() => validateTokens(roleData), /invalid theme value/);

    const aliasData = fixture();
    aliasData.aliases[name] = "color-text";
    assert.throws(() => validateTokens(aliasData), /invalid alias/);
  }
});

test("CSS-breaking declarations and remote URL values are rejected before generation", () => {
  for (const value of ["8px; color:red", "8px}body{display:none", "</style>", "url(https://example.com/a)", "URL (https://example.com/a)", "/*", "8px /* hidden declaration", "*/8px", "4px\ncolor:red", "\u0000", "", "   ", Infinity, NaN, null, [], {}]) {
    const data = fixture();
    data.tokens["space-2"].value = value;
    assert.throws(() => tokenCSS(data), /invalid token space-2/);
  }
});

test("legacy aliases cannot redefine generated variables or claim the reserved namespace", () => {
  for (const name of ["lester-color-text", "lester-space-2", "lester-future-role"]) {
    const data = fixture();
    data.aliases[name] = "color-text";
    assert.throws(() => validateTokens(data), /alias|reserved|collision/);
  }
});

test("a palette with no semantic roles cannot pass simply because all themes are equally empty", () => {
  const data = fixture();
  for (const theme of Object.values(data.themes)) theme.values = {};
  data.roles = {};
  data.aliases = {};
  assert.throws(() => validateTokens(data), /nonempty|role|values/);
});

test("aliases must resolve directly to a token or palette role, never another alias", () => {
  for (const target of ["missing-role", "ink", "gap", "var(--ink)", null]) {
    const data = fixture();
    data.aliases.secondary = target;
    assert.throws(() => validateTokens(data), /invalid alias secondary/);
  }
});

test("token references resolve to known design variables and cannot form cycles", () => {
  const valid = fixture();
  valid.tokens["shadow-focus"] = {
    type: "shadow",
    value: "0 0 0 3px color-mix(in srgb, var(--lester-color-text) 16%, transparent)",
    description: "Focus ring referencing a theme role",
  };
  assert.doesNotThrow(() => validateTokens(valid));

  const unknown = fixture();
  unknown.tokens["space-2"].value = "var(--lester-missing-space)";
  assert.throws(() => validateTokens(unknown), /unknown reference space-2 -> missing-space/);

  const self = fixture();
  self.tokens["space-2"].value = "var(--lester-space-2)";
  assert.throws(() => validateTokens(self), /cyclic token reference/);

  const cycle = fixture();
  cycle.tokens["space-2"].value = "var(--lester-font-weight-medium)";
  cycle.tokens["font-weight-medium"].value = "var(--lester-space-2)";
  assert.throws(() => validateTokens(cycle), /cyclic token reference/);
});

test("palette colors must be explicit hex values and cannot collide with global tokens", () => {
  for (const value of ["red", "#fff", "#12345g", "#123456789", "var(--ink)", 123456]) {
    const data = fixture();
    data.themes.jade.values["color-text"] = value;
    assert.throws(() => validateTokens(data), /invalid theme value jade.color-text/);
  }
  const data = fixture();
  for (const theme of Object.values(data.themes)) theme.values["space-2"] = "#FFFFFF";
  data.roles["space-2"] = "A colliding palette role";
  assert.throws(() => validateTokens(data), /invalid theme value jade.space-2/);
});

test("missing descriptions and an absent default theme produce actionable errors", () => {
  const token = fixture();
  token.tokens["space-2"].description = " ";
  assert.throws(() => validateTokens(token), /invalid token space-2/);
  const role = fixture();
  delete role.roles["color-text"];
  assert.throws(() => validateTokens(role), /missing role description for color-text/);
  const theme = fixture();
  theme.defaultTheme = "absent";
  assert.throws(() => validateTokens(theme), /defaultTheme/);
});

test("generated CSS rebinds legacy aliases within each theme scope", () => {
  const css = tokenCSS(fixture());
  // Inherited CSS custom properties resolve aliases where they are declared;
  // redeclaring each alias is necessary for nested palette previews to work.
  const scopes = [...css.matchAll(/(:root|\[data-design-theme="[^"]+"\])\s*\{([^}]+)\}/g)];
  assert.equal(scopes.length, 3);
  for (const [, , body] of scopes) {
    assert.match(body, /--ink: var\(--lester-color-text\);/);
    assert.match(body, /--gap: var\(--lester-space-2\);/);
  }
  assert.match(scopes.find(([, selector]) => selector.includes('"indigo"'))[2], /--lester-color-text: #252E4A;/);
  assert.equal(css, tokenCSS(fixture()), "generation must be deterministic");
});

test("readTokens parses and validates the supplied file instead of silently falling back", () => {
  const directory = mkdtempSync(join(tmpdir(), "lester-token-test-"));
  try {
    const file = join(directory, "tokens.json");
    writeFileSync(file, JSON.stringify(fixture()));
    assert.equal(readTokens(file).themes.indigo.values["color-text"], "#252E4A");
    writeFileSync(file, JSON.stringify({ ...fixture(), version: 999 }));
    assert.throws(() => readTokens(file), /expected version 1/);
    writeFileSync(file, "{broken");
    assert.throws(() => readTokens(file), SyntaxError);
  } finally {
    rmSync(directory, { recursive: true, force: true });
  }
});
