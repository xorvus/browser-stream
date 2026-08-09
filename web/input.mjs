const INPUT_TAGS = new Set(["input", "textarea", "select", "button"]);

export function shouldForwardKeyboard(target) {
  if (!target) return true;
  const tagName = String(target.tagName || "").toLowerCase();
  if (INPUT_TAGS.has(tagName)) return false;
  return !target.isContentEditable;
}

export function createInputControl({ send, release }) {
  let enabled = true;
  return {
    get enabled() {
      return enabled;
    },
    setEnabled(value) {
      const next = Boolean(value);
      if (enabled && !next) release();
      enabled = next;
    },
    send(input) {
      return enabled ? send(input) : false;
    },
  };
}

const TOUCH_TAP_DISTANCE = 8;

export function touchGesture({ dx = 0, dy = 0 }) {
  return Math.hypot(dx, dy) < TOUCH_TAP_DISTANCE ? "tap" : "drag";
}

export function keyboardInput(text) {
  return text ? { type: "paste", text } : null;
}

export function normalizeRemoteKey({ key, code }) {
  if (code === "MetaLeft" || code === "MetaRight") {
    return { key: "Control", code: "ControlLeft" };
  }
  return { key, code };
}

export function clipboardShortcut({ metaKey, ctrlKey, code }) {
  if (!metaKey && !ctrlKey) return null;
  if (code === "KeyC") return "copy";
  if (code === "KeyV") return "paste";
  return null;
}

export function pointerCoordinates(event, rect, capture) {
  const width = Math.max(1, Number(capture?.width) || 1);
  const height = Math.max(1, Number(capture?.height) || 1);
  if (!(rect.width > 0) || !(rect.height > 0)) return { x: 0, y: 0 };

  const pictureAspect = width / height;
  const elementAspect = rect.width / rect.height;
  let pictureWidth = rect.width;
  let pictureHeight = rect.height;
  let pictureLeft = rect.left;
  let pictureTop = rect.top;
  if (elementAspect > pictureAspect) {
    pictureWidth = rect.height * pictureAspect;
    pictureLeft += (rect.width - pictureWidth) / 2;
  } else if (elementAspect < pictureAspect) {
    pictureHeight = rect.width / pictureAspect;
    pictureTop += (rect.height - pictureHeight) / 2;
  }

  const x = (event.clientX - pictureLeft) * width / pictureWidth;
  const y = (event.clientY - pictureTop) * height / pictureHeight;
  return {
    x: Math.min(width - 1, Math.max(0, x)),
    y: Math.min(height - 1, Math.max(0, y)),
  };
}

export function createInputState() {
  const keys = new Map();
  let pointerDown = null;
  return {
    accept(input) {
      switch (input.type) {
        case "keydown":
          if (keys.has(input.code)) return false;
          keys.set(input.code, { key: input.key, code: input.code });
          return true;
        case "keyup":
          if (!keys.has(input.code)) return false;
          keys.delete(input.code);
          return true;
        case "down":
          if (pointerDown) return false;
          pointerDown = { x: input.x, y: input.y };
          return true;
        case "up":
          if (!pointerDown) return false;
          pointerDown = null;
          return true;
        default:
          return true;
      }
    },
    releaseAll() {
      const releases = [...keys.values()].map(({ key, code }) => ({ type: "keyup", key, code }));
      keys.clear();
      if (pointerDown) releases.push({ type: "up", ...pointerDown });
      pointerDown = null;
      return releases;
    },
  };
}
