import test from "node:test";
import assert from "node:assert/strict";
import { createPlaybackStarter } from "./playback.mjs";

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
