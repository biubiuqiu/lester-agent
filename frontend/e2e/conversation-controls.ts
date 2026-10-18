import { expect, type Page } from "@playwright/test";

export async function openConversationRecords(page: Page) {
  const records = page.locator(".conversation-records");
  await expect(records).toBeVisible();
  if (await records.getAttribute("open") === null) await records.locator(":scope > summary").click();
}

export async function openConversationOptions(page: Page) {
  const menu = page.getByRole("button", { name: "更多会话选项", exact: true });
  if (await menu.getAttribute("aria-expanded") !== "true") await menu.click();
}
