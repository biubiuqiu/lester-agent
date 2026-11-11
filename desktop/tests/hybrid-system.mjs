// Invoked by desktopcloud.TestHybridDesktopSystem with a private JSON-lines
// control channel. This deliberately stays outside the ordinary Node test glob:
// it needs PostgreSQL, the Go desktop API and a freshly built real sidecar.
import assert from 'node:assert/strict';
import { spawn } from 'node:child_process';
import { createHash, randomUUID } from 'node:crypto';
import { mkdir, readFile, readdir } from 'node:fs/promises';
import { join } from 'node:path';
import { createInterface } from 'node:readline';
import { setTimeout as delay } from 'node:timers/promises';
import { createCloudBridge } from '../main/cloud-bridge.mjs';
import { RunnerRPC } from '../main/sidecar.mjs';

const hash = value => createHash('sha256').update(value).digest('hex');
const control = createInterface({ input: process.stdin, crlfDelay: Infinity });
const lines = control[Symbol.asyncIterator]();
const report = value => process.stdout.write(`${JSON.stringify(value)}\n`);
const watchdog = setTimeout(() => { console.error('Hybrid desktop helper timed out'); process.exit(1); }, 75_000);
let runner, bridge;

class MemoryStorage {
  settings = new Map();
  secrets = new Map();
  secureAvailable() { return true; }
  getSetting(key, fallback = null) { return structuredClone(this.settings.has(key) ? this.settings.get(key) : fallback); }
  async setSetting(key, value) { this.settings.set(key, structuredClone(value)); }
  getSecret(key) { return this.secrets.get(key) ?? null; }
  async setSecret(key, value) { this.secrets.set(key, value); }
  async deleteSecret(key) { this.secrets.delete(key); }
}

async function eventually(read, accept, message) {
  const deadline = Date.now() + 20_000;
  while (Date.now() < deadline) {
    const value = await read();
    if (accept(value)) return value;
    await delay(20);
  }
  assert.fail(message);
}

try {
  const first = await lines.next();
  assert.equal(first.done, false, 'Go fixture configuration missing');
  const config = JSON.parse(first.value);
  const profile = hash(`${config.origin}\n${config.account_id}`).slice(0, 32);
  const storage = new MemoryStorage();
  await storage.setSetting('cloudOrigin', config.origin);
  await storage.setSetting(`cloudIdentity:${hash(config.origin)}`, {
    account_id: config.account_id, workspace_id: config.workspace_id, device_id: config.device_id,
  });
  await storage.setSetting('profileDevices', { [profile]: config.device_id });
  await storage.setSecret(`desktop-device:${profile}`, config.device_token);
  const local = {
    id: randomUUID(), name: 'Shared local fixture', source: 'local', provider: 'openai_compatible',
    protocol: 'openai', endpoint: config.provider_url, model_id: 'local-fixture',
    config: { context_window: 32768 }, share_with_cloud: true,
  };
  await storage.setSetting(`models:${profile}`, [local]);
  await mkdir(config.state_dir, { recursive: true });
  await mkdir(config.project_dir, { recursive: true });
  const child = spawn(config.runner_path, [], { stdio: ['pipe', 'pipe', 'pipe'] });
  runner = new RunnerRPC(child, { timeout: 20_000 });
  await runner.request('initialize', { state_dir: config.state_dir, profile_id: profile, device_id: config.device_id });
  await runner.request('configureModels', { models: [
    { ...Object.fromEntries(Object.entries(local).filter(([key]) => key !== 'share_with_cloud')), credential: config.local_credential },
    { id: `cloud:${config.cloud_model_id}`, name: 'Cloud fixture', source: 'cloud', provider: 'cloud', model_id: config.cloud_model_id },
  ] });
  const events = [];
  // This is the only Electron adapter: real HTTP fetch plus a fixture cookie jar.
  // The bridge itself still chooses include/omit and the real ws implementation.
  const session = { fromPartition() { return {
    fetch(url, options) {
      const headers = new Headers(options.headers);
      if (options.credentials === 'include') headers.set('Cookie', config.session_cookie);
      else assert.equal(headers.has('Cookie'), false);
      return fetch(url, { ...options, headers });
    },
    async clearStorageData() {},
  }; } };
  bridge = createCloudBridge({ storage, session, shell: { async openExternal() { assert.fail('unexpected browser login'); } },
    renewSession: async () => {}, onEvent: event => events.push(event) });
  bridge.attachRunner(runner);
  await eventually(async () => bridge.getState(), state => state.status === 'online', 'real desktop WebSocket did not connect');
  await bridge.sync();

  const project = await runner.request('project.open', { name: 'Hybrid system project', path: config.project_dir });
  const conversation = await runner.request('conversation.create', {
    project_id: project.id, model_id: `cloud:${config.cloud_model_id}`, title: 'Local tools with cloud inference', permission_mode: 'standard',
  });
  assert.equal(conversation.execution_target, 'local');
  const requestID = randomUUID();
  const submission = { conversation_id: conversation.id, request_id: requestID, content: 'Write hybrid.txt using the selected cloud model.' };
  const accepted = await runner.request('conversation.send', submission);
  assert.equal(accepted.accepted, true);
  const receipt = await eventually(() => runner.request('conversation.receipt', { request_id: requestID }),
    value => value.status !== 'running', 'local Harness did not finish through the cloud gateway');
  assert.equal(receipt.status, 'completed');
  assert.equal(receipt.id, accepted.run.id);
  assert.equal(await readFile(join(config.project_dir, 'hybrid.txt'), 'utf8'), 'Created by local tools through cloud inference.\n');
  const snapshot = await runner.request('conversation.get', { id: conversation.id });
  assert.equal(snapshot.messages.at(-1).content, 'Cloud inference and local tools completed.');
  assert.ok(snapshot.messages.some(message => message.role === 'tool' && message.tool_call_id === 'cloud-write'));
  const replay = await runner.request('conversation.send', submission);
  assert.equal(replay.accepted, false);
  assert.equal(replay.run.id, receipt.id);
  await bridge.sync();
  report({ type: 'ready', local_model_key: local.id, conversation_id: conversation.id, run_id: receipt.id });

  const command = await lines.next();
  assert.equal(command.done, false, 'Go reverse-inference result missing');
  assert.equal(JSON.parse(command.value).type, 'verify');
  // Reverse model requests carry real write schemas/calls, but inferLocal must
  // never start a local run, mutate its transcript or execute those tools.
  assert.deepEqual(await readdir(config.project_dir), ['hybrid.txt']);
  assert.deepEqual(await runner.request('conversation.get', { id: conversation.id }), snapshot);
  const directory = await bridge.request('conversations.list');
  assert.ok(directory.conversations.some(item => item.id === conversation.id && item.source === 'local'));
  const publicState = JSON.stringify({ state: bridge.getState(), events, settings: [...storage.settings] });
  for (const secret of [config.device_token, config.local_credential]) assert.equal(publicState.includes(secret), false);
  report({ type: 'verified', local_tool_file: true, receipt: true, reverse_inference_only: true });
} catch (error) {
  // Assertions contain fixture data only. Runner/provider diagnostics remain private.
  console.error(error.stack ?? String(error));
  process.exitCode = 1;
} finally {
  clearTimeout(watchdog);
  bridge?.disconnect();
  await runner?.close();
  control.close();
  process.stdin.destroy();
}
