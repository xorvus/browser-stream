import test from "node:test";
import assert from "node:assert/strict";
import { clearTracks, createPlaybackStarter } from "./playback.mjs";

test("clearTracks empties the stream in place instead of replacing it", () => {
  const stopped = [];
  const tracks = [
    { kind: "video", stop: () => stopped.push("video") },
    { kind: "audio", stop: () => stopped.push("audio") },
  ];
  const stream = {
    getTracks: () => [...tracks],
    removeTrack: (track) => tracks.splice(tracks.indexOf(track), 1),
  };

  clearTracks(stream);

  assert.deepEqual(tracks, [], "every track must be removed from the same stream object");
  assert.deepEqual(stopped, ["video", "audio"], "removed tracks must be stopped");
});

test("clearTracks is safe on an already empty stream", () => {
  assert.doesNotThrow(() => clearTracks({ getTracks: () => [], removeTrack: () => {} }));
});

test("starts unmuted playback synchronously and negotiates only once", async () => {
  const calls = [];
  const video = {
    muted: true,
    play() {
      calls.push("play");
      return Promise.resolve();
    },
  };
  let connects = 0;
  const starter = createPlaybackStarter({
    video,
    connect() {
      connects += 1;
      calls.push("connect");
      return Promise.resolve();
    },
  });

  const first = starter.start();
  const second = starter.start();

  assert.equal(video.muted, false);
  assert.deepEqual(calls, ["play", "connect"]);
  assert.equal(connects, 1);
  assert.equal(first, second);
  await first;
});

test("reports a playback rejection from the shared start promise", async () => {
  const failure = new Error("sound blocked");
  let reported;
  const starter = createPlaybackStarter({
    video: {
      muted: true,
      play: () => Promise.reject(failure),
    },
    connect: () => Promise.resolve(),
    onError: (error) => {
      reported = error;
    },
  });

  await assert.rejects(starter.start(), failure);
  assert.equal(reported, failure);
});

test("retries playback without renegotiating an established connection", async () => {
  let plays = 0;
  let connects = 0;
  const starter = createPlaybackStarter({
    video: {
      muted: true,
      play() {
        plays += 1;
        return plays === 1 ? Promise.reject(new Error("blocked")) : Promise.resolve();
      },
    },
    connect() {
      connects += 1;
      return Promise.resolve();
    },
  });

  await assert.rejects(starter.start(), /blocked/);
  await starter.start();

  assert.equal(plays, 2);
  assert.equal(connects, 1);
});
