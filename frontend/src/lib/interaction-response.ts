function canonical(value: unknown): unknown {
  if (!value || typeof value !== "object") return value;
  if (Array.isArray(value)) return value.map(canonical);
  return Object.fromEntries(Object.entries(value).filter(([key, item]) => item !== undefined && !(item === "" && ["option_id", "text", "feedback", "decision"].includes(key))).sort(([a], [b]) => a.localeCompare(b)).map(([key, item]) => [key, canonical(item)]));
}

// Go omits empty optional response strings and orders object keys differently.
// Compare their exact effective fields without accepting a different answer.
export function sameInteractionResponse(a: unknown, b: unknown) {
  return JSON.stringify(canonical(a)) === JSON.stringify(canonical(b));
}
