export type RecapBookmark = { enabled: boolean; seenAt: number; eventId: number; messageId: string };
export const recapAwayMs = 120_000;
export function recapChanged(bookmark: RecapBookmark, latest: { eventId: number; messageId: string }) {
  return latest.eventId > bookmark.eventId || latest.messageId !== bookmark.messageId;
}
export function shouldOfferRecap(bookmark: RecapBookmark, latest: { eventId: number; messageId: string }, now: number, draft: boolean) {
  return bookmark.enabled && (latest.eventId > 0 || Boolean(latest.messageId)) && bookmark.seenAt > 0 && now - bookmark.seenAt >= recapAwayMs && !draft && recapChanged(bookmark, latest);
}
export function recapExcerpt(text: string, limit = 400) {
  const chars = Array.from(text.trim());
  if (chars.length <= limit) return chars.join("");
  const prefix = chars.slice(0, limit - 1).join("");
  const boundary = prefix.lastIndexOf(" ");
  return (boundary > prefix.length * .7 ? prefix.slice(0, boundary) : prefix).trimEnd() + "…";
}
