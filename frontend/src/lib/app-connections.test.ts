import assert from "node:assert/strict";
import { test } from "node:test";
import { appAuthorizationURL, appProviders, parseAppConnections } from "./app-connections";

test("authorization accepts fixed providers and binds callback and PKCE to this deployment", () => {
  const origin = "https://lester.example", state = "s".repeat(43), challenge = "p".repeat(43);
  const endpoints = ["https://github.com/login/oauth/authorize", "https://accounts.google.com/o/oauth2/v2/auth", "https://x.com/i/oauth2/authorize", "https://accounts.feishu.cn/open-apis/authen/v1/authorize", "https://slack.com/oauth/v2/authorize"];
  appProviders.forEach((provider, index) => {
    const url = new URL(endpoints[index]); url.search = new URLSearchParams({ client_id: "client-id", state, redirect_uri: `${origin}/api/v1/connectors/${provider}/callback`, code_challenge_method: "S256", code_challenge: challenge }).toString();
    assert.equal(appAuthorizationURL(provider, url.href, origin), url.href);
    for (const [key, value] of [["redirect_uri", "https://foreign.test/callback"], ["state", "short"], ["access_token", "must-not-leak"], ["client_secret", "secret"], ...(provider === "slack" ? [] : [["code_challenge_method", "plain"]])]) {
      const wrong = new URL(url); wrong.searchParams.set(key, value); assert.equal(appAuthorizationURL(provider, wrong.href, origin), null);
    }
    for (const target of [url.href.replace(url.hostname, "foreign.example"), url.href.replace("https:", "http:"), `${url.href}#secret`, url.href.replace("https://", "https://user:pass@")]) assert.equal(appAuthorizationURL(provider, target, origin), null);
  });
});

test("metadata never disguises malformed or incomplete connection state as an empty account", () => {
  const rows = appProviders.map(provider => ({ provider, configured: false, status: "disconnected", account_name: "", scopes: [], revision: 0 }));
  assert.equal(parseAppConnections(rows), rows);
  for (const invalid of [null, [], rows.slice(1), [...rows.slice(1), rows[1]], rows.map(row => ({ ...row, revision: -1 })), rows.map(row => ({ ...row, scopes: "gmail.readonly" }))]) assert.throws(() => parseAppConnections(invalid));
});

test("GitHub code consent is optional for old servers and cannot enable another app", () => {
  const rows = appProviders.map(provider => ({ provider, configured: true, status: "connected", account_name: "account", scopes: [], revision: 2 }));
  assert.equal(parseAppConnections(rows)[0].github_code_enabled ?? false, false);
  assert.equal(parseAppConnections(rows.map(row => ({ ...row, github_code_enabled: row.provider === "github" })))[0].github_code_enabled, true);
  assert.throws(() => parseAppConnections(rows.map(row => ({ ...row, github_code_enabled: "true" }))));
  assert.throws(() => parseAppConnections(rows.map(row => ({ ...row, github_code_enabled: true }))));
});
