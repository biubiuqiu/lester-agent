import assert from "node:assert/strict";
import test from "node:test";
import { resolveWorkspaceReference } from "./workspace-reference";

test("preview navigation resolves conversation-relative pages and URL-encoded names", () => {
  assert.equal(resolveWorkspaceReference("site/index.html", "about.html"), "site/about.html");
  assert.equal(resolveWorkspaceReference("site/docs/page.html", "../index.html#intro"), "site/index.html");
  assert.equal(resolveWorkspaceReference("site/index.html", "/shared/%E4%B8%AD%E6%96%87%20page.html?v=1"), "shared/中文 page.html");
  assert.equal(resolveWorkspaceReference("site/index.html", "?v=2"), "site/index.html");
});

test("preview URLs reject external URLs and decoded path escapes", () => {
  for (const reference of ["../../secret", "%2e%2e/%2e%2e/secret", "..%2fsecret", "%5csecret", "%00.html", "https://example.com", "javascript:alert(1)", "//example.com/page", "#section", "%zz"]) {
    assert.equal(resolveWorkspaceReference("site/index.html", reference), "", reference);
  }
});
