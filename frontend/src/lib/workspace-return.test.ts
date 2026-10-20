import assert from "node:assert/strict";
import { test } from "node:test";
import { workspaceReturnPath } from "./workspace-return";

test("settings return paths preserve task, project and agent context without accepting redirects", () => {
  for (const value of ["/app", "/app/new", "/app/c/task-id", "/app/p/project-id", "/app/p/project-id/overview", "/app?agent=writer"]) assert.equal(workspaceReturnPath(value), value);
  for (const value of [null, undefined, "", "//evil.test/app", "https://evil.test/app", "/app/../login", "/app\\evil", "/app/settings/models", "/admin", "/app?returnTo=//evil.test", "/app/c/id\n"]) assert.equal(workspaceReturnPath(value), "/app");
});
