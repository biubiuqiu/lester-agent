// Renderer-only regressions in an isolated headless browser. No Electron window,
// native input, runner, real account, provider request or user profile is opened.
import assert from 'node:assert/strict';
import { readFile, mkdtemp, mkdir } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { dirname, extname, join, resolve, sep } from 'node:path';
import { fileURLToPath } from 'node:url';
import http from 'node:http';
import { chromium, expect } from '@playwright/test';

const desktop = resolve(dirname(fileURLToPath(import.meta.url)), '..');
const renderer = join(desktop, 'renderer');
const artifacts = process.env.LESTER_DESKTOP_ARTIFACTS || await mkdtemp(join(tmpdir(), 'lester-renderer-qa-'));
await mkdir(artifacts, { recursive: true });
const mime = { '.html': 'text/html', '.js': 'text/javascript', '.css': 'text/css', '.svg': 'image/svg+xml', '.png': 'image/png', '.txt': 'text/plain' };
const server = http.createServer(async (request, response) => {
  try {
    const pathname = decodeURIComponent(new URL(request.url, 'http://localhost').pathname);
    const file = resolve(renderer, '.' + (pathname === '/' ? '/index.html' : pathname));
    if (!file.startsWith(renderer + sep)) throw new Error('outside fixture');
    response.setHeader('Content-Type', `${mime[extname(file)] || 'application/octet-stream'}; charset=utf-8`);
    response.end(await readFile(file));
  } catch { response.writeHead(404).end(); }
});
await new Promise(resolve => server.listen(0, '127.0.0.1', resolve));
const url = `http://127.0.0.1:${server.address().port}`;
let browser, page;
const errors = [];
try {
  const channel = process.env.LESTER_BROWSER_CHANNEL || (process.platform === 'win32' ? 'chrome' : undefined);
  browser = await chromium.launch({ headless: true, ...(channel ? { channel } : {}) });
  const context = await browser.newContext({ viewport: { width: 1366, height: 864 }, reducedMotion: 'reduce' });
  await context.addInitScript(() => {
    if (window !== window.top) return;
    localStorage.setItem('lester-desktop-locale', 'zh-CN');
    const projects = [{ id: 'p-1', name: 'lester-agent', path: '/mnt/g/codespace/lester-agent' }, { id: 'p-2', name: '个人项目', path: '/mnt/g/personal' }];
    const tasks = [
      { id: 'c-1', title: '优化客户端界面', project_id: 'p-1', project_name: 'lester-agent', status: 'completed', model_id: 'fixture', updated_at: '2026-11-14' },
      { id: 'c-2', title: '整理项目文档', project_id: 'p-1', project_name: 'lester-agent', status: 'completed', model_id: 'fixture', updated_at: '2026-11-13' },
      { id: 'c-3', title: '读书笔记', project_id: 'p-2', project_name: '个人项目', status: 'idle', model_id: 'fixture', updated_at: '2026-11-12' },
    ];
    const state = { profile_id: 'renderer-fixture', platform: 'win32', runner: { status: 'ready' }, secret_storage: { available: true }, cloud: { signed_in: false }, models: [{ id: 'fixture', name: '示例模型', provider: 'openai_compatible', model_id: 'fixture' }] };
    const calls = [];
    const messages = [
      { id: 'm-1', role: 'user', content: '请整理客户端的界面和交互，让项目与会话的关系更清楚。' },
      { id: 'm-2', role: 'assistant', content: '我们可以从项目导航、任务输入和设置三个地方开始。\n\n项目下直接展开会话；新任务保留清晰的项目和权限选择；模型与云端连接在设置中分别管理。所有发送仍需要你明确确认。' },
    ];
    window.__rendererFixture = { calls, tasks, state };
    window.lesterDesktop = {
      getState: async () => structuredClone(state), onEvent(handler) { window.__rendererFixture.emit = handler; }, closeCloudConversation: async () => {}, resizeCloudConversation: async () => {},
      chooseProject: async () => structuredClone(projects[0]),
      request: async (method, params) => {
        calls.push({ method, params });
        if (method === 'project.list') return structuredClone(projects);
        if (method === 'conversation.list') return structuredClone(tasks);
        if (method === 'conversation.get') return { conversation: structuredClone(tasks.find(task => task.id === params.id)), messages: structuredClone(messages), permissions: [], runs: [] };
        if (method === 'files.list') return { entries: [{ name: 'index.html', path: 'index.html', is_dir: false }, { name: 'README.md', path: 'README.md', is_dir: false }] };
        if (method === 'files.read') return { path: params.path, content: params.path === 'index.html' ? '<!doctype html><h1>Isolated preview</h1><button id="count">0</button><script>document.querySelector("button").onclick=e=>e.target.textContent=Number(e.target.textContent)+1</script>' : '# Renderer fixture\n' };
        if (method === 'git.diff') return { diff: '' };
        throw new Error(`Unexpected renderer fixture request: ${method}`);
      },
    };
  });
  page = await context.newPage();
  page.on('pageerror', error => errors.push(error.message));
  page.on('console', message => { if (message.type() === 'error') errors.push(message.text()); });
  await page.goto(url);
  await expect(page.locator('#project-picker')).toHaveValue('p-1');
  await expect(page.locator('#send')).toBeDisabled();
  await expect(page.locator('#chat')).toHaveClass(/is-new/);
  const group = page.locator('[data-project-key="local:p-1"]');
  await expect(group.locator('.conversation-row')).toHaveCount(2);
  await expect(group).toHaveAttribute('open', '');
  await expect(page.locator('#recent-section')).toBeHidden();
  await page.screenshot({ path: join(artifacts, 'desktop-new-task.png') });
  await page.setViewportSize({ width: 1552, height: 1014 });
  await page.screenshot({ path: join(artifacts, 'desktop-new-task-native.png') });
  await page.setViewportSize({ width: 1366, height: 864 });

  // Filtering temporarily opens a group without replacing its saved fold state.
  await group.locator('summary').click();
  await expect(group).not.toHaveAttribute('open', '');
  await page.locator('#search').fill('整理');
  await expect(group).toHaveAttribute('open', '');
  await expect(group.locator('.conversation-row')).toHaveCount(1);
  await page.locator('#search').fill('not-a-match');
  await expect(page.locator('#project-list')).toContainText('没有匹配');
  await page.locator('#search').fill('');
  await expect(group).not.toHaveAttribute('open', '');
  await group.locator('summary').click();

  // Settings navigation and six-language changes preserve authored drafts.
  const draft = '这个任务草稿需要在设置和会话之间保留。';
  await page.locator('#prompt').fill(draft);
  await expect(page.locator('#send')).toBeEnabled();
  await page.locator('#settings-button').click();
  await expect(page.locator('#settings-general')).toBeVisible();
  await page.locator('#language').selectOption('en');
  await page.locator('#settings-tab-models').click();
  await page.locator('#add-model').click();
  await page.locator('#model-form [name=name]').fill('An unsaved model');
  await page.locator('#model-form details summary').click();
  await page.locator('#model-form [name=protocol]').selectOption('anthropic');
  await page.locator('#settings-tab-cloud').click();
  await expect(page.locator('#settings-cloud')).toBeVisible();
  await page.locator('#settings-tab-models').click();
  await expect(page.locator('#model-form [name=name]')).toHaveValue('An unsaved model');
  await expect(page.locator('#model-form [name=protocol]')).toHaveValue('anthropic');
  await page.locator('#model-form details summary').click();
  await page.locator('#settings-tab-models').focus();
  await page.keyboard.press('End');
  await expect(page.locator('#settings-tab-cloud')).toHaveAttribute('aria-selected', 'true');
  await page.keyboard.press('Home');
  await expect(page.locator('#settings-tab-general')).toHaveAttribute('aria-selected', 'true');
  for (const locale of ['ja', 'ko', 'fr', 'es', 'zh-CN', 'en']) {
    await page.locator('#language').selectOption(locale);
    await expect(page.locator('html')).toHaveAttribute('lang', locale);
    assert.equal(await page.locator('[data-t]').evaluateAll(nodes => nodes.some(node => !node.textContent.trim() || node.textContent === node.dataset.t)), false);
  }
  await page.locator('#language').selectOption('zh-CN');
  await page.locator('#settings-tab-models').click();
  await page.screenshot({ path: join(artifacts, 'desktop-settings.png') });
  await page.locator('#close-settings').click();
  await expect(page.locator('#prompt')).toHaveValue(draft);

  await page.locator('[data-project-key="local:p-2"] .project-add').click();
  await expect(page.locator('#project-picker')).toHaveValue('p-2');
  await expect(page.locator('#prompt')).toHaveValue('');
  await page.locator('#prompt').fill('A separate project draft');
  await group.locator('.project-add').click();
  await expect(page.locator('#project-picker')).toHaveValue('p-1');
  await expect(page.locator('#prompt')).toHaveValue(draft);

  await group.locator('.conversation-row').first().click();
  await expect(page.locator('#chat')).not.toHaveClass(/is-new/);
  await expect(page.locator('#messages')).toContainText('项目导航');
  await page.locator('#prompt').fill('会话中的另一个草稿');
  await page.locator('#new-task').click();
  await expect(page.locator('#prompt')).toHaveValue(draft);
  await group.locator('.conversation-row').first().click();
  await expect(page.locator('#prompt')).toHaveValue('会话中的另一个草稿');

  // An older selected conversation remains in the bounded project history.
  await page.evaluate(() => {
    for (let index = 0; index < 9; index++) window.__rendererFixture.tasks.push({ id: `new-${index}`, title: `More recent ${index}`, project_id: 'p-1', status: 'completed', model_id: 'fixture', updated_at: '2026-11-15' });
  });
  await page.locator('#refresh').click();
  await expect(group.locator('.conversation-row')).toHaveCount(6);
  await expect(group.locator('[aria-current=page]')).toBeVisible();
  await group.locator('.show-more').click();
  await expect(group.locator('.conversation-row')).toHaveCount(11);

  // Workspace views retain read-only preview isolation and keyboard tab access.
  await page.locator('#workspace-toggle').click();
  await page.locator('#workspace-body').getByText('index.html', { exact: true }).click();
  const preview = page.frameLocator('.preview-frame').frameLocator('#document');
  await expect(preview.getByRole('heading')).toHaveText('Isolated preview');
  await preview.getByRole('button').click();
  await expect(preview.getByRole('button')).toHaveText('1');
  assert.equal(await preview.getByRole('heading').evaluate(() => typeof window.lesterDesktop === 'undefined'), true);
  assert.equal(await page.frameLocator('.preview-frame').locator('#document').getAttribute('sandbox'), 'allow-scripts');
  await page.screenshot({ path: join(artifacts, 'desktop-conversation.png') });
  await page.locator('[data-pane=files]').focus();
  await page.keyboard.press('ArrowRight');
  await expect(page.locator('[data-pane=diff]')).toHaveAttribute('aria-selected', 'true');
  await page.keyboard.press('ArrowRight');
  await expect(page.locator('[data-pane=terminal]')).toHaveAttribute('aria-selected', 'true');
  await page.locator('#close-workspace').click();
  await expect(page.locator('#workspace-toggle')).toBeFocused();

  await page.setViewportSize({ width: 760, height: 540 });
  await page.locator('#new-task').click();
  await page.locator('#prompt').fill(Array.from({ length: 30 }, (_, index) => `长草稿第 ${index + 1} 行`).join('\n'));
  const send = await page.locator('#send').boundingBox();
  const prompt = await page.locator('#prompt').boundingBox();
  assert.ok(send.y >= 0 && send.y + send.height <= 540, 'send stays within the short viewport');
  assert.ok(prompt.y >= 0 && prompt.height <= 181, 'long drafts scroll inside the composer');
  assert.ok(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth + 1), 'no horizontal page overflow');
  await page.screenshot({ path: join(artifacts, 'desktop-compact.png') });
  await page.locator('#settings-button').click();
  await page.locator('#settings-tab-models').click();
  await page.locator('#model-form [type=submit]').scrollIntoViewIfNeeded();
  const save = await page.locator('#model-form [type=submit]').boundingBox();
  assert.ok(save.y >= 0 && save.y + save.height < 540, 'settings forms scroll to their real submit action');
  await page.locator('#close-settings').click();

  // Pending permission details and a long running draft stay inside a short
  // window, without answering the permission or starting/stopping any process.
  await group.locator('[data-sidebar-key="task:local:c-1"]').click();
  await page.locator('#prompt').fill(Array(30).fill('Keep this running draft').join('\n'));
  await page.evaluate(() => {
    const emit = window.__rendererFixture.emit;
    emit({ type: 'runner', event: { conversation_id: 'c-1', type: 'RUN_STARTED', run_id: 'fixture-run', payload: {} } });
    emit({ type: 'runner', event: { conversation_id: 'c-1', type: 'PERMISSION_REQUESTED', payload: { id: 'fixture-permission', conversation_id: 'c-1', run_id: 'fixture-run', status: 'pending', tool: 'bash', arguments: { command: Array(30).fill('printf fixture-only').join('\n') } } } });
  });
  await page.locator('#approval summary').click();
  await page.locator('#stop').scrollIntoViewIfNeeded();
  const stop = await page.locator('#stop').boundingBox();
  assert.ok(stop.y >= 0 && stop.y + stop.height <= 540, 'stop remains reachable alongside expanded permission details');
  assert.ok(await page.locator('#approval').evaluate(node => node.scrollHeight > node.clientHeight), 'long approval details scroll within their own panel');

  // First-model setup is directly reachable and must not discard an existing
  // unsaved form when the user returns through the welcome action.
  await page.locator('#new-task').click();
  await page.evaluate(() => {
    const fixture = window.__rendererFixture;
    fixture.state.models = [];
    fixture.emit({ type: 'state', state: structuredClone(fixture.state) });
  });
  await expect(page.locator('#welcome-model')).toBeVisible();
  await page.locator('#welcome-model').click();
  await expect(page.locator('#settings-models')).toBeVisible();
  await expect(page.locator('#model-form [name=name]')).toHaveValue('An unsaved model');
  await page.locator('#cancel-model').scrollIntoViewIfNeeded();
  await page.locator('#cancel-model').click();
  await page.locator('#close-settings').click();
  await page.locator('#welcome-model').click();
  await expect(page.locator('#model-form [name=name]')).toHaveValue('');
  await page.locator('#model-form [name=name]').fill('Previous profile draft');
  await page.evaluate(() => {
    const fixture = window.__rendererFixture;
    fixture.state.profile_id = 'another-renderer-profile';
    fixture.emit({ type: 'state', state: structuredClone(fixture.state) });
  });
  await expect(page.locator('#model-form')).toBeHidden();
  await expect(page.locator('#model-form [name=name]')).toHaveValue('');
  await page.locator('#close-settings').click();

  const calls = await page.evaluate(() => window.__rendererFixture.calls);
  assert.equal(calls.some(call => ['conversation.send', 'conversation.create', 'permission.respond', 'terminal.open'].includes(call.method)), false, 'view changes never execute or approve a task');
  assert.deepEqual(await page.locator('img').evaluateAll(images => images.filter(image => !image.complete || !image.naturalWidth).map(image => image.src)), []);
  assert.deepEqual(errors, []);
  console.log(`Headless renderer checks passed: tree/search, six languages, drafts, settings, keyboard tabs, isolated preview and short-window approval/stop access. Viewports: 1366x864, 1552x1014, 760x540. Artifacts: ${artifacts}`);
} catch (error) {
  await page?.screenshot({ path: join(artifacts, 'failure.png') }).catch(() => {});
  console.error(`Renderer failure evidence: ${artifacts}`);
  throw error;
} finally {
  await browser?.close();
  server.closeAllConnections();
  await new Promise(resolve => server.close(resolve));
}
