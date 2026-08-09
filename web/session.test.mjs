import assert from "node:assert/strict";
import test from "node:test";

import { createStreamSession, offerURL, prefersDataSaver } from "./session.mjs";

class FakePeerConnection {
  static instances = [];

  constructor() {
    this.iceGatheringState = "complete";
    this.localDescription = { type: "offer", sdp: "fake" };
    this.remoteDescription = null;
    this.transceivers = [];
    this.connectionState = "new";
    this.closed = false;
    FakePeerConnection.instances.push(this);
  }

  addTransceiver(kind, options) {
    this.transceivers.push({ kind, ...options });
  }
  addEventListener() {}
  removeEventListener() {}
  async createOffer() {
    return { type: "offer", sdp: "fake" };
  }
  async setLocalDescription(description) {
    this.localDescription = description;
  }
  async setRemoteDescription(description) {
    this.remoteDescription = description;
  }
  close() {
    this.closed = true;
  }
}

function fakeFetch(calls, { ok = true, body = { type: "answer", sdp: "answer" } } = {}) {
  return async (url, options) => {
    calls.push({ url, options });
    return {
      ok,
      json: async () => body,
      text: async () => "rejected",
    };
  };
}

test("offerURL only carries a profile when it is not the full stream", () => {
  assert.equal(offerURL("full"), "/offer");
  assert.equal(offerURL(""), "/offer");
  assert.equal(offerURL("saver360"), "/offer?profile=saver360");
});

test("prefersDataSaver follows the browser data-saver and slow-network signals", () => {
  assert.equal(prefersDataSaver(undefined), false);
  assert.equal(prefersDataSaver({ saveData: true }), true);
  assert.equal(prefersDataSaver({ saveData: false, effectiveType: "4g" }), false);
  assert.equal(prefersDataSaver({ saveData: false, effectiveType: "2g" }), true);
  assert.equal(prefersDataSaver({ saveData: false, effectiveType: "3g" }), true);
});

test("connect signals the requested delivery profile to the server", async () => {
  FakePeerConnection.instances = [];
  const calls = [];
  const session = createStreamSession({ PeerConnection: FakePeerConnection, fetch: fakeFetch(calls) });

  await session.connect("saver360");

  assert.equal(calls.length, 1);
  assert.equal(calls[0].url, "/offer?profile=saver360");
  assert.equal(session.profile, "saver360");
  assert.deepEqual(
    FakePeerConnection.instances[0].transceivers.map((entry) => entry.kind),
    ["video", "audio"],
  );
});

test("switching profile replaces the peer connection instead of reusing it", async () => {
  FakePeerConnection.instances = [];
  const session = createStreamSession({ PeerConnection: FakePeerConnection, fetch: fakeFetch([]) });

  await session.connect("full");
  await session.connect("saver360");

  assert.equal(FakePeerConnection.instances.length, 2);
  assert.equal(FakePeerConnection.instances[0].closed, true, "the previous connection must be closed");
  assert.equal(FakePeerConnection.instances[1].closed, false);
  assert.equal(session.profile, "saver360");
});

test("a rejected offer surfaces the server message and leaves no stale state", async () => {
  FakePeerConnection.instances = [];
  const session = createStreamSession({
    PeerConnection: FakePeerConnection,
    fetch: fakeFetch([], { ok: false }),
  });

  await assert.rejects(() => session.connect("saver480"), /rejected/);
});

test("callbacks from a replaced connection are ignored", async () => {
  FakePeerConnection.instances = [];
  const states = [];
  const session = createStreamSession({
    PeerConnection: FakePeerConnection,
    fetch: fakeFetch([]),
    onState: (state) => states.push(state),
  });

  await session.connect("full");
  const stale = FakePeerConnection.instances[0];
  await session.connect("saver360");

  stale.connectionState = "failed";
  stale.onconnectionstatechange();
  assert.deepEqual(states, [], "a closed connection must not drive the viewer status");
});

test("ICE gathering that never completes rejects instead of hanging", async () => {
  FakePeerConnection.instances = [];
  const session = createStreamSession({
    PeerConnection: class extends FakePeerConnection {
      constructor() {
        super();
        this.iceGatheringState = "gathering";
      }
    },
    fetch: fakeFetch([]),
    schedule: (callback) => {
      callback();
      return 1;
    },
    cancel: () => {},
  });

  await assert.rejects(() => session.connect("full"), /ICE gathering timed out/);
});
