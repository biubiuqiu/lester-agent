#!/usr/local/bin/python
"""Exercise the installed runtime as its normal user, without Internet access."""
import os
import pathlib
import subprocess
import tempfile

from playwright.sync_api import sync_playwright

assert os.getuid() != 0, "run the check as the image's sandbox user"
subprocess.run(["sh", "-lc", "node --version && npm --version && pnpm --version && python --version && pip --version && go version && playwright --version && git --version && rg --version"], check=True)

with tempfile.TemporaryDirectory(prefix="runtime-check-", dir="/workspace") as directory:
    root = pathlib.Path(directory)
    # Compile real programs, not just version banners; Go must work offline.
    (root / "hello.go").write_text('package main\nimport "fmt"\nfunc main() { fmt.Print("go-ready") }\n')
    result = subprocess.check_output(["go", "run", str(root / "hello.go")], text=True, env={**os.environ, "GOPROXY": "off", "GOSUMDB": "off"})
    assert result == "go-ready"
    (root / "hello.c").write_text('#include <stdio.h>\nint main(void) { puts("cc-ready"); }\n')
    subprocess.run(["cc", str(root / "hello.c"), "-o", str(root / "hello")], check=True)
    assert subprocess.check_output([str(root / "hello")], text=True).strip() == "cc-ready"
    subprocess.run(["python", "-m", "venv", str(root / "venv")], check=True)
    subprocess.run([str(root / "venv/bin/python"), "-m", "pip", "--version"], check=True)

    with sync_playwright() as p:
        browser = p.chromium.launch()
        context = browser.new_context(record_video_dir=str(root / "videos"))
        page = context.new_page()
        page.set_content('<!doctype html><html lang="zh"><title>runtime-ready</title><h1>沙盒已就绪</h1></html>')
        assert page.title() == "runtime-ready"
        assert page.locator("h1").inner_text() == "沙盒已就绪"
        assert page.screenshot().startswith(b"\x89PNG\r\n\x1a\n")
        video = page.video
        context.close()
        assert video is not None and pathlib.Path(video.path()).stat().st_size > 0
        browser.close()

    # ESM and CommonJS must resolve preinstalled modules from conversation dirs.
    subprocess.run(["node", "--input-type=module", "-e", """
import { chromium } from 'playwright';
import { createRequire } from 'node:module';
const require = createRequire(import.meta.url);
if (!require('@playwright/test').test) throw new Error('test runner unavailable');
const browser = await chromium.launch();
try {
  const page = await browser.newPage();
  await page.setContent('<!doctype html><title>node-ready</title><h1>Node ready</h1>');
  if (await page.title() !== 'node-ready') throw new Error('wrong title');
  const png = await page.screenshot();
  if (png.subarray(0, 8).toString('hex') !== '89504e470d0a1a0a') throw new Error('invalid screenshot');
} finally { await browser.close(); }
"""], cwd=root, check=True)

print("Runtime check passed: toolchains, venv, Python + Node Chromium, screenshots and video.")
