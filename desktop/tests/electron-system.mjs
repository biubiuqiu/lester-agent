// Explicit graphical system test: real Electron, sidecar, filesystem and PTY.
// Uses a deterministic provider fixture; no live model credentials are needed.
import assert from 'node:assert/strict';
import { mkdtemp, writeFile, readFile, mkdir, rm } from 'node:fs/promises';
import { createRequire } from 'node:module';
import { tmpdir } from 'node:os';
import { dirname, join, resolve } from 'node:path';
import { fileURLToPath, pathToFileURL } from 'node:url';
import http from 'node:http';
import { _electron as electron, expect } from '@playwright/test';

const require = createRequire(import.meta.url);
const desktop = resolve(dirname(fileURLToPath(import.meta.url)), '..');
const root = await mkdtemp(join(tmpdir(), 'lester-electron-system-'));
const project = join(root, 'project');
const artifacts = process.env.LESTER_DESKTOP_ARTIFACTS || join(root, 'artifacts');
await Promise.all([mkdir(project), mkdir(join(root, 'profile')), mkdir(artifacts, { recursive: true })]);
await writeFile(join(project, 'README.md'), '# Desktop fixture\n');
// Isolate app storage on every OS before the production entrypoint initializes.
// The test must not replace an installed app's system protocol registration.
const bootstrap = join(root, 'bootstrap.cjs');
await writeFile(bootstrap, `const {app}=require('electron');app.setPath('userData',${JSON.stringify(join(root, 'profile'))});app.setAsDefaultProtocolClient=()=>true;import(${JSON.stringify(pathToFileURL(join(desktop, 'main/index.mjs')).href)});`);
let calls = 0, app, page;
const errors = [], inputs = [];
const provider = http.createServer(async (request, response) => {
  let raw = '';
  for await (const data of request) raw += data;
  const body = JSON.parse(raw);
  inputs.push(body); calls++;
  response.setHeader('Content-Type', 'text/event-stream');
  const tool = calls === 1 ? { index: 0, id: 'write-html', type: 'function', function: { name: 'write', arguments: JSON.stringify({ path: 'index.html', content: '<!doctype html><title>Desktop preview</title><h1>Made locally</h1><button id="counter">0</button><script>document.querySelector("#counter").onclick=e=>e.target.textContent=Number(e.target.textContent)+1</script>', create_only: true }) } }
    : calls === 2 ? { index: 0, id: 'test-command', type: 'function', function: { name: 'bash', arguments: JSON.stringify({ command: 'printf desktop-test-ok' }) } } : null;
  response.write(`data: ${JSON.stringify({ choices: [{ index: 0, delta: tool ? { tool_calls: [tool] } : { content: 'The HTML is ready, and the local test passed.' }, finish_reason: null }] })}\n\n`);
  response.write(`data: ${JSON.stringify({ choices: [{ index: 0, delta: {}, finish_reason: tool ? 'tool_calls' : 'stop' }], usage: { prompt_tokens: 32, completion_tokens: 16 } })}\n\n`);
  response.end('data: [DONE]\n\n');
});
await new Promise(resolve => provider.listen(0, '127.0.0.1', resolve));
try {
  app = await electron.launch({ executablePath: require('electron'), args: [bootstrap, ...(process.env.LESTER_ELECTRON_NO_SANDBOX === '1' ? ['--no-sandbox'] : [])], env: { ...process.env }, timeout: 60_000 });
  await app.evaluate(({ dialog }, path) => { dialog.showOpenDialog = async () => ({ canceled: false, filePaths: [path] }); }, project);
  page = await app.firstWindow();
  page.on('pageerror', error => errors.push(error.message));
  page.on('console', message => { if (message.type() === 'error') errors.push(message.text()); });
  await page.waitForFunction(async () => (await window.lesterDesktop?.getState())?.runner?.status === 'ready');
  await expect(page.locator('#global-notice')).toBeHidden();
  await page.locator('#settings-button').click();
  await page.locator('#language').selectOption('en');
  await page.locator('#add-model').click();
  await page.locator('[name=name]').fill('Fixture model');
  await page.locator('[name=endpoint]').fill(`http://127.0.0.1:${provider.address().port}/v1`);
  await page.locator('[name=model_id]').fill('fixture');
  await page.locator('[name=api_key]').fill('fixture-key-no-real-secret');
  await page.locator('#model-form [type=submit]').click();
  await expect(page.locator('#model-form')).toBeHidden();
  await page.locator('#close-settings').click();
  await page.locator('#welcome-project').click();
  await expect(page.locator('#project-list')).toContainText('project');
  await page.locator('#prompt').fill('Create a local HTML preview and run a test');
  await page.locator('#send').click();
  await expect(page.locator('#approval')).toBeVisible({ timeout: 30_000 });
  assert.equal(calls, 2, 'Command must wait for user approval before the next model turn');
  await page.locator('#approval button').last().click();
  await expect(page.locator('#messages')).toContainText('The HTML is ready, and the local test passed.', { timeout: 30_000 });
  assert.match(await readFile(join(project, 'index.html'), 'utf8'), /Made locally/);
  assert.equal(calls, 3);
  assert.ok(inputs[2].messages.some(message => message.role === 'tool' && message.content.includes('desktop-test-ok')), 'Provider must receive observed local command output');
  await page.locator('#workspace-toggle').click();
  await page.locator('#workspace-body').getByText('index.html', { exact: true }).click();
  const document = page.frameLocator('.preview-frame').frameLocator('#document');
  await expect(document.getByRole('heading')).toHaveText('Made locally');
  await document.getByRole('button').click();
  await expect(document.getByRole('button')).toHaveText('1');
  assert.equal(await page.frameLocator('.preview-frame').locator('#document').evaluate(frame => frame.sandbox.value), 'allow-scripts');
  assert.equal(await document.getByRole('heading').evaluate(() => typeof window.lesterDesktop === 'undefined' && typeof require === 'undefined' && typeof process === 'undefined'), true);
  assert.deepEqual(await page.locator('img').evaluateAll(images => images.filter(image => !image.complete || !image.naturalWidth).map(image => image.src)), []);
  await page.screenshot({ path: join(artifacts, 'lester-desktop-local.png'), fullPage: true });
  await page.locator('[data-pane=terminal]').click();
  await page.getByRole('button', { name: 'Open terminal', exact: true }).click();
  await expect(page.locator('.xterm')).toBeVisible();
  await page.locator('.xterm-helper-textarea').focus();
  await page.keyboard.type('printf terminal-e2e-ok');
  await page.keyboard.press('Enter');
  await expect(page.locator('.xterm-screen')).toContainText('terminal-e2e-ok');
  await page.locator('#prompt').fill('draft survives reload');
  await page.reload();
  await page.waitForFunction(async () => (await window.lesterDesktop?.getState())?.runner?.status === 'ready');
  await page.locator('#conversation-list button').first().click();
  await expect(page.locator('#prompt')).toHaveValue('draft survives reload');
  assert.equal(JSON.stringify(await page.evaluate(() => window.lesterDesktop.getState())).includes('fixture-key-no-real-secret'), false);
  assert.deepEqual(errors, []);
  console.log(`Electron system test passed: real file, approval, command, isolated interactive preview, icons, PTY and draft recovery. Screenshots: ${artifacts}`);
} catch (error) {
  if (page) await page.screenshot({ path: join(artifacts, 'failure.png'), fullPage: true }).catch(() => {});
  console.error(`Electron system test failed. Artifacts: ${artifacts}`);
  if (errors.length) console.error(errors.join('\n'));
  throw error;
} finally {
  await app?.close();
  provider.closeAllConnections();
  await new Promise(resolve => provider.close(resolve));
  // Keep artifacts for inspection; all state belongs to this isolated fixture.
  await rm(join(root, 'profile'), { recursive: true, force: true });
}
