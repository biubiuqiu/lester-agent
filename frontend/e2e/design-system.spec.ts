import { test, expect, type Page } from "@playwright/test";
import { readFile } from "node:fs/promises";

const path = "/design-system.html";

async function paletteMetadata(page: Page) {
  const data = JSON.parse((await page.locator("#token-data").textContent())!) as { defaultTheme: string; themes: Record<string, unknown> };
  return { defaultTheme: data.defaultTheme, themes: Object.keys(data.themes) };
}

async function openWorkbench(page: Page) {
  const unexpected: string[] = [], errors: string[] = [];
  page.on("pageerror", error => errors.push(error.message));
  await page.route("**/*", route => {
    const request = route.request(), url = new URL(request.url());
    if (request.isNavigationRequest() && url.pathname === path) return route.continue();
    unexpected.push(`${request.method()} ${url.pathname}`);
    return route.abort();
  });
  await page.goto(path);
  await expect(page.locator("html")).toHaveClass(/is-enhanced/);
  return { unexpected, errors };
}

async function artworkColors(page: Page) {
  return page.evaluate(() => ({
    button: getComputedStyle(document.querySelector("#controls .primary-button")!).backgroundColor,
    icon: [...new Set(Array.from(document.querySelectorAll('.glyph-tile[data-kind="document"] svg[data-size="24"] :is(path,rect,circle,ellipse,polygon,polyline,line,stop)')).flatMap(node => {
      const computed = getComputedStyle(node);
      return node.tagName.toLowerCase() === "stop" ? [computed.stopColor] : [computed.fill, computed.stroke];
    }).filter(color => color !== "none" && !color.startsWith("url(")).sort())],
    logo: Array.from(document.querySelectorAll(".logo-primary svg stop")).map(stop => getComputedStyle(stop).stopColor),
    danger: getComputedStyle(document.querySelector('[data-state="danger"]')!).color,
  }));
}

test("the public workbench renders and responds without API, font or asset requests", async ({ page }, info) => {
  const observed = await openWorkbench(page);
  await expect(page).toHaveTitle("Lester · Design system");
  await expect(page.locator("main")).toBeVisible();
  await expect(page.locator(".logo-primary svg")).toBeVisible();
  await expect(page.locator(".glyph-tile")).toHaveCount(34);
  await expect(page.locator(".shadow-specimens figure")).toHaveCount(3);
  await page.locator("#motion-preview").click();
  await expect(page.locator("#motion-preview")).toHaveAttribute("aria-pressed", "true");
  await expect(page.locator("#motion-samples")).toHaveClass(/is-shifted/);
  await expect(page.locator("#token-rows tr")).not.toHaveCount(0);
  await expect(page.locator("body")).not.toContainText(/__[A-Z_]+__/);
  await page.getByRole("button", { name: "主操作", exact: true }).click();
  await expect(page.locator("#sample-feedback")).toContainText("没有发送网络请求");
  await expect(page.getByRole("button", { name: "不可用", exact: true })).toBeDisabled();
  await expect(page.getByRole("button", { name: "处理中", exact: true })).toBeDisabled();
  await page.locator("#brand").scrollIntoViewIfNeeded();
  await page.screenshot({ path: info.outputPath("design-brand-and-foundations.png") });
  expect(observed.unexpected).toEqual([]);
  expect(observed.errors).toEqual([]);
});

test("local palettes recolor brand controls while entity identities and state colors stay stable", async ({ page }) => {
  const observed = await openWorkbench(page);
  const { defaultTheme, themes } = await paletteMetadata(page);
  const alternatives = themes.filter(theme => theme !== defaultTheme);
  expect(alternatives.length).toBeGreaterThanOrEqual(2);
  await expect(page.locator("html")).toHaveAttribute("data-design-theme", defaultTheme);
  await expect(page.locator(`input[name="design-theme"][value="${defaultTheme}"]`)).toBeChecked();
  const baseline = await artworkColors(page);
  expect(baseline.logo.length).toBeGreaterThan(0);
  await expect(page.locator('.contrast-item[data-foreground="color-on-brand"] .contrast-result')).toHaveAttribute("data-pass", "true");
  let previous = baseline;
  for (const theme of alternatives) {
    await page.locator(`input[name="design-theme"][value="${theme}"]`).check();
    await expect(page.locator("html")).toHaveAttribute("data-design-theme", theme);
    const preview = await artworkColors(page);
    expect(preview.button).not.toEqual(previous.button);
    expect(preview.icon).toEqual(baseline.icon);
    expect(preview.logo).not.toEqual(previous.logo);
    expect(preview.danger).toEqual(baseline.danger);
    previous = preview;
  }

  // Override the actual foreground, then trigger recalculation: a static AA badge must fail here.
  await page.evaluate(() => document.documentElement.style.setProperty("--lester-color-text", "var(--lester-color-surface)"));
  await page.locator(`input[name="design-theme"][value="${defaultTheme}"]`).check();
  const contrast = page.locator('.contrast-item[data-foreground="color-text"] .contrast-result');
  await expect(contrast).toHaveAttribute("data-pass", "false");
  await expect(contrast).toContainText("1.00:1");
  await page.evaluate(() => document.documentElement.style.removeProperty("--lester-color-text"));
  await page.locator(`input[name="design-theme"][value="${alternatives[0]}"]`).check();
  await expect(contrast).toHaveAttribute("data-pass", "true");
  await page.reload();
  await expect(page.locator("html")).toHaveAttribute("data-design-theme", defaultTheme);
  expect(await artworkColors(page)).toEqual(baseline);
  expect(await page.context().cookies()).toEqual([]);
  expect(await page.evaluate(() => ({ local: localStorage.length, session: sessionStorage.length }))).toEqual({ local: 0, session: 0 });
  expect(observed.unexpected).toEqual([]);
  expect(observed.errors).toEqual([]);
});

test("colored entity art keeps filled silhouettes, local gradients and compact geometry", async ({ page }, info) => {
  const observed = await openWorkbench(page);
  const ids = await page.locator("svg [id]").evaluateAll(nodes => nodes.map(node => node.id));
  expect(ids.length).toBeGreaterThan(0);
  expect(new Set(ids).size).toBe(ids.length);
  for (const size of [16, 20, 24, 32]) {
    await page.locator(`input[name="icon-size"][value="${size}"]`).check();
    const art = await page.locator(`.glyph-tile .product-glyph[data-size="${size}"]`).evaluateAll(nodes => nodes.map(node => {
      const svg = node as SVGSVGElement, ownIds = new Set(Array.from(svg.querySelectorAll("[id]"), item => item.id));
      const shapes = Array.from(svg.querySelectorAll<SVGGraphicsElement>("path,rect,circle,ellipse,polygon,polyline,line")).filter(shape => !shape.closest("defs"));
      const colors = new Set<string>(), missingReferences: string[] = [];
      let filledShapes = 0, broadStrokes = 0;
      const color = (value: string) => {
        if (value === "none" || value === "transparent") return;
        const reference = value.match(/^url\(["']?(?:[^#)]*#)([^"')\s]+)["']?\)$/);
        if (reference) {
          if (!ownIds.has(reference[1])) { missingReferences.push(reference[1]); return; }
          const gradient = Array.from(svg.querySelectorAll("[id]")).find(item => item.id === reference[1]);
          gradient?.querySelectorAll("stop").forEach(stop => color(getComputedStyle(stop).stopColor));
          return;
        }
        const channels = value.match(/^rgba?\(([^)]+)\)$/)?.[1].replaceAll(",", " ").split(/[\s/]+/).filter(Boolean).slice(0, 3).map(Number);
        if (channels?.length === 3 && Math.max(...channels) - Math.min(...channels) >= 20) colors.add(channels.join(","));
      };
      for (const shape of shapes) {
        const computed = getComputedStyle(shape);
        if (Number(computed.opacity) === 0) continue;
        if (computed.fill !== "none" && Number(computed.fillOpacity) > 0) { filledShapes++; color(computed.fill); }
        if (computed.stroke !== "none" && Number(computed.strokeOpacity) > 0) { color(computed.stroke); if (parseFloat(computed.strokeWidth) >= 2.5) broadStrokes++; }
      }
      const box = svg.getBBox(), screen = svg.getBoundingClientRect();
      return { kind: svg.dataset.productGlyph, appearance: svg.dataset.glyphAppearance, optical: svg.dataset.opticalSize, colors: colors.size, filledShapes, broadStrokes, missingReferences, bounds: { x: box.x, y: box.y, right: box.x + box.width, bottom: box.y + box.height }, size: { width: screen.width, height: screen.height } };
    }));
    expect(art).toHaveLength(24);
    for (const glyph of art) {
      expect(glyph.appearance, glyph.kind).toBe("color");
      expect(glyph.optical, glyph.kind).toBe(size <= 20 ? "compact" : "regular");
      expect(glyph.colors, `${glyph.kind} must use actual chromatic paints, not unused colored defs`).toBeGreaterThanOrEqual(2);
      expect(glyph.filledShapes + glyph.broadStrokes, `${glyph.kind} must have a clear body rather than hairline-only art`).toBeGreaterThan(0);
      expect(glyph.missingReferences, glyph.kind).toEqual([]);
      expect(glyph.bounds.x, glyph.kind).toBeGreaterThanOrEqual(0);
      expect(glyph.bounds.y, glyph.kind).toBeGreaterThanOrEqual(0);
      expect(glyph.bounds.right, glyph.kind).toBeLessThanOrEqual(24);
      expect(glyph.bounds.bottom, glyph.kind).toBeLessThanOrEqual(24);
      expect(glyph.size.width, glyph.kind).toBeCloseTo(size, 1);
      expect(glyph.size.height, glyph.kind).toBeCloseTo(size, 1);
    }
    if (size === 16 || size === 24) {
      await page.locator("#icons").scrollIntoViewIfNeeded();
      await page.screenshot({ path: info.outputPath(`colored-entity-icons-${size}.png`) });
    }
  }
  expect(observed.unexpected).toEqual([]);
  expect(observed.errors).toEqual([]);
});

test("icons and tokens can be found, sized and exported without unresolved SVG colors", async ({ page }) => {
  await page.addInitScript(() => {
    const state = window as Window & { __designClipboard?: string[] };
    state.__designClipboard = [];
    Object.defineProperty(navigator, "clipboard", { configurable: true, value: { writeText: async (text: string) => { state.__designClipboard!.push(text); } } });
  });
  const observed = await openWorkbench(page);
  await page.locator("#icon-search").fill("document");
  const documentTile = page.locator('.glyph-tile[data-kind="document"]');
  await expect(documentTile).toBeVisible();
  await expect(page.locator(".glyph-tile:visible")).toHaveCount(1);
  await page.locator('input[name="icon-size"][value="16"]').check();
  await expect(documentTile.locator('svg[data-size="16"]')).toBeVisible();
  await expect(documentTile.locator('svg[data-size="24"]')).toBeHidden();
  const { defaultTheme, themes } = await paletteMetadata(page);
  const exportTheme = themes.find(theme => theme !== defaultTheme)!;
  await page.locator(`input[name="design-theme"][value="${exportTheme}"]`).check();
  await documentTile.click();
  await expect(page.locator("#page-status")).toContainText("SVG");
  const svg = await page.evaluate(() => (window as Window & { __designClipboard?: string[] }).__designClipboard!.at(-1)!);
  expect(svg).toContain('<svg');
  expect(svg).toContain('width="16"');
  expect(svg).not.toContain("var(");
  expect(svg).not.toContain("currentColor");
  expect(svg).not.toContain("aria-hidden");
  const exported = await page.evaluate(async text => {
    const document = new DOMParser().parseFromString(text, "image/svg+xml");
    const ids = new Set(Array.from(document.querySelectorAll("[id]"), node => node.id));
    const references = Array.from(text.matchAll(/url\(([^)]+)\)/g), match => match[1].replace(/["']/g, "").trim());
    const image = new Image();
    await new Promise<void>((resolve, reject) => {
      image.onload = () => resolve();
      image.onerror = () => reject(new Error("Exported SVG did not load as an independent image"));
      image.src = `data:image/svg+xml;charset=utf-8,${encodeURIComponent(text)}`;
    });
    const canvas = window.document.createElement("canvas"); canvas.width = 16; canvas.height = 16;
    const context = canvas.getContext("2d")!; context.drawImage(image, 0, 0, 16, 16);
    const pixels = context.getImageData(0, 0, 16, 16).data;
    let coloredPixels = 0;
    for (let index = 0; index < pixels.length; index += 4) {
      if (pixels[index + 3] >= 128 && Math.max(pixels[index], pixels[index + 1], pixels[index + 2]) - Math.min(pixels[index], pixels[index + 1], pixels[index + 2]) >= 20) coloredPixels++;
    }
    return { error: !!document.querySelector("parsererror"), gradientCount: document.querySelectorAll("linearGradient,radialGradient").length, referencesLocal: references.length > 0 && references.every(reference => reference.startsWith("#") && ids.has(reference.slice(1))), coloredPixels };
  }, svg);
  expect(exported.error).toBe(false);
  expect(exported.gradientCount).toBeGreaterThan(0);
  expect(exported.referencesLocal).toBe(true);
  expect(exported.coloredPixels).toBeGreaterThanOrEqual(8);
  await page.locator("#copy-css").click();
  await expect(page.locator("#page-status")).toContainText("已复制当前主题 CSS");
  const css = await page.evaluate(() => (window as Window & { __designClipboard?: string[] }).__designClipboard!.at(-1)!);
  expect(css).toContain(`/* Lester · ${exportTheme} */`);
  expect(css).toContain("--lester-color-brand:");
  expect(css).toContain("--lester-logo-tile:");
  const downloaded = page.waitForEvent("download");
  await page.locator("#download-json").click();
  const download = await downloaded;
  expect(download.suggestedFilename()).toBe("lester-design-tokens.json");
  const data = JSON.parse(await readFile((await download.path())!, "utf8"));
  expect(data.version).toBe(1);
  expect(Object.keys(data.themes)).toEqual(themes);
  expect(data.defaultTheme).toBe(defaultTheme);
  expect(data.tokens["icon-grid"].value).toBe(24);

  await page.locator("#icon-search").fill('<img src=x onerror="window.designXss=true">');
  await expect(page.locator("#icon-empty")).toBeVisible();
  expect(await page.evaluate(() => "designXss" in window)).toBe(false);
  await page.locator("#token-search").fill("color-brand");
  const visibleRows = page.locator("#token-rows tr:visible");
  expect(await visibleRows.count()).toBeGreaterThan(0);
  for (const text of await visibleRows.allTextContents()) expect(text.toLowerCase()).toContain("color-brand");
  await page.locator("#token-search").fill("nonexistent-token-name");
  await expect(page.locator("#token-empty")).toBeVisible();
  expect(observed.unexpected).toEqual([]);
  expect(observed.errors).toEqual([]);
});

test("clipboard rejection gives honest recovery feedback and 320px layout stays contained", async ({ page }, info) => {
  await page.addInitScript(() => Object.defineProperty(navigator, "clipboard", { configurable: true, value: { writeText: async () => { throw new Error("test clipboard unavailable"); } } }));
  const observed = await openWorkbench(page);
  await page.locator("#copy-css").click();
  await expect(page.locator("#page-status")).toContainText("复制未完成");
  await expect(page.locator("#page-status")).not.toContainText("已复制");
  await page.emulateMedia({ reducedMotion: "reduce" });
  expect(await page.locator(".motion-track i").first().evaluate(node => getComputedStyle(node).transitionDuration)).toBe("0s");
  await page.setViewportSize({ width: 320, height: 740 });
  await page.locator("#icons").scrollIntoViewIfNeeded();
  await expect(page.locator("#icon-search")).toBeVisible();
  expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBeLessThanOrEqual(320);
  await page.locator('input[name="icon-size"][value="32"]').check();
  await page.screenshot({ path: info.outputPath("design-icons-mobile-320.png") });
  expect(await page.locator(".glyph-tile").first().boundingBox()).not.toBeNull();
  expect(observed.unexpected).toEqual([]);
  expect(observed.errors).toEqual([]);
});

test.describe("without JavaScript", () => {
  test.use({ javaScriptEnabled: false });
  test("the complete default reference stays readable and interactive previews stay disabled", async ({ page }, info) => {
    await page.goto(path);
    await expect(page).toHaveTitle("Lester · Design system");
    const { defaultTheme } = await paletteMetadata(page);
    await expect(page.locator("html")).toHaveAttribute("data-design-theme", defaultTheme);
    await expect(page.locator(`input[name="design-theme"][value="${defaultTheme}"]`)).toBeChecked();
    await expect(page.locator(".logo-primary svg")).toBeVisible();
    await expect(page.locator('.glyph-tile[data-kind="document"] svg[data-size="24"]')).toBeVisible();
    await expect(page.locator("#token-rows tr")).not.toHaveCount(0);
    await expect(page.locator("noscript")).toBeVisible();
    await expect(page.locator("#copy-css")).toBeDisabled();
    await expect(page.locator("#download-json")).toBeDisabled();
    await expect(page.locator("#icon-search")).toBeDisabled();
    await expect(page.locator('input[name="design-theme"]:enabled')).toHaveCount(0);
    await expect(page.locator(".glyph-tile:enabled")).toHaveCount(0);
    await expect(page.locator("#tokens")).not.toContainText(/__[A-Z_]+__/);
    await page.screenshot({ path: info.outputPath("design-reference-no-javascript.png") });
  });
});
