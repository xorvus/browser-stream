import test from "node:test";
import assert from "node:assert/strict";

import { deriveVideoFPS } from "./stats.mjs";

test("uses the browser supplied FPS when available", () => {
  const got = deriveVideoFPS(null, { timestamp: 1000, framesDecoded: 60, framesPerSecond: 59.4 });
  assert.equal(got.fps, 59.4);
});

test("derives FPS from decoded-frame deltas when the metric is missing", () => {
  const previous = { timestamp: 1000, framesDecoded: 100, fps: 60 };
  const got = deriveVideoFPS(previous, { timestamp: 6000, framesDecoded: 400 });
  assert.equal(got.fps, 60);
});

test("does not turn a missing first FPS sample into zero", () => {
  const got = deriveVideoFPS(null, { timestamp: 1000, framesDecoded: 0 });
  assert.equal(got.fps, null);
});

test("preserves the last valid FPS when a report cannot be compared", () => {
  const previous = { timestamp: 1000, framesDecoded: 100, fps: 58 };
  const got = deriveVideoFPS(previous, { timestamp: 1000, framesDecoded: 100 });
  assert.equal(got.fps, 58);
});
