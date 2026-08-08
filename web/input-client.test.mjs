import test from "node:test";
import assert from "node:assert/strict";

import { createInputClient } from "./input-client.mjs";

class FakeSocket {
  static instances = [];

  constructor(url) {
    this.url = url;
    this.readyState = 0;
    this.sent = [];
    FakeSocket.instances.push(this);
  }

  open() {
    this.readyState = 1;
    this.onopen?.();
  }

  send(message) {
    this.sent.push(JSON.parse(message));
  }

  receive(message) {
    this.onmessage?.({ data: JSON.stringify(message) });
  }

  disconnect() {
    this.readyState = 3;
    this.onclose?.();
  }

  close() {
    this.disconnect();
  }
}

function createScheduler() {
  const jobs = [];
  return {
    jobs,
    schedule(callback, delay) {
      const job = { callback, delay, cancelled: false };
      jobs.push(job);
      return job;
    },
    cancel(job) {
      if (job) job.cancelled = true;
    },
    runNext() {
      const job = jobs.find((candidate) => !candidate.cancelled);
      assert.ok(job, "expected a scheduled job");
      job.cancelled = true;
      job.callback();
      return job.delay;
    },
  };
}

function setup(options = {}) {
  FakeSocket.instances = [];
  const scheduler = createScheduler();
  const client = createInputClient({
    url: "ws://example.test/input/ws",
    WebSocketImpl: FakeSocket,
    schedule: scheduler.schedule,
    cancel: scheduler.cancel,
    ...options,
  });
  client.connect();
  return { client, scheduler, socket: FakeSocket.instances[0] };
}

test("sends urgent input once in monotonically increasing sequence", () => {
  const { client, socket } = setup();
  socket.open();

  assert.equal(client.send({ type: "keydown", key: "a", code: "KeyA" }), true);
  assert.equal(client.send({ type: "keyup", key: "a", code: "KeyA" }), true);

  assert.deepEqual(socket.sent.map(({ seq, type }) => ({ seq, type })), [
    { seq: 1, type: "keydown" },
    { seq: 2, type: "keyup" },
  ]);
});

test("coalesces pointer movement to the latest position", () => {
  const { client, scheduler, socket } = setup();
  socket.open();

  client.send({ type: "move", x: 10, y: 20 });
  client.send({ type: "move", x: 30, y: 40 });
  assert.equal(socket.sent.length, 0);
  assert.equal(Math.round(scheduler.runNext()), 17);
  assert.deepEqual(socket.sent, [{ v: 1, seq: 1, type: "move", x: 30, y: 40 }]);
});

test("uses the animation scheduler for pointer movement", () => {
  const scheduler = createScheduler();
  let animationFrames = 0;
  const { client, socket } = setup({
    scheduleMove(callback) {
      animationFrames += 1;
      return scheduler.schedule(callback, 16);
    },
    cancelMove: scheduler.cancel,
  });
  socket.open();

  client.send({ type: "move", x: 10, y: 20 });

  assert.equal(animationFrames, 1);
});

test("does not replay input produced while disconnected", () => {
  const { client, socket } = setup();
  assert.equal(client.send({ type: "move", x: 10, y: 20 }), false);
  assert.equal(client.send({ type: "keydown", key: "a", code: "KeyA" }), false);

  socket.open();
  assert.deepEqual(socket.sent, []);
});

test("reports execution errors from acknowledgements", () => {
  const errors = [];
  const { client, socket } = setup({ onError: (error) => errors.push(error.message) });
  socket.open();
  client.send({ type: "keydown", key: "a", code: "KeyA" });
  socket.receive({ v: 1, ack: 1, error: "execution timeout" });
  assert.deepEqual(errors, ["execution timeout"]);
});

test("reconnects with bounded backoff", () => {
  const { scheduler, socket } = setup();
  socket.open();
  socket.disconnect();
  const delay = scheduler.runNext();
  assert.equal(delay, 250);
  assert.equal(FakeSocket.instances.length, 2);
});
