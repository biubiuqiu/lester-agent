import { fileURLToPath } from 'node:url';
import { dirname, join, resolve } from 'node:path';
import { DesktopError } from './errors.mjs';

export const RUNNER_METHODS = new Set([
  'project.list', 'conversation.list', 'conversation.create', 'conversation.get', 'conversation.send',
  'conversation.stop', 'conversation.receipt', 'conversation.events', 'permission.respond', 'files.list',
  'files.read', 'git.diff', 'terminal.open', 'terminal.input', 'terminal.resize', 'terminal.close',
]);
export const CLOUD_METHODS = new Set([
  'models.list', 'projects.list', 'conversations.list', 'conversations.get', 'conversations.create',
  'conversations.send', 'conversations.stop', 'conversations.events', 'personal.state', 'personal.messages',
]);
export const UUID = /^[a-f0-9]{8}-[a-f0-9]{4}-[1-5][a-f0-9]{3}-[89ab][a-f0-9]{3}-[a-f0-9]{12}$/i;
export function object(value) { return value !== null && typeof value === 'object' && !Array.isArray(value); }
export function boundedObject(value, max = 1 << 20) {
  if (!object(value)) throw new DesktopError('INVALID_REQUEST', 'Expected an object.');
  let bytes; try { bytes = Buffer.byteLength(JSON.stringify(value)); } catch { throw new DesktopError('INVALID_REQUEST'); }
  if (bytes > max) throw new DesktopError('REQUEST_TOO_LARGE');
  return value;
}
export function assertTrustedSender(event, window, rendererPath) {
  const contents = window?.webContents;
  if (!contents || contents.isDestroyed() || event.sender !== contents || event.senderFrame !== contents.mainFrame) throw new DesktopError('UNTRUSTED_FRAME');
  try {
    const url = new URL(event.senderFrame.url);
    if (url.protocol !== 'file:' || url.host || url.search || resolve(fileURLToPath(url)) !== resolve(rendererPath)) throw new Error();
  } catch { throw new DesktopError('UNTRUSTED_FRAME'); }
}
export function allowedLocalNavigation(value, rendererPath, isMainFrame) {
  if(!isMainFrame && ['about:blank','about:srcdoc'].includes(value))return true;
  try {
    const url=new URL(value),expected=isMainFrame?rendererPath:join(dirname(rendererPath),'preview.html');
    return url.protocol==='file:' && !url.host && !url.search && resolve(fileURLToPath(url))===resolve(expected);
  } catch { return false; }
}
export function assertRunnerRequest(method, params) {
  if (!RUNNER_METHODS.has(method)) throw new DesktopError('METHOD_NOT_ALLOWED');
  boundedObject(params);
  // Directory grants and persistent storage locations are main-process only.
  for (const key of ['state_dir', 'profile_id', 'device_id', 'credential', 'api_key', 'project_path', 'root']) {
    if (Object.hasOwn(params, key)) throw new DesktopError('METHOD_NOT_ALLOWED');
  }
  return params;
}
export function profileIdentity(origin = '', account = '') {
  return `${origin}\n${account}`;
}
export function allowedCloudNavigation(value, origin) {
  try {
    const url = new URL(value);
    if (url.origin !== origin || url.username || url.password) return false;
    // No public pages, artifacts, login redirects or arbitrary remote origins.
    return /^\/app(?:\/(?:new|c\/[a-f0-9-]{36}|p\/[a-f0-9-]{36}(?:\/overview)?|settings(?:\/(?:models|profile|sandbox|skills|connectors|automations))?|artifacts|contexts|agents(?:\/[a-z0-9][a-z0-9-]{0,127}(?:\/edit)?)?))?\/?$/i.test(url.pathname);
  } catch { return false; }
}
export function validateCloudOrigin(value) {
  let url; try { url = new URL(value); } catch { throw new DesktopError('INVALID_CLOUD_ORIGIN'); }
  if (url.username || url.password || url.search || url.hash || (url.pathname !== '/' && url.pathname !== '')) throw new DesktopError('INVALID_CLOUD_ORIGIN');
  const loopback = ['localhost', '127.0.0.1', '[::1]'].includes(url.hostname);
  if (url.protocol !== 'https:' && !(url.protocol === 'http:' && loopback)) throw new DesktopError('HTTPS_REQUIRED');
  return url.origin;
}
export function externalURL(value) {
  let url; try { url = new URL(value); } catch { throw new DesktopError('INVALID_EXTERNAL_URL'); }
  if (url.protocol !== 'https:' || url.username || url.password) throw new DesktopError('INVALID_EXTERNAL_URL');
  return url.href;
}
export function validateCallback(value) {
  let url; try { url = new URL(value); } catch { return false; }
  return url.protocol === 'lester-desktop:' && url.hostname === 'auth' && url.pathname === '/callback' && !url.username && !url.password && !url.hash && value.length <= 8192;
}
export function clampBounds(value, size) {
  if (!object(value) || !['x','y','width','height'].every(key => Number.isFinite(value[key]))) throw new DesktopError('INVALID_VIEW_BOUNDS');
  const x = Math.min(Math.max(0, Math.round(value.x)), Math.max(0, size.width - 1));
  const y = Math.min(Math.max(0, Math.round(value.y)), Math.max(0, size.height - 1));
  return { x, y, width: Math.min(Math.max(1, Math.round(value.width)), size.width-x), height: Math.min(Math.max(1, Math.round(value.height)), size.height-y) };
}
