type Message = { id: string; role: string; run_id?: string; seq?: number; created_at: string };
type Event = { id: number; run_id: string; type: string; payload: Record<string, unknown>; created_at: string };
export function orderedMessages<M extends Message>(messages: M[]): M[] {
  return messages.toSorted((a, b) => a.seq !== undefined && b.seq !== undefined ? a.seq - b.seq : Date.parse(a.created_at) - Date.parse(b.created_at));
}
// Accepted adjustments begin a new user turn within the same run.
export function runSegments<E extends Event>(events: E[]): E[][] {
  const segments: E[][] = [];
  for (const event of events.toSorted((a, b) => a.id - b.id)) {
    if (!segments.length || event.type === "RUN_INPUT_APPLIED") segments.push([]);
    segments[segments.length - 1].push(event);
  }
  return segments;
}
export function segmentOrder(events: Event[], messages: Message[]): number {
  const indexes = new Map(messages.map((message, index) => [message.id, index]));
  const inputs = events.flatMap(event => event.type === "RUN_INPUT_APPLIED" ? Array.isArray(event.payload.message_ids) ? event.payload.message_ids : [] : event.type === "RUN_STARTED" ? [event.payload.input_message_id] : []);
  const anchors = inputs.filter((id): id is string => typeof id === "string" && indexes.has(id)).map(id => indexes.get(id)!);
  if (anchors.length) return Math.max(...anchors) * 10 + 1;
  const runInput = messages.findIndex(message => message.role === "user" && message.run_id === events[0]?.run_id);
  if (runInput >= 0) return runInput * 10 + 1;
  // Old events have no message identifiers; preserve their timestamp fallback.
  const next = messages.findIndex(message => Date.parse(message.created_at) > Date.parse(events[0]?.created_at || ""));
  return next < 0 ? messages.length * 10 + 1 : next * 10 - 1;
}
