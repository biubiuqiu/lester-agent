import assert from "node:assert/strict";
import test from "node:test";
import { guideFor, pageGuide, resumeGuide } from "./user-guides";

test("resume paused tours but replay completed tours from the beginning", () => {
  assert.equal(resumeGuide(undefined, 7), 0);
  assert.equal(resumeGuide({ topic: "welcome", step: 4, status: "skipped" }, 7), 4);
  assert.equal(resumeGuide({ topic: "welcome", step: 6, status: "completed" }, 7), 0);
  assert.equal(resumeGuide({ topic: "welcome", step: 19, status: "in_progress" }, 7), 6);
});

test("feature prompts belong to specific pages and never interrupt conversation editing", () => {
  assert.equal(pageGuide("/app"), undefined);
  assert.equal(pageGuide("/app/c/current"), undefined);
  assert.equal(pageGuide("/app/agents/new"), undefined);
  assert.equal(pageGuide("/app/settings/models"), "models");
  assert.equal(pageGuide("/app/contexts"), "contexts");
  assert.equal(guideFor("models").path, "/app/settings/models");
});
