import assert from "node:assert/strict";
import test from "node:test";
import { randomId } from "./random-id";

test("HTTP LAN UUID fallback uses random bytes with RFC v4 version and variant", t => {
  const original = Object.getOwnPropertyDescriptor(globalThis, "crypto")!;
  const nativeCrypto = crypto;
  Object.defineProperty(globalThis, "crypto", { configurable: true, value: { getRandomValues: nativeCrypto.getRandomValues.bind(nativeCrypto) } });
  t.after(() => Object.defineProperty(globalThis, "crypto", original));
  const a = randomId(), b = randomId();
  assert.match(a, /^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/);
  assert.notEqual(a, b);
});
