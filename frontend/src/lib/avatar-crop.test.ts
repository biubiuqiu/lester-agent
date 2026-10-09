import test from "node:test";
import assert from "node:assert/strict";
import { cropGeometry, initialCrop } from "./avatar-crop";

test("portrait/landscape crops cover the square and never expose empty edges", () => {
  const landscape = cropGeometry(800, 400, { ...initialCrop, x: 999, y: -999 });
  assert.equal(landscape.scale, .8);
  assert.deepEqual(landscape.crop, { ...initialCrop, x: 160, y: 0 });
  const portrait = cropGeometry(400, 800, { ...initialCrop, x: -999, y: 999 });
  assert.deepEqual(portrait.crop, { ...initialCrop, x: 0, y: 160 });
});
test("zoom and quarter-turn rotation recalculate bounds for preview and export", () => {
  const result = cropGeometry(800, 400, { zoom: 2, rotation: 90, x: 999, y: -999 });
  assert.equal(result.scale, 1.6);
  assert.deepEqual(result.crop, { zoom: 2, rotation: 90, x: 160, y: -480 });
  assert.equal(cropGeometry(400, 400, { ...initialCrop, zoom: .1 }).crop.zoom, 1);
  assert.equal(cropGeometry(400, 400, { ...initialCrop, zoom: 9 }).crop.zoom, 4);
});
