export function platformPresentation(value) {
  const platform = ['darwin', 'win32', 'linux'].includes(value) ? value : 'other';
  const mac = platform === 'darwin';
  return { platform, settings: mac ? '⌘,' : 'Ctrl+,', newTask: mac ? '⌘N' : 'Ctrl+N' };
}

export function shortcutAction(event, platform) {
  if (event.altKey || event.repeat) return null;
  const primary = platform === 'darwin'
    ? event.metaKey && !event.ctrlKey
    : event.ctrlKey && !event.metaKey;
  if (!primary) return null;
  if (event.key === ',') return 'settings';
  if (event.key?.toLowerCase() === 'n' && !event.shiftKey) return 'newTask';
  return null;
}
