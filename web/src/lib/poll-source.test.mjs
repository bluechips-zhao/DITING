import assert from "node:assert/strict";
import test from "node:test";
import { pollSource } from "./poll-source.ts";

function harness(visible = true) {
  const listeners = new Set();
  const timers = new Set();
  return {
    environment: {
      isVisible: () => visible,
      subscribe: (callback) => { listeners.add(callback); return () => listeners.delete(callback); },
      schedule: (callback) => { timers.add(callback); return () => timers.delete(callback); },
    },
    visibility(value) { visible = value; for (const callback of listeners) callback(); },
    tick() { const pending = [...timers]; timers.clear(); for (const callback of pending) callback(); },
    get timers() { return timers.size; },
    get listeners() { return listeners.size; },
  };
}
const flush = () => new Promise((resolve) => setImmediate(resolve));
const deferred = () => {
  let resolve;
  const promise = new Promise((done) => { resolve = done; });
  return { promise, resolve };
};

test("hidden pages make no new requests and resume immediately when visible", async () => {
  const h = harness(false);
  let requests = 0;
  const values = [];
  const stop = pollSource({ interval: 5000, request: async () => ++requests, receive: (v) => values.push(v), fail: assert.fail }, h.environment);
  assert.equal(requests, 0);
  h.visibility(true);
  await flush();
  assert.deepEqual(values, [1]);
  assert.equal(h.timers, 1);
  h.visibility(false);
  h.tick();
  await flush();
  assert.equal(requests, 1);
  h.visibility(true);
  await flush();
  assert.deepEqual(values, [1, 2]);
  stop();
  assert.equal(h.timers, 0);
  assert.equal(h.listeners, 0);
});

test("slow requests never overlap, including repeated hide and resume", async () => {
  const h = harness();
  const pending = deferred();
  let requests = 0;
  const stop = pollSource({ interval: 5, request: () => { requests++; return pending.promise; }, receive() {}, fail: assert.fail }, h.environment);
  for (let i = 0; i < 5; i++) { h.visibility(false); h.visibility(true); h.tick(); }
  assert.equal(requests, 1);
  assert.equal(h.timers, 0);
  pending.resolve("done");
  await flush();
  assert.equal(h.timers, 1);
  h.tick();
  assert.equal(requests, 2);
  stop();
  await flush();
  assert.equal(h.timers, 0);
});

test("one failing or stalled source cannot block a healthy source and failure retries", async () => {
  const h = harness();
  const stalled = deferred();
  let attempts = 0;
  const errors = [];
  const recovered = [];
  const healthy = [];
  const stopSlow = pollSource({ interval: 5, request: () => stalled.promise, receive() {}, fail: assert.fail }, h.environment);
  const stopFail = pollSource({ interval: 5, request: async () => { if (++attempts === 1) throw new Error("offline"); return "recovered"; }, receive: (v) => recovered.push(v), fail: (e) => errors.push(e.message) }, h.environment);
  const stopGood = pollSource({ interval: 5, request: async () => "good", receive: (v) => healthy.push(v), fail: assert.fail }, h.environment);
  await flush();
  assert.deepEqual(errors, ["offline"]);
  assert.deepEqual(healthy, ["good"]);
  h.tick();
  await flush();
  assert.deepEqual(recovered, ["recovered"]);
  assert.deepEqual(healthy, ["good", "good"]);
  stopSlow(); stopFail(); stopGood(); stalled.resolve();
  await flush();
  assert.equal(h.timers, 0);
});

test("disposing suppresses late success and failure and removes visibility listeners", async () => {
  const h = harness();
  const pending = deferred();
  const values = [];
  const stop = pollSource({ interval: 5, request: () => pending.promise, receive: (v) => values.push(v), fail: (e) => values.push(e) }, h.environment);
  stop();
  h.visibility(false); h.visibility(true);
  pending.resolve("late");
  await flush();
  assert.deepEqual(values, []);
  assert.equal(h.timers, 0);
  assert.equal(h.listeners, 0);
  const stopError = pollSource({ interval: 5, request: async () => { throw new Error("late failure"); }, receive: assert.fail, fail: assert.fail }, h.environment);
  stopError();
  await flush();
  assert.equal(h.timers, 0);
});
