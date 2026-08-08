import test from "node:test";
import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";

const html = await readFile(new URL("./index.html", import.meta.url), "utf8");
const app = await readFile(new URL("./app.mjs", import.meta.url), "utf8").catch(() => "");
const source = `${html}\n${app}`;

test("loads the viewer controller as an external module", () => {
  assert.match(html, /<script type="module" src="\.\/app\.mjs"><\/script>/);
  assert.doesNotMatch(html, /<script type="module">/);
});

test("uses one WebSocket input client instead of per-event HTTP requests", () => {
  assert.match(source, /createInputClient/);
  assert.doesNotMatch(source, /fetch\(["']\/input["']/);
});

test("uses pointer capture so button release survives leaving the video", () => {
  assert.match(source, /setPointerCapture/);
  assert.match(source, /pointerup/);
  assert.match(source, /lostpointercapture/);
});

test("uses capture dimensions reported by runtime stats", () => {
  assert.match(source, /runtime\.captureWidth/);
  assert.match(source, /runtime\.captureHeight/);
  assert.match(source, /pointerCoordinates/);
});

test("requires one explicit start action and requests sound from that action", () => {
  assert.match(html, /<button id="start" type="button">Start stream<\/button>/);
  assert.match(source, /createPlaybackStarter/);
  assert.doesNotMatch(html, /<video id="video"[^>]*\bmuted\b/);
});

test("provides touch-friendly clipboard and keyboard controls", () => {
  assert.match(html, /id="copy"/);
  assert.match(html, /id="paste"/);
  assert.match(html, /id="keyboard"/);
  assert.match(html, /id="mobile-keyboard"/);
  assert.match(source, /clipboardAPI\(\)\.readText/);
  assert.match(source, /clipboardAPI\(\)\.writeText/);
});

test("renders every viewer control in a toolbar below the video", () => {
  const headerEnd = html.indexOf("</header>");
  const videoStart = html.indexOf('<video id="video"');
  const toolbarStart = html.indexOf('<div class="player-toolbar"');
  const toolbarEnd = html.indexOf("</div>", toolbarStart);

  assert.ok(headerEnd >= 0, "header must exist");
  assert.ok(videoStart > headerEnd, "video must be outside the header");
  assert.ok(toolbarStart > videoStart, "viewer toolbar must be after the video");
  assert.ok(toolbarEnd > toolbarStart, "viewer toolbar must be closed");

  const toolbar = html.slice(toolbarStart, toolbarEnd);
  for (const id of ["quality", "start", "sound", "copy", "paste", "keyboard", "fullscreen"]) {
    assert.match(toolbar, new RegExp(`id=["']${id}["']`));
  }
});
