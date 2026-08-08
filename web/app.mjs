import { deriveVideoFPS } from "./stats.mjs";
import { createInputClient, inputWebSocketURL } from "./input-client.mjs";
import { clipboardShortcut, createInputState, keyboardInput, normalizeRemoteKey, pointerCoordinates, shouldForwardKeyboard, touchGesture } from "./input.mjs";
import { createPlaybackStarter } from "./playback.mjs";

const status = document.getElementById("status");
const video = document.getElementById("video");
const peer = new RTCPeerConnection();
const form = document.getElementById("url-form");
const browserURL = document.getElementById("browser-url");
const openButton = form.querySelector("button");
const fullscreen = document.getElementById("fullscreen");
const startButton = document.getElementById("start");
const sound = document.getElementById("sound");
const copy = document.getElementById("copy");
const paste = document.getElementById("paste");
const keyboard = document.getElementById("keyboard");
const mobileKeyboard = document.getElementById("mobile-keyboard");
const quality = document.getElementById("quality");
const mediaStream = new MediaStream();
const inputState = createInputState();
const captureSize = { width: 1920, height: 1080 };
let touchStart = null;
let previousVideoStats = null;

video.srcObject = mediaStream;

function setStatus(message, isError = false) {
  status.textContent = message;
  status.classList.toggle("error", isError);
}

const inputClient = createInputClient({
  url: inputWebSocketURL(),
  onStatus: (state) => {
    if (state === "disconnected" || state === "error") setStatus("Input reconnecting…", true);
  },
  onError: (error) => setStatus(`Input failed: ${error.message}`, true),
});

function pointer(event) {
  return pointerCoordinates(event, video.getBoundingClientRect(), captureSize);
}

function releaseRemoteInput() {
  for (const input of inputState.releaseAll()) inputClient.send(input);
}

function clipboardAPI() {
  if (!window.isSecureContext) {
    throw new Error("Clipboard requires the viewer at http://localhost:8080 or HTTPS");
  }
  if (!navigator.clipboard) throw new Error("Clipboard is unavailable in this browser");
  return navigator.clipboard;
}

async function pasteClipboard() {
  try {
    const text = await clipboardAPI().readText();
    const input = keyboardInput(text);
    if (input) inputClient.send(input);
  } catch (error) {
    setStatus(`Paste failed: ${error.message}`, true);
  }
}

async function copyClipboard() {
  try {
    const response = await fetch("/clipboard");
    if (!response.ok) throw new Error(await response.text());
    const { text } = await response.json();
    await clipboardAPI().writeText(text);
    setStatus(text ? "Copied selected text" : "No selected text to copy");
  } catch (error) {
    setStatus(`Copy failed: ${error.message}`, true);
  }
}

async function waitForIceGathering() {
  if (peer.iceGatheringState === "complete") return;
  await new Promise((resolve) => {
    peer.addEventListener("icegatheringstatechange", function complete() {
      if (peer.iceGatheringState !== "complete") return;
      peer.removeEventListener("icegatheringstatechange", complete);
      resolve();
    });
  });
}

async function connect() {
  const offer = await peer.createOffer();
  await peer.setLocalDescription(offer);
  await waitForIceGathering();
  const response = await fetch("/offer", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(peer.localDescription),
  });
  if (!response.ok) throw new Error(await response.text());
  await peer.setRemoteDescription(await response.json());
}

async function updateStats() {
  const reports = await peer.getStats();
  for (const report of reports.values()) {
    if (report.type !== "inbound-rtp" || report.kind !== "video") continue;
    if (peer.connectionState !== "connected" || !report.frameWidth || !report.frameHeight) continue;
    previousVideoStats = deriveVideoFPS(previousVideoStats, report);
    const fps = previousVideoStats.fps;
    setStatus(fps === null
      ? `${report.frameWidth}×${report.frameHeight} · Measuring FPS…`
      : `${report.frameWidth}×${report.frameHeight} · ${Math.round(fps)} FPS`);
  }
}

async function syncRuntimeProfile() {
  const response = await fetch("/stats");
  if (!response.ok) return;
  const runtime = await response.json();
  if (runtime.profile) quality.value = runtime.profile;
  if (runtime.captureWidth > 0) captureSize.width = runtime.captureWidth;
  if (runtime.captureHeight > 0) captureSize.height = runtime.captureHeight;
}

video.addEventListener("pointermove", (event) => {
  if (event.pointerType === "touch" && !touchStart) return;
  inputClient.send({ type: "move", ...pointer(event) });
});

video.addEventListener("pointerdown", (event) => {
  if (event.pointerType !== "touch" && event.button !== 0) return;
  event.preventDefault();
  video.focus({ preventScroll: true });
  video.setPointerCapture?.(event.pointerId);
  if (event.pointerType === "touch") {
    touchStart = { pointerId: event.pointerId, clientX: event.clientX, clientY: event.clientY };
    return;
  }
  const input = { type: "down", ...pointer(event) };
  if (inputState.accept(input)) inputClient.send(input);
});

video.addEventListener("pointerup", (event) => {
  if (event.pointerType !== "touch" && event.button !== 0) return;
  event.preventDefault();
  if (event.pointerType === "touch") {
    const start = touchStart;
    touchStart = null;
    if (start && start.pointerId === event.pointerId && touchGesture({ dx: event.clientX - start.clientX, dy: event.clientY - start.clientY }) === "tap") {
      const coordinates = pointer(event);
      inputClient.send({ type: "move", ...coordinates });
      inputClient.send({ type: "down", ...coordinates });
      inputClient.send({ type: "up", ...coordinates });
    }
    if (video.hasPointerCapture?.(event.pointerId)) video.releasePointerCapture(event.pointerId);
    return;
  }
  const input = { type: "up", ...pointer(event) };
  if (inputState.accept(input)) inputClient.send(input);
  if (video.hasPointerCapture?.(event.pointerId)) video.releasePointerCapture(event.pointerId);
});

video.addEventListener("pointercancel", releaseRemoteInput);
video.addEventListener("lostpointercapture", releaseRemoteInput);
video.addEventListener("blur", releaseRemoteInput);
window.addEventListener("blur", releaseRemoteInput);
window.addEventListener("pagehide", releaseRemoteInput);
document.addEventListener("visibilitychange", () => {
  if (document.visibilityState === "hidden") releaseRemoteInput();
});

window.addEventListener("keydown", (event) => {
  if (!shouldForwardKeyboard(event.target)) return;
  const shortcut = clipboardShortcut(event);
  if (shortcut) {
    event.preventDefault();
    if (shortcut === "copy") copyClipboard();
    else pasteClipboard();
    return;
  }
  event.preventDefault();
  const input = { type: "keydown", ...normalizeRemoteKey(event) };
  if (inputState.accept(input)) inputClient.send(input);
});

window.addEventListener("keyup", (event) => {
  const input = { type: "keyup", ...normalizeRemoteKey(event) };
  if (!inputState.accept(input)) return;
  event.preventDefault();
  inputClient.send(input);
});

mobileKeyboard.addEventListener("input", () => {
  const input = keyboardInput(mobileKeyboard.value);
  mobileKeyboard.value = "";
  if (input) inputClient.send(input);
});

mobileKeyboard.addEventListener("keydown", (event) => {
  if (event.code !== "Backspace") return;
  event.preventDefault();
  const input = { type: "keydown", key: event.key, code: event.code };
  if (inputState.accept(input)) inputClient.send(input);
  const release = { type: "keyup", key: event.key, code: event.code };
  if (inputState.accept(release)) inputClient.send(release);
});

peer.addTransceiver("video", { direction: "recvonly" });
peer.addTransceiver("audio", { direction: "recvonly" });
peer.ontrack = (event) => mediaStream.addTrack(event.track);

peer.onconnectionstatechange = () => {
  switch (peer.connectionState) {
    case "connected":
      setStatus("Live");
      break;
    case "failed":
    case "disconnected":
    case "closed":
      setStatus(`Connection ${peer.connectionState}`, true);
      break;
    default:
      setStatus("Connecting…");
  }
};

sound.addEventListener("click", async () => {
  video.muted = !video.muted;
  sound.textContent = video.muted ? "Enable sound" : "Mute";
  try {
    await video.play();
  } catch (error) {
    setStatus(`Audio playback failed: ${error.message}`, true);
  }
});

paste.addEventListener("click", pasteClipboard);
copy.addEventListener("click", copyClipboard);
keyboard.addEventListener("click", () => mobileKeyboard.focus({ preventScroll: true }));

quality.addEventListener("change", async () => {
  quality.disabled = true;
  setStatus(`Switching all viewers to ${quality.value}…`);
  try {
    const response = await fetch("/quality", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ profile: quality.value }),
    });
    if (!response.ok) throw new Error(await response.text());
    await syncRuntimeProfile();
  } catch (error) {
    setStatus(`Quality change failed: ${error.message}`, true);
  } finally {
    quality.disabled = false;
  }
});

const playbackStarter = createPlaybackStarter({
  video,
  connect,
  onError: (error) => {
    video.muted = true;
    sound.textContent = "Enable sound";
    setStatus(`Unable to start stream: ${error.message}`, true);
  },
});

startButton.addEventListener("click", () => {
  startButton.disabled = true;
  startButton.textContent = "Starting…";
  setStatus("Connecting…");
  playbackStarter.start().then(() => {
    startButton.textContent = "Streaming";
    sound.disabled = false;
  }).catch(() => {});
});

form.addEventListener("submit", async (event) => {
  event.preventDefault();
  openButton.disabled = true;
  setStatus("Opening browser page…");
  try {
    const response = await fetch("/browser-url", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ url: browserURL.value.trim() }),
    });
    if (!response.ok) throw new Error(await response.text());
    browserURL.value = (await response.json()).url;
    setStatus("Browser page updated");
  } catch (error) {
    setStatus(`Unable to open URL: ${error.message}`, true);
  } finally {
    openButton.disabled = false;
  }
});

fullscreen.addEventListener("click", async () => {
  try {
    if (document.fullscreenElement) await document.exitFullscreen();
    else await video.requestFullscreen();
  } catch (error) {
    setStatus(`Fullscreen failed: ${error.message}`, true);
  }
});

inputClient.connect();
syncRuntimeProfile().catch(() => {});
setInterval(() => updateStats().catch(() => {}), 1000);
setInterval(() => syncRuntimeProfile().catch(() => {}), 5000);
