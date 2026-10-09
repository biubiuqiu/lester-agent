import test from "node:test";
import assert from "node:assert/strict";
import { authenticatedFetch, safeLoginReturn } from "./auth-client";

test("login return accepts only local workspace, admin and preview routes", () => {
  for (const path of ["https://evil.test/app", "//evil.test/app", "/app/../../login", "/\\evil.test/app", "/docs", "/preview/nope", "/app\n"]) assert.equal(safeLoginReturn(path), "/app");
  const path = "/preview/11111111-1111-1111-1111-111111111111?path=hello%20world.html";
  assert.equal(safeLoginReturn(path), path);
  assert.equal(safeLoginReturn("/app/settings/profile"), "/app/settings/profile");
});
test("parallel expired requests share one renewal and replay only rejected handlers", async context => {
  let refreshes = 0, statuses = 0, writes = 0;
  context.mock.method(globalThis, "fetch", async (input: string | URL | Request) => {
    const url = String(input);
    if (url.endsWith("/auth/session")) { statuses++; return Response.json({code:"access_required"}, {status:401}); }
    if (url.endsWith("/auth/refresh")) { refreshes++; await new Promise(resolve => setTimeout(resolve, 10)); return Response.json({ access_expires_at:new Date(Date.now()+7200000).toISOString(), refresh_expires_at:new Date(Date.now()+2592000000).toISOString() }); }
    writes++;
    return writes<=2 ? Response.json({code:"access_required"},{status:401}) : Response.json({ saved:true });
  });
  const results = await Promise.all([authenticatedFetch("/protected",{method:"POST",body:"data"}),authenticatedFetch("/protected",{method:"POST",body:"data"})]);
  assert.ok(results.every(result => result.ok));
  assert.equal(refreshes,1);assert.equal(statuses,1);assert.equal(writes,4);
});
test("ordinary domain 401 never retries a mutation or renews credentials", async context => {
  let calls=0;
  context.mock.method(globalThis,"fetch", async () => { calls++; return Response.json({error:"current password incorrect"},{status:401}); });
  assert.equal((await authenticatedFetch("/password",{method:"POST"})).status,401);
  assert.equal(calls,1);
});
