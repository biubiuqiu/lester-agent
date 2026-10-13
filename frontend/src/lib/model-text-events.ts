type Event = { id: number; run_id: string; type: string; payload: Record<string, unknown> };
type Message = { id: string; role: string };

// The same run can continue after an accepted adjustment. Filter only the
// stream text whose exact completed message is already present in durable chat;
// preserve current streaming output, tool events and old events without IDs.
export function withoutPersistedModelText<E extends Event>(events: E[], messages: Message[]): E[] {
  const saved = new Set(messages.filter(message => message.role === "assistant").map(message => message.id));
  const pending = new Map<string, number[]>(), omit = new Set<number>();
  for (let index = 0; index < events.length; index++) {
    const event = events[index];
    if (event.type === "MODEL_STARTED") pending.set(event.run_id, []);
    if (event.type === "MODEL_DELTA" || event.type === "MODEL_TEXT") {
      const indexes = pending.get(event.run_id) || [];
      indexes.push(index);
      pending.set(event.run_id, indexes);
    }
    if (event.type === "MODEL_COMPLETED") {
      if (typeof event.payload.message_id === "string" && saved.has(event.payload.message_id)) for (const item of pending.get(event.run_id) || []) omit.add(item);
      pending.delete(event.run_id);
    }
  }
  return events.filter((_event, index) => !omit.has(index));
}
