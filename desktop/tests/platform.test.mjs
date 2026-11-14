import test from 'node:test';
import assert from 'node:assert/strict';
import { platformPresentation, shortcutAction } from '../renderer/platform.js';
import { windowChrome } from '../main/window-chrome.mjs';

test('only macOS extends content into the native title bar', () => {
  assert.deepEqual(windowChrome('darwin'), { titleBarStyle: 'hiddenInset', trafficLightPosition: { x: 20, y: 18 } });
  for (const platform of ['win32', 'linux', undefined]) assert.deepEqual(windowChrome(platform), {});
  assert.equal(windowChrome('darwin').frame, undefined, 'native controls are retained');
  assert.equal(windowChrome('darwin').webPreferences, undefined, 'chrome does not override renderer isolation');
});

test('platform presentation has bounded metadata and consistent shortcuts', () => {
  assert.deepEqual(platformPresentation('darwin'), { platform: 'darwin', settings: '⌘,', newTask: '⌘N' });
  assert.equal(platformPresentation('win32').settings, 'Ctrl+,');
  assert.equal(platformPresentation('untrusted platform').platform, 'other');
});

test('Mac uses Command and Windows/Linux use Control without consuming text-editing shortcuts', () => {
  const event = { key: ',', metaKey: true, ctrlKey: false, altKey: false, shiftKey: false, repeat: false };
  assert.equal(shortcutAction(event, 'darwin'), 'settings');
  assert.equal(shortcutAction(event, 'win32'), null);
  assert.equal(shortcutAction({ ...event, key: 'n' }, 'darwin'), 'newTask');
  for (const key of ['c', 'v', 'a', 'z']) assert.equal(shortcutAction({ ...event, key }, 'darwin'), null);
  assert.equal(shortcutAction({ ...event, key: 'n', shiftKey: true }, 'darwin'), null);
  assert.equal(shortcutAction({ ...event, altKey: true }, 'darwin'), null);
  assert.equal(shortcutAction({ ...event, repeat: true }, 'darwin'), null);
  for (const platform of ['win32', 'linux']) {
    assert.equal(shortcutAction({ ...event, metaKey: false, ctrlKey: true }, platform), 'settings');
    assert.equal(shortcutAction({ ...event, key: 'n', metaKey: false, ctrlKey: true }, platform), 'newTask');
  }
});
