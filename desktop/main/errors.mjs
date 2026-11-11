export class DesktopError extends Error {
  constructor(code, message = code) { super(message); this.name = 'DesktopError'; this.code = code; }
}
export function publicError(error) {
  if (error instanceof DesktopError) return { code: error.code, message: error.message };
  return { code: 'DESKTOP_REQUEST_FAILED', message: 'The desktop request could not be completed.' };
}
