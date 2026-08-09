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
  assert.match(html, /<button id="start"[^>]*type="button"[^>]*>Start stream<\/button>/);
  assert.match(source, /createPlaybackStarter/);
  assert.doesNotMatch(html, /<video id="video"[^>]*\bmuted\b/);
});

test("renders Start stream over the video instead of in the toolbar", () => {
  const stageStart = html.indexOf('<div class="video-stage">');
  const videoStart = html.indexOf('<video id="video"', stageStart);
  const startButton = html.indexOf('<button id="start"', videoStart);
  const stageEnd = html.indexOf("</div>", startButton);
  const toolbarStart = html.indexOf('<div class="player-toolbar"', stageEnd);

  assert.ok(stageStart >= 0, "video stage must exist");
  assert.ok(videoStart > stageStart, "video must be inside the stage");
  assert.ok(startButton > videoStart, "start button must follow the video inside the stage");
  assert.ok(stageEnd > startButton, "video stage must close after the start button");
  assert.ok(toolbarStart > stageEnd, "toolbar must follow the video stage");
});

test("renders a per-viewer control toggle in the toolbar", () => {
  const toolbarStart = html.indexOf('<div class="player-toolbar"');
  const toolbarEnd = html.indexOf("</div>", toolbarStart);
  const toolbar = html.slice(toolbarStart, toolbarEnd);

  assert.match(toolbar, /<button id="control"[^>]*aria-pressed="false"[^>]*>Control: Off<\/button>/);
});

test("shows the Keyboard button only for touch input", () => {
  assert.match(html, /#keyboard\s*\{\s*display:\s*none/);
  const coarseStart = html.indexOf("@media (pointer: coarse)");
  const coarseEnd = html.indexOf("}", html.indexOf("}", coarseStart) + 1);
  assert.match(html.slice(coarseStart, coarseEnd + 1), /#keyboard\s*\{\s*display:\s*block/);
});

test("renders the volume control in a video sidebar", () => {
  const layoutStart = html.indexOf('<div class="video-layout">');
  const sidebarStart = html.indexOf('<aside class="sound-sidebar"', layoutStart);
  const toolbarStart = html.indexOf('<div class="player-toolbar"', sidebarStart);
  const sidebar = html.slice(sidebarStart, toolbarStart);

  assert.ok(layoutStart >= 0, "video layout must exist");
  assert.ok(sidebarStart > layoutStart, "sound sidebar must be inside the video layout");
  assert.ok(toolbarStart > sidebarStart, "toolbar must follow the sound sidebar");
  assert.match(sidebar, /<button id="sound"[^>]*aria-label="Mute"/);
  assert.match(sidebar, /<svg class="sound-icon sound-icon-on"/);
  assert.match(sidebar, /<svg class="sound-icon sound-icon-off"/);
  assert.match(sidebar, /<input id="volume" type="range"[^>]*min="0"[^>]*max="1"/);
  assert.match(source, /volume\.addEventListener\("input"/);
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
  for (const id of ["quality", "control", "copy", "paste", "keyboard", "fullscreen"]) {
    assert.match(toolbar, new RegExp(`id=["']${id}["']`));
  }
});
