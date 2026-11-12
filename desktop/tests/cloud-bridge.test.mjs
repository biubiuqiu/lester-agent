import test from 'node:test';
import assert from 'node:assert/strict';
import { EventEmitter } from 'node:events';
import { createHash } from 'node:crypto';
import { createCloudBridge } from '../main/cloud-bridge.mjs';
import { publicError } from '../main/errors.mjs';

const ORIGIN = 'https://cloud.test';
const ACCOUNT = '11111111-1111-4111-8111-111111111111';
const WORKSPACE = '22222222-2222-4222-8222-222222222222';
const DEVICE = '33333333-3333-4333-8333-333333333333';
const MODEL = '44444444-4444-4444-8444-444444444444';
const CONVERSATION = '55555555-5555-4555-8555-555555555555';
const PROJECT = '66666666-6666-4666-8666-666666666666';
const SUBMISSION = '77777777-7777-4777-8777-777777777777';
const TOKEN = 's'.repeat(43);
const hash = value => createHash('sha256').update(value).digest('hex');
const profile = (origin = ORIGIN, account = ACCOUNT) => hash(`${origin}\n${account}`).slice(0, 32);
const json = (value, options) => new Response(JSON.stringify(value), options);
const tick = () => new Promise(resolve => setImmediate(resolve));
function deferred() {
  let resolve, reject;
  const promise = new Promise((yes, no) => { resolve = yes; reject = no; });
  return { promise, resolve, reject };
}

class MemoryStorage {
  constructor(secure = true) { this.settings = new Map(); this.secrets = new Map(); this.secure = secure; }
  secureAvailable() { return this.secure; }
  getSetting(key, fallback = null) { return structuredClone(this.settings.has(key) ? this.settings.get(key) : fallback); }
  async setSetting(key, value) { this.settings.set(key, structuredClone(value)); }
  getSecret(key) { return this.secrets.get(key) ?? null; }
  async setSecret(key, value) { this.secrets.set(key, value); }
  async deleteSecret(key) { this.secrets.delete(key); }
}

class FakeRunner extends EventEmitter {
  constructor() { super(); this.handlers = new Map(); this.requests = []; this.notifications = []; this.conversations = []; this.infer = () => Promise.resolve({ text: 'generated' }); }
  handleRequest(method, handler) { this.handlers.set(method, handler); return () => this.handlers.delete(method); }
  request(method, params, options) {
    this.requests.push({ method, params: structuredClone(params), options });
    if (method === 'conversation.list') return Promise.resolve({ conversations: this.conversations });
    return this.infer(method, params, options);
  }
  notify(method, params) { this.notifications.push({ method, params: structuredClone(params) }); }
}

function fixture(t, { signedIn = true, secure = true, route, renewSession } = {}) {
  const storage = new MemoryStorage(secure), events = [], authChanges = [], requests = [], external = [], sockets = [], cleared = [];
  const clock = { value: 1_700_000_000_000 };
  storage.settings.set('cloudOrigin', ORIGIN);
  if (signedIn) {
    storage.settings.set(`cloudIdentity:${hash(ORIGIN)}`, { account_id: ACCOUNT, workspace_id: WORKSPACE, device_id: DEVICE });
    storage.secrets.set(`desktop-device:${profile()}`, TOKEN);
  }
  class FakeWebSocket extends EventEmitter {
    constructor(url, options) { super(); this.url = url; this.options = options; this.readyState = 1; this.bufferedAmount = 0; this.sent = []; sockets.push(this); }
    send(value) { this.sent.push(JSON.parse(value)); }
    close() { if (this.readyState === 3) return; this.readyState = 3; this.emit('close'); }
    receive(value) { this.emit('message', Buffer.from(typeof value === 'string' ? value : JSON.stringify(value))); }
  }
  const defaultRoute = request => {
    const path = new URL(request.url).pathname;
    if (path === '/api/v1/auth/native/exchange') return json({ ok: true });
    if (path === '/api/v1/me') return json({ user_id: ACCOUNT, workspace_id: WORKSPACE });
    if (path === '/api/v1/desktop/devices' || path.endsWith('/token')) return json({ token: TOKEN });
    if (path === '/api/v1/desktop/models') return json({ models: [] });
    return json({ ok: true });
  };
  const session = { fromPartition(partition) { return {
    async fetch(url, options) {
      const record = { url, partition, ...options, headers: new Headers(options.headers), body: options.body === undefined ? undefined : JSON.parse(options.body) };
      requests.push(record);
      return route ? route(record, defaultRoute) : defaultRoute(record);
    },
    async clearStorageData() { cleared.push(partition); },
  }; } };
  const bridge = createCloudBridge({ storage, session, shell: { async openExternal(url) { external.push(url); } }, WebSocketFactory: FakeWebSocket, now: () => clock.value, renewSession,
    onEvent: value => events.push(structuredClone(value)), onAuthChanged: value => authChanges.push(structuredClone(value)) });
  t.after(() => bridge.disconnect());
  return { bridge, storage, events, authChanges, requests, external, sockets, cleared, clock };
}

async function beginLogin(f) {
  assert.deepEqual(await f.bridge.login(), { status: 'awaiting_browser' });
  const target = new URL(f.external.at(-1));
  return { target, callback: `lester-desktop://auth/callback?code=${'c'.repeat(43)}&state=${target.searchParams.get('state')}` };
}

test('native PKCE callback is single use and public state never contains exchange or device secrets', async t => {
  const exchange = deferred(), entered = deferred();
  const f = fixture(t, { signedIn: false, route(request, fallback) {
    if (new URL(request.url).pathname === '/api/v1/auth/native/exchange') { entered.resolve(); return exchange.promise; }
    return fallback(request);
  } });
  const { target, callback } = await beginLogin(f);
  assert.equal(target.origin, ORIGIN); assert.equal(target.pathname, '/app/native-auth'); assert.equal(target.searchParams.get('client'), 'desktop');
  assert.match(target.searchParams.get('state'), /^[-\w]{43}$/);
  assert.match(target.searchParams.get('challenge'), /^[-\w]{43}$/);
  assert.equal(target.searchParams.has('verifier'), false);
  const accepted = f.bridge.onCallback(callback);
  await entered.promise;
  await assert.rejects(f.bridge.onCallback(callback), { code: 'CLOUD_AUTH_FAILED' });
  assert.equal(f.requests.length, 1);
  const exchangeRequest = f.requests[0];
  assert.equal(exchangeRequest.body.code, 'c'.repeat(43));
  assert.equal(createHash('sha256').update(exchangeRequest.body.verifier).digest('base64url'), target.searchParams.get('challenge'));
  assert.equal(exchangeRequest.headers.has('Authorization'), false);
  exchange.resolve(json({ ok: true }));
  const state = await accepted;
  assert.equal(state.signed_in, true); assert.equal(state.account_id, ACCOUNT); assert.equal(state.workspace_id, WORKSPACE);
  assert.match(state.device_id, /^[a-f\d-]{36}$/);
  assert.equal(f.authChanges.length, 1);
  const visible = JSON.stringify({ state, events: f.events, auth: f.authChanges, settings: [...f.storage.settings] });
  for (const secret of [TOKEN, exchangeRequest.body.verifier, exchangeRequest.body.code]) assert.ok(!visible.includes(secret), 'private credential leaked into public state/settings');
  assert.equal([...f.storage.secrets.values()].includes(TOKEN), true);
  await assert.rejects(f.bridge.onCallback(callback), { code: 'CLOUD_AUTH_FAILED' });
  assert.equal(f.requests.filter(request => request.url.endsWith('/native/exchange')).length, 1);
});

test('invalid, expired, duplicate and uncertain native callbacks cannot replay the exchange', async t => {
  for (const invalid of ['state', 'duplicate', 'expired', 'failed']) {
    await t.test(invalid, async t => {
      const f = fixture(t, { signedIn: false, route(request, fallback) { return invalid === 'failed' && request.url.endsWith('/native/exchange') ? Promise.reject(new Error('private network details')) : fallback(request); } });
      const { callback } = await beginLogin(f);
      let value = callback;
      if (invalid === 'state') value = callback.replace(/state=.*/, 'state=wrong');
      if (invalid === 'duplicate') value += '&code=' + 'x'.repeat(43);
      if (invalid === 'expired') f.clock.value += 5 * 60_000 + 1;
      await assert.rejects(f.bridge.onCallback(value));
      await assert.rejects(f.bridge.onCallback(callback), { code: 'CLOUD_AUTH_FAILED' });
      assert.equal(f.requests.length, invalid === 'failed' ? 1 : 0);
      assert.equal(f.bridge.getState().signed_in, false);
    });
  }
});

test('switching origins while the native exchange body resolves cannot install the old account into the new profile', async t => {
  const entered = deferred(); let bodyController;
  const f = fixture(t, { signedIn: false, route(request, fallback) {
    if (!request.url.endsWith('/native/exchange')) return fallback(request);
    return new Response(new ReadableStream({ start(controller) { bodyController = controller; entered.resolve(); } }));
  } });
  const { callback } = await beginLogin(f);
  const pending = f.bridge.onCallback(callback);
  const rejected = assert.rejects(pending, { code: 'PROFILE_CHANGED' });
  await entered.promise;
  await f.bridge.configure({ origin: 'https://other.test' });
  // The response has already reached the caller, so resolving its buffered body
  // models the completion race even when transport cancellation comes too late.
  bodyController.enqueue(Buffer.from('{"ok":true}')); bodyController.close();
  await rejected;
  assert.equal(f.requests.length, 1); assert.equal(f.authChanges.length, 0);
  assert.equal(f.bridge.getState().origin, 'https://other.test');
  assert.equal(f.bridge.getState().signed_in, false); assert.equal(f.bridge.getState().account_id, '');
  assert.equal(f.storage.secrets.size, 0);
});

test('device bearer credentials only reach desktop endpoints and cookie requests use the isolated partition', async t => {
  const renewals = [], f = fixture(t, { renewSession: async value => renewals.push(value), route(request, fallback) {
    if (request.url.endsWith('/desktop/models')) return json({ models: [
      { id: MODEL, name: 'Cloud', enabled: true }, { id: 'shared', source: 'device', device_id: ACCOUNT },
      { id: 'self', source: 'device', device_id: DEVICE }, { id: 'disabled', enabled: false },
    ] });
    return fallback(request);
  } });
  assert.deepEqual((await f.bridge.listModels()).map(model => [model.id, model.source]), [[MODEL, 'cloud'], ['shared', 'device']]);
  await f.bridge.request('conversations.list');
  await f.bridge.request('projects.list');
  await f.bridge.request('conversations.get', { id: CONVERSATION });
  assert.equal(renewals.length, 2);
  for (const request of f.requests) {
    const desktop = new URL(request.url).pathname.startsWith('/api/v1/desktop/');
    assert.equal(request.headers.get('Authorization'), desktop ? `Bearer ${TOKEN}` : null);
    assert.equal(request.credentials, desktop ? 'omit' : 'include');
    assert.equal(request.headers.get('Origin'), ORIGIN);
    assert.equal(request.partition, f.bridge.getState().partition);
    assert.equal(request.redirect, 'error'); assert.equal(request.cache, 'no-store');
    assert.ok(!request.url.includes(TOKEN));
  }
});

test('without a secure native keyring cloud cookies use a memory-only partition', async t => {
  const f = fixture(t, { secure: false });
  const state = f.bridge.getState();
  assert.match(state.partition, /^lester-cloud-[a-f\d]{24}$/);
  assert.equal(state.partition.startsWith('persist:'), false);
  await f.bridge.request('projects.list');
  assert.equal(f.requests[0].partition, state.partition);
  assert.equal(f.requests[0].credentials, 'include');
  await f.bridge.logout();
  assert.deepEqual(f.cleared, [state.partition]);
});

test('cloud request allowlist rejects privileged methods, extra fields and path-shaped identifiers before fetch', async t => {
  const f = fixture(t);
  for (const [method, params] of [
    ['models.generate', {}], ['files.read', {}], ['constructor', {}],
    ['projects.list', { root: '/private' }], ['conversations.get', { id: '../secrets' }],
    ['conversations.send', { id: CONVERSATION, content: 'goal', submission_id: SUBMISSION, authorization: TOKEN }],
    ['conversations.create', { project_id: PROJECT, model_deployment_id: '../model' }],
  ]) await assert.rejects(f.bridge.request(method, params));
  assert.equal(f.requests.length, 0);
});

test('uncertain cloud mutations are reported once without leaking errors or replaying the submission', async t => {
  const f = fixture(t, { route() { throw new Error(`private provider detail ${TOKEN}`); } });
  const result = await f.bridge.request('conversations.send', { id: CONVERSATION, content: 'goal', submission_id: SUBMISSION, permission_mode: 'standard' }).then(() => assert.fail('lost mutation was accepted'), error => publicError(error));
  assert.equal(result.code, 'RESULT_UNKNOWN'); assert.ok(!JSON.stringify(result).includes(TOKEN));
  assert.equal(f.requests.length, 1); assert.equal(f.requests[0].body.submission_id, SUBMISSION);
  await tick(); assert.equal(f.requests.length, 1);
});

test('origin changes abort in-flight requests and prevent an old partition from authenticating the new profile', async t => {
  const entered = deferred(); let aborted = false;
  const f = fixture(t, { route(request) {
    entered.resolve();
    return new Promise((resolve, reject) => request.signal.addEventListener('abort', () => { aborted = true; reject(new DOMException('Aborted', 'AbortError')); }, { once: true }));
  } });
  const pending = f.bridge.request('conversations.list');
  const rejected = assert.rejects(pending, error => ['CLOUD_UNAVAILABLE', 'PROFILE_CHANGED'].includes(error.code));
  await entered.promise;
  const oldPartition = f.bridge.getState().partition;
  await f.bridge.configure({ origin: 'https://other.test' });
  await rejected;
  assert.equal(aborted, true); assert.equal(f.requests.length, 1);
  assert.equal(f.bridge.getState().signed_in, false); assert.notEqual(f.bridge.getState().partition, oldPartition);
  await assert.rejects(f.bridge.request('conversations.list'), { code: 'CLOUD_SIGN_IN_REQUIRED' });
  assert.equal(f.requests.length, 1);
});

test('logout clears the original scoped token and cookie partition even if the network result is lost', async t => {
  const f = fixture(t, { route() { throw new Error('lost logout response'); } });
  const partition = f.bridge.getState().partition;
  assert.equal((await f.bridge.logout()).signed_in, false);
  assert.equal(f.storage.secrets.has(`desktop-device:${profile()}`), false);
  assert.deepEqual(f.cleared, [partition]); assert.equal(f.requests.length, 1);
  assert.equal(f.requests[0].headers.has('Authorization'), false);
});

test('an old logout finishing after an origin switch preserves the newly selected signed-in profile', async t => {
  const entered = deferred(), response = deferred();
  const f = fixture(t, { route() { entered.resolve(); return response.promise; } });
  const nextOrigin = 'https://other.test', nextIdentity = { account_id: PROJECT, workspace_id: WORKSPACE, device_id: CONVERSATION };
  f.storage.settings.set(`cloudIdentity:${hash(nextOrigin)}`, nextIdentity);
  const nextSecretKey = `desktop-device:${profile(nextOrigin, PROJECT)}`;
  f.storage.secrets.set(nextSecretKey, 'n'.repeat(43));
  const logout = f.bridge.logout(); await entered.promise;
  await f.bridge.configure({ origin: nextOrigin });
  assert.equal(f.bridge.getState().signed_in, true);
  response.resolve(json({ ok: true })); await logout;
  assert.equal(f.bridge.getState().origin, nextOrigin);
  assert.equal(f.bridge.getState().signed_in, true, 'old logout cleared the new origin identity');
  assert.equal(f.bridge.getState().account_id, PROJECT);
  assert.equal(f.storage.secrets.get(nextSecretKey), 'n'.repeat(43));
  assert.equal(f.storage.secrets.has(`desktop-device:${profile()}`), false);
  assert.equal(f.cleared.length, 1); assert.notEqual(f.cleared[0], f.bridge.getState().partition);
});

test('sync publishes only explicitly shared model metadata and conversation directory fields', async t => {
  const f = fixture(t), runner = new FakeRunner();
  f.storage.settings.set(`models:${profile()}`, [
    { id: MODEL, name: 'Shared', source: 'local', provider: 'openai', model_id: 'example', share_with_cloud: true, config: { context_window: 32000, output_reserve: 2000 }, endpoint: 'https://private-provider.test', credential: 'private-provider-secret' },
    { id: PROJECT, name: 'Private', share_with_cloud: false },
  ]);
  runner.conversations = [{ id: CONVERSATION, project_name: 'Project', title: 'A task', status: 'completed', updated_at: '2026-11-10T00:00:00Z', path: '/private/workspace', project_path: '/private/project', messages: [{ content: 'PRIVATE TRANSCRIPT BODY' }] }];
  f.bridge.attachRunner(runner); await f.bridge.sync();
  const models = f.requests.find(request => request.url.endsWith('/models')).body.models;
  assert.deepEqual(models, [{ id: MODEL, name: 'Shared', source: 'local', provider: 'openai', model_id: 'example', context_window: 32000, output_reserve: 2000 }]);
  const published = f.requests.find(request => request.url.endsWith('/conversations')).body.conversations;
  assert.deepEqual(published, [{ id: CONVERSATION, project_name: 'Project', title: 'A task', status: 'completed', updated_at: '2026-11-10T00:00:00Z', revision: f.clock.value }]);
  const wire = JSON.stringify(f.requests.map(request => request.body));
  for (const privateValue of ['/private/', 'PRIVATE TRANSCRIPT BODY', 'private-provider-secret', 'private-provider.test']) assert.ok(!wire.includes(privateValue));
  await f.bridge.sync();
  assert.equal(f.requests.filter(request => request.url.endsWith('/conversations')).at(-1).body.conversations[0].revision, published[0].revision);
});

test('local runner streams cloud models through the scoped API with fragmented UTF-8 events', async t => {
  const events = [{ type: 'MODEL_DELTA', delta: '你好' }, { type: 'MODEL_COMPLETED', normalized_usage: { source: 'actual', input_tokens: 12 } }];
  const bytes = Buffer.from(events.map(value => JSON.stringify(value)).join('\n'));
  const split = bytes.indexOf(Buffer.from('你好')) + 1;
  const f = fixture(t, { route(request) {
    assert.equal(request.url, `${ORIGIN}/api/v1/desktop/models/${MODEL}/stream`);
    return new Response(new ReadableStream({ start(controller) { controller.enqueue(bytes.subarray(0, split)); controller.enqueue(bytes.subarray(split)); controller.close(); } }));
  } });
  const runner = new FakeRunner(); f.bridge.attachRunner(runner);
  const result = await runner.handlers.get('cloud.model.stream')({ model_id: `cloud:${MODEL}`, request: { model: 'provider-model', messages: [{ role: 'user', content: 'goal' }] } }, { id: 'local-model-call', signal: new AbortController().signal });
  assert.deepEqual(result, { done: true });
  assert.deepEqual(runner.notifications, events.map(event => ({ method: 'cloud.model.event', params: { request_id: 'local-model-call', event } })));
  assert.equal(f.requests.length, 1); assert.equal(f.requests[0].headers.get('Authorization'), `Bearer ${TOKEN}`); assert.equal(f.requests[0].credentials, 'omit');
});

test('local model cancellation aborts the cloud stream, preserves observed events and never replays', async t => {
  const entered = deferred(); let bodyController, fetchSignal;
  const f = fixture(t, { route(request) {
    fetchSignal = request.signal;
    const body = new ReadableStream({ start(controller) { bodyController = controller; request.signal.addEventListener('abort', () => controller.error(new DOMException('Aborted', 'AbortError')), { once: true }); entered.resolve(); } });
    return new Response(body);
  } });
  const runner = new FakeRunner(); f.bridge.attachRunner(runner);
  const cancellation = new AbortController();
  const pending = runner.handlers.get('cloud.model.stream')({ model_id: MODEL, request: {} }, { id: 'cancel-cloud', signal: cancellation.signal });
  const rejected = assert.rejects(pending);
  await entered.promise; bodyController.enqueue(Buffer.from('{"type":"MODEL_DELTA","delta":"observed"}\n')); await tick();
  cancellation.abort(); await rejected;
  assert.equal(fetchSignal.aborted, true); assert.equal(f.requests.length, 1);
  assert.deepEqual(runner.notifications, [{ method: 'cloud.model.event', params: { request_id: 'cancel-cloud', event: { type: 'MODEL_DELTA', delta: 'observed' } } }]);
  await tick(); assert.equal(f.requests.length, 1);
});

test('cloud model HTTP failures preserve only allowlisted kinds without exposing or replaying the response', async t => {
  for (const [status, body, kind] of [
    [502, { error_kind: 'authentication', error: 'private-provider-secret' }, 'authentication'],
    [502, { error_kind: 'rate_limit' }, 'rate_limit'],
    [502, { error_kind: 'context_length_exceeded' }, 'context_length_exceeded'],
    [502, { error_kind: 'transport' }, 'transport'],
    [502, { error_kind: 'private-provider-secret', error: TOKEN }, 'provider'],
    [400, { error: 'private-transcript' }, 'protocol'],
    [401, {}, 'authentication'],
    [429, {}, 'rate_limit'],
    [502, 'not JSON: private-provider-secret', 'provider'],
  ]) {
    await t.test(`${status}/${kind}/${typeof body}`, async t => {
      const f = fixture(t, { route() { return typeof body === 'string' ? new Response(body, { status }) : json(body, { status }); } });
      const runner = new FakeRunner(); f.bridge.attachRunner(runner);
      const result = await runner.handlers.get('cloud.model.stream')({ model_id: MODEL, request: {} }, { id: 'rejected-model', signal: new AbortController().signal });
      assert.deepEqual(result, { done: true });
      assert.deepEqual(runner.notifications, [{ method: 'cloud.model.event', params: { request_id: 'rejected-model', event: { type: 'RUN_FAILED', error_kind: kind } } }]);
      assert.equal(f.requests.length, 1);
    });
  }
});

test('the model deadline remains active after response headers until the streaming body finishes', async t => {
  t.mock.timers.enable({ apis: ['setTimeout'] });
  const entered = deferred(); let fetchSignal;
  const f = fixture(t, { route(request) {
    fetchSignal = request.signal;
    return new Response(new ReadableStream({ start(controller) {
      request.signal.addEventListener('abort', () => controller.error(new DOMException('Aborted', 'AbortError')), { once: true });
      entered.resolve();
    } }));
  } });
  const runner = new FakeRunner(); f.bridge.attachRunner(runner);
  const pending = runner.handlers.get('cloud.model.stream')({ model_id: MODEL, request: {} }, { id: 'deadline', signal: new AbortController().signal });
  const rejected = assert.rejects(pending);
  await entered.promise; await tick();
  t.mock.timers.tick(600_000); await rejected;
  assert.equal(fetchSignal.aborted, true); assert.equal(f.requests.length, 1); assert.equal(runner.notifications.length, 0);
});

test('cloud stream EOF requires an explicit completion or failure and never retries partial output', async t => {
  for (const [name, events, interrupted] of [
    ['empty', [], true],
    ['partial', [{ type: 'MODEL_DELTA', delta: 'observed' }], true],
    ['failed', [{ type: 'MODEL_DELTA', delta: 'observed' }, { type: 'RUN_FAILED', error_kind: 'transport' }], false],
  ]) {
    await t.test(name, async t => {
      const f = fixture(t, { route() { return new Response(events.map(event => JSON.stringify(event)).join('\n')); } });
      const runner = new FakeRunner(); f.bridge.attachRunner(runner);
      const result = runner.handlers.get('cloud.model.stream')({ model_id: MODEL, request: {} }, { id: name, signal: new AbortController().signal });
      if (interrupted) await assert.rejects(result, { code: 'CLOUD_STREAM_INTERRUPTED' });
      else assert.deepEqual(await result, { done: true });
      assert.deepEqual(runner.notifications, events.map(event => ({ method: 'cloud.model.event', params: { request_id: name, event } })));
      assert.equal(f.requests.length, 1);
    });
  }
});

test('cloud inference only uses explicitly shared local models and forwards stream/generate results', async t => {
  const f = fixture(t), runner = new FakeRunner(), stream = deferred();
  f.storage.settings.set(`models:${profile()}`, [{ id: MODEL, share_with_cloud: true }, { id: PROJECT, share_with_cloud: false }]);
  runner.infer = method => method === 'models.stream' ? stream.promise : Promise.resolve({ text: 'answer' });
  f.bridge.attachRunner(runner); const ws = f.sockets[0];
  assert.equal(ws.url, `${ORIGIN.replace('https:', 'wss:')}/api/v1/desktop/devices/${DEVICE}/connect`);
  assert.equal(ws.options.headers.Authorization, `Bearer ${TOKEN}`); assert.equal(ws.options.followRedirects, false); assert.equal(ws.options.maxPayload, 8 << 20);
  for (const model_key of [PROJECT, CONVERSATION]) ws.receive({ type: 'model.request', id: model_key, model_key, stream: true, request: {} });
  assert.equal(runner.requests.length, 0); assert.equal(ws.sent.filter(frame => frame.error_kind === 'transport').length, 2);
  ws.receive({ type: 'model.request', id: 'stream-call', model_key: MODEL, stream: true, request: { messages: [{ role: 'user', content: 'goal' }] } });
  ws.receive({ type: 'model.request', id: 'stream-call', model_key: MODEL, stream: true, request: {} });
  assert.equal(runner.requests.length, 1); assert.equal(runner.requests[0].method, 'models.stream'); assert.equal(runner.requests[0].params.model_id, MODEL);
  runner.emit('notification', { method: 'model.event', params: { id: 'unknown', event: { type: 'MODEL_DELTA', delta: 'stale' } } });
  runner.emit('notification', { method: 'model.event', params: { id: 'stream-call', event: { type: 'MODEL_DELTA', delta: 'observed' } } });
  assert.deepEqual(ws.sent.at(-1), { type: 'model.event', id: 'stream-call', event: { type: 'MODEL_DELTA', delta: 'observed' } });
  stream.resolve({ done: true }); await tick();
  assert.deepEqual(ws.sent.at(-1), { type: 'model.done', id: 'stream-call' });
  ws.receive({ type: 'model.request', id: 'generate-call', model_key: MODEL, stream: false, request: {} }); await tick();
  assert.equal(runner.requests.at(-1).method, 'models.generate');
  assert.deepEqual(ws.sent.at(-1), { type: 'model.done', id: 'generate-call', response: { text: 'answer' } });
  ws.receive({ type: 'model.request', id: 'generate-call', model_key: MODEL, stream: false, request: {} });
  assert.equal(runner.requests.length, 2); assert.deepEqual(ws.sent.at(-1), { type: 'model.done', id: 'generate-call', error_kind: 'protocol' });
  f.storage.settings.set(`models:${profile()}`, [{ id: MODEL, share_with_cloud: false }]);
  ws.receive({ type: 'model.request', id: 'revoked', model_key: MODEL, stream: true, request: {} });
  assert.equal(runner.requests.length, 2); assert.deepEqual(ws.sent.at(-1), { type: 'model.done', id: 'revoked', error_kind: 'transport' });
});

test('revoking sharing cancels active cloud inference and suppresses late results and duplicate requests', async t => {
  const f = fixture(t), runner = new FakeRunner(), active = deferred();
  f.storage.settings.set(`models:${profile()}`, [{ id: MODEL, share_with_cloud: true }]);
  runner.infer = () => active.promise;
  f.bridge.attachRunner(runner); const ws = f.sockets[0];
  const frame = { type: 'model.request', id: 'sharing-revoked', model_key: MODEL, stream: true, request: {} };
  ws.receive(frame);
  f.storage.settings.set(`models:${profile()}`, [{ id: MODEL, share_with_cloud: false }]);
  await f.bridge.sync();
  assert.deepEqual(runner.notifications, [{ method: 'models.cancel', params: { id: frame.id } }]);
  assert.deepEqual(ws.sent.at(-1), { type: 'model.done', id: frame.id, error_kind: 'transport' });
  runner.emit('notification', { method: 'model.event', params: { id: frame.id, event: { type: 'MODEL_DELTA', delta: 'late private output' } } });
  active.resolve({ done: true }); await tick();
  assert.equal(ws.sent.some(value => value.event?.delta === 'late private output'), false);
  assert.equal(ws.sent.some(value => value.type === 'model.done' && value.id === frame.id && !value.error_kind), false);
  f.storage.settings.set(`models:${profile()}`, [{ id: MODEL, share_with_cloud: true }]);
  ws.receive(frame);
  assert.equal(runner.requests.filter(value => value.method === 'models.stream').length, 1);
});

test('cancelled or disconnected cloud inference is never replayed by duplicate frames or transport reconnect', async t => {
  t.mock.timers.enable({ apis: ['setTimeout'] });
  const f = fixture(t), runner = new FakeRunner(), pending = deferred();
  f.storage.settings.set(`models:${profile()}`, [{ id: MODEL, share_with_cloud: true }]);
  runner.infer = () => pending.promise;
  f.bridge.attachRunner(runner); const ws = f.sockets[0];
  const frame = { type: 'model.request', id: 'cancelled-once', model_key: MODEL, stream: true, request: {} };
  ws.receive(frame); ws.receive({ type: 'model.cancel', id: frame.id });
  assert.deepEqual(runner.notifications, [{ method: 'models.cancel', params: { id: frame.id } }]);
  ws.receive(frame);
  assert.equal(runner.requests.length, 1, 'cancelled inference was executed again');
  assert.deepEqual(ws.sent.at(-1), { type: 'model.done', id: frame.id, error_kind: 'protocol' });
  pending.resolve({ done: true }); await tick();
  assert.equal(ws.sent.filter(value => value.id === frame.id && value.type === 'model.done' && !value.error_kind).length, 0);
  const active = deferred(); runner.infer = () => active.promise;
  const lost = { ...frame, id: 'lost-connection' }; ws.receive(lost); ws.close();
  assert.equal(runner.notifications.filter(value => value.params.id === lost.id).length, 1);
  t.mock.timers.tick(5000); await tick();
  assert.equal(f.sockets.length, 2); assert.equal(runner.requests.length, 2, 'reconnect replayed an accepted inference');
  f.sockets[1].receive(lost);
  assert.equal(runner.requests.length, 2, 'a repeated request after reconnect was replayed');
  assert.deepEqual(f.sockets[1].sent.at(-1), { type: 'model.done', id: lost.id, error_kind: 'protocol' });
  active.resolve({ done: true }); await tick();
  assert.equal(f.sockets[1].sent.some(value => value.id === lost.id && !value.error_kind), false);
});

test('advertised and streamed oversized JSON responses are cancelled at the body boundary', async t => {
  for (const advertised of [true, false]) {
    await t.test(String(advertised), async t => {
      let cancelled = false;
      const f = fixture(t, { route() {
        let chunks = 0;
        return new Response(new ReadableStream({ pull(controller) { if (chunks++ < 17) controller.enqueue(new Uint8Array(1 << 20).fill(32)); }, cancel() { cancelled = true; } }), { headers: advertised ? { 'Content-Length': String((16 << 20) + 1) } : {} });
      } });
      await assert.rejects(f.bridge.request('conversations.list'), { code: 'CLOUD_RESPONSE_TOO_LARGE' });
      assert.equal(cancelled, true, 'oversized response source was left running'); assert.equal(f.requests.length, 1);
    });
  }
});

test('cloud HTTP failures discard and cancel private response bodies while exposing only bounded status codes', async t => {
  for (const [status, code] of [[401, 'CLOUD_SIGN_IN_REQUIRED'], [409, 'CLOUD_CONFLICT'], [429, 'CLOUD_BUSY'], [503, 'CLOUD_REQUEST_FAILED']]) {
    await t.test(String(status), async t => {
      let cancelled = false;
      const f = fixture(t, { route() { return new Response(new ReadableStream({ start(controller) { controller.enqueue(Buffer.from(`private server detail ${TOKEN}`)); }, cancel() { cancelled = true; } }), { status }); } });
      const error = await f.bridge.request('conversations.list').then(() => assert.fail('HTTP error accepted'), error => publicError(error));
      assert.equal(error.code, code); assert.equal(cancelled, true); assert.ok(!JSON.stringify(error).includes(TOKEN)); assert.equal(f.requests.length, 1);
    });
  }
});

test('oversized, malformed or privileged local-to-cloud requests fail before contacting the API', async t => {
  const f = fixture(t), runner = new FakeRunner(); f.bridge.attachRunner(runner);
  const stream = runner.handlers.get('cloud.model.stream');
  for (const params of [{ model_id: '../model', request: {} }, { model_id: MODEL, request: [] }, { model_id: MODEL, request: {}, credential: TOKEN }, { model_id: MODEL, request: { messages: [{ role: 'user', content: 'x'.repeat((4 << 20) + 1) }] } }]) {
    await assert.rejects(stream(params, { id: 'invalid', signal: new AbortController().signal }));
  }
  assert.equal(f.requests.length, 0); assert.equal(runner.notifications.length, 0);
});

test('malformed and oversized model event bodies are cancelled without executing or replaying requests', async t => {
  for (const [name, content, code] of [['malformed', '{"type":', 'CLOUD_PROTOCOL_ERROR'], ['oversized', 'x'.repeat((8 << 20) + 1), 'CLOUD_RESPONSE_TOO_LARGE']]) {
    await t.test(name, async t => {
      let cancelled = false;
      const f = fixture(t, { route() { return new Response(new ReadableStream({ start(controller) { controller.enqueue(Buffer.from(content)); if (name === 'malformed') controller.close(); }, cancel() { cancelled = true; } })); } });
      const runner = new FakeRunner(); f.bridge.attachRunner(runner);
      await assert.rejects(runner.handlers.get('cloud.model.stream')({ model_id: MODEL, request: {} }, { id: name, signal: new AbortController().signal }), { code });
      assert.equal(f.requests.length, 1); assert.equal(runner.notifications.length, 0);
      if (name === 'oversized') assert.equal(cancelled, true);
    });
  }
});
