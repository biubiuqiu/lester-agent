// Receipts must survive the same window closure as their preserved draft.
export function readAttemptStorage(key: string): string | null {
  if (typeof window === "undefined") return null;
  for (const get of [() => window.localStorage, () => window.sessionStorage]) try { const value = get().getItem(key); if (value) return value; } catch { /* Try legacy/tab storage. */ }
  return null;
}
export function writeAttemptStorage(key: string, value: string) {
  if (typeof window === "undefined") return;
  try { window.localStorage.setItem(key, value); return; } catch { /* Fall back in restricted browsers. */ }
  try { window.sessionStorage.setItem(key, value); } catch { /* In-memory tracking remains available. */ }
}
export function removeAttemptStorage(key: string) {
  if (typeof window === "undefined") return;
  for (const get of [() => window.localStorage, () => window.sessionStorage]) try { get().removeItem(key); } catch { /* best effort */ }
}
export function clearAttemptStorage(prefix: string) {
  if (typeof window === "undefined") return;
  for (const get of [() => window.localStorage, () => window.sessionStorage]) try { const storage = get(); for (const key of Object.keys(storage)) if (key.startsWith(prefix)) storage.removeItem(key); } catch { /* best effort */ }
}
