import { defineConfig } from "@playwright/test";
import { tmpdir } from "node:os";
import { join } from "node:path";
export default defineConfig({
  testDir: "./demo", workers: 1, retries: 0, timeout: 180_000,
  outputDir: join(process.env.RUNNER_TEMP || tmpdir(), "lester-walkthrough"),
  use: { baseURL: "http://127.0.0.1:13020", viewport: { width: 1440, height: 960 }, locale: "zh-CN", serviceWorkers: "block", video: { mode: "on", size: { width: 1440, height: 960 } }, launchOptions: { executablePath: process.env.PLAYWRIGHT_CHROMIUM_EXECUTABLE_PATH } },
  webServer: { command: "node scripts/serve-e2e.mjs", url: "http://127.0.0.1:13020", timeout: 30_000, reuseExistingServer: false },
});
