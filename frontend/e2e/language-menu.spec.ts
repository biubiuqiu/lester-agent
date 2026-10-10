import { test, expect } from "@playwright/test";

test("SVG language menu supports keyboard selection, Escape and remembered preference", async ({ page, context }, testInfo) => {
  const errors: string[] = [], consoleErrors: string[] = [];
  page.on("console", message => { if (message.type() === "error") consoleErrors.push(message.text()); });
  page.on("pageerror", error => errors.push(error.message));
  await page.goto("/");
  const trigger = page.locator(".site-header .language-trigger");
  await expect(page.locator(".site-header .brand-mark")).toHaveCount(1);
  await expect(trigger.locator("svg.language-flag")).toHaveCount(1);
  await trigger.press("ArrowDown");
  const options = page.getByRole("menuitemradio");
  await expect(options).toHaveCount(6);
  await expect(options.locator("svg.language-flag")).toHaveCount(6);
  await expect(options.first()).toBeFocused();
  await expect(options.first()).toHaveAttribute("aria-checked", "true");
  await page.screenshot({ path: testInfo.outputPath("language-menu.png") });
  const menu = page.locator(".language-picker");
  expect(await menu.evaluate(element => {
    const rect = element.getBoundingClientRect();
    return rect.top >= 0 && rect.bottom <= innerHeight && rect.left >= 0 && rect.right <= innerWidth;
  })).toBe(true);
  await options.first().press("Escape");
  await expect(options).toHaveCount(0); await expect(trigger).toBeFocused();
  await trigger.click();
  await page.getByRole("heading", { level: 1 }).click({ position: { x: 8, y: 8 } });
  await expect(options).toHaveCount(0);
  await trigger.press("ArrowDown");
  await options.first().press("End");
  await expect(options.last()).toBeFocused();
  await options.last().press("Enter");
  await expect(page.locator("html")).toHaveAttribute("lang", "es");
  await expect(trigger).toHaveAttribute("aria-expanded", "false");
  expect((await context.cookies()).find(cookie => cookie.name === "lester_locale")?.value).toBe("es");
  await page.reload();
  await expect(trigger).toHaveAttribute("aria-label", "Idioma de la interfaz · Español");
  expect(errors).toEqual([]); expect(consoleErrors).toEqual([]);
});
