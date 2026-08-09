import test from "node:test";
import assert from "node:assert/strict";

import { clipboardShortcut, createInputControl, createInputState, keyboardInput, normalizeRemoteKey, pointerCoordinates, shouldForwardKeyboard, touchGesture } from "./input.mjs";

test("stops sending input while per-viewer control is disabled", () => {
  const sent = [];
  const control = createInputControl({
    send(input) {
      sent.push(input);
      return true;
    },
    release() {},
  });

  assert.equal(control.send({ type: "move", x: 1, y: 2 }), true);
  control.setEnabled(false);
  assert.equal(control.send({ type: "keydown", key: "a", code: "KeyA" }), false);
  control.setEnabled(true);
  assert.equal(control.send({ type: "keyup", key: "a", code: "KeyA" }), true);
  assert.deepEqual(sent, [
    { type: "move", x: 1, y: 2 },
    { type: "keyup", key: "a", code: "KeyA" },
  ]);
});

test("releases held input once when per-viewer control is disabled", () => {
  let releases = 0;
  const control = createInputControl({
    send() {},
    release() {
      releases += 1;
    },
  });

  control.setEnabled(false);
  control.setEnabled(false);

  assert.equal(releases, 1);
  assert.equal(control.enabled, false);
});

test("can start with per-viewer control disabled", () => {
  const control = createInputControl({
    initialEnabled: false,
    send() {
      throw new Error("disabled control sent input");
    },
    release() {},
  });

  assert.equal(control.enabled, false);
  assert.equal(control.send({ type: "move", x: 1, y: 2 }), false);
});

test("forwards keyboard input from the video or page", () => {
  assert.equal(shouldForwardKeyboard({ tagName: "VIDEO", isContentEditable: false }), true);
  assert.equal(shouldForwardKeyboard({ tagName: "BODY", isContentEditable: false }), true);
});

test("maps the Mac Command modifier to remote Control", () => {
  assert.deepEqual(normalizeRemoteKey({ key: "Meta", code: "MetaLeft" }), { key: "Control", code: "ControlLeft" });
  assert.deepEqual(normalizeRemoteKey({ key: "c", code: "KeyC" }), { key: "c", code: "KeyC" });
});

test("recognizes Command or Control clipboard shortcuts", () => {
  assert.equal(clipboardShortcut({ metaKey: true, ctrlKey: false, code: "KeyC" }), "copy");
  assert.equal(clipboardShortcut({ metaKey: false, ctrlKey: true, code: "KeyV" }), "paste");
  assert.equal(clipboardShortcut({ metaKey: false, ctrlKey: false, code: "KeyC" }), null);
});

test("deduplicates repeated key and mouse button transitions", () => {
  const state = createInputState();
  assert.equal(state.accept({ type: "keydown", key: "a", code: "KeyA" }), true);
  assert.equal(state.accept({ type: "keydown", key: "a", code: "KeyA" }), false);
  assert.equal(state.accept({ type: "keyup", key: "a", code: "KeyA" }), true);
  assert.equal(state.accept({ type: "keyup", key: "a", code: "KeyA" }), false);
  assert.equal(state.accept({ type: "down", x: 10, y: 20 }), true);
  assert.equal(state.accept({ type: "down", x: 10, y: 20 }), false);
  assert.equal(state.accept({ type: "up", x: 10, y: 20 }), true);
  assert.equal(state.accept({ type: "up", x: 10, y: 20 }), false);
});

test("releases held input when the viewer loses focus", () => {
  const state = createInputState();
  state.accept({ type: "keydown", key: "Shift", code: "ShiftLeft" });
  state.accept({ type: "down", x: 50, y: 60 });
  assert.deepEqual(state.releaseAll(), [
    { type: "keyup", key: "Shift", code: "ShiftLeft" },
    { type: "up", x: 50, y: 60 },
  ]);
  assert.deepEqual(state.releaseAll(), []);
});

test("keeps keyboard input local inside viewer controls", () => {
  for (const tagName of ["INPUT", "TEXTAREA", "SELECT", "BUTTON"]) {
    assert.equal(shouldForwardKeyboard({ tagName, isContentEditable: false }), false);
  }
  assert.equal(shouldForwardKeyboard({ tagName: "DIV", isContentEditable: true }), false);
});

test("maps a scaled 720p viewer back to the 1080p capture coordinates", () => {
  const got = pointerCoordinates(
    { clientX: 640, clientY: 360 },
    { left: 0, top: 0, width: 1280, height: 720 },
    { width: 1920, height: 1080 },
  );
  assert.deepEqual(got, { x: 960, y: 540 });
});

test("maps pointer coordinates inside letterboxed fullscreen video content", () => {
  const got = pointerCoordinates(
    { clientX: 800, clientY: 50 },
    { left: 0, top: 0, width: 1600, height: 1000 },
    { width: 1280, height: 720 },
  );
  assert.deepEqual(got, { x: 640, y: 0 });
});

test("classifies a short stationary touch as a tap", () => {
  assert.equal(touchGesture({ dx: 0, dy: 0 }), "tap");
  assert.equal(touchGesture({ dx: 7, dy: 0 }), "tap");
});

test("classifies a moved touch as a drag", () => {
  assert.equal(touchGesture({ dx: 8, dy: 0 }), "drag");
});

test("forwards text from the mobile keyboard as paste input", () => {
  assert.deepEqual(keyboardInput("halo"), { type: "paste", text: "halo" });
  assert.equal(keyboardInput(""), null);
});

test("clamps pointer coordinates to the capture bounds", () => {
  const got = pointerCoordinates(
    { clientX: 2000, clientY: -20 },
    { left: 0, top: 0, width: 1280, height: 720 },
    { width: 1920, height: 1080 },
  );
  assert.deepEqual(got, { x: 1919, y: 0 });
});
