import assert from "node:assert/strict";
import test from "node:test";
import { observeNearViewportOnce } from "./observe-near-viewport.ts";

function observers(t) {
  const previous = globalThis.IntersectionObserver;
  const instances = [];
  globalThis.IntersectionObserver = class {
    targets = new Set();
    disconnected = false;
    constructor(callback, options) { this.callback = callback; this.options = options; instances.push(this); }
    observe(element) { this.targets.add(element); }
    unobserve(element) { this.targets.delete(element); }
    disconnect() { this.disconnected = true; this.targets.clear(); }
    enter(element, isIntersecting = true) { this.callback([{ target: element, isIntersecting }]); }
  };
  t.after(() => { globalThis.IntersectionObserver = previous; });
  return instances;
}
const element = (root) => ({ closest: () => root });

test("5000 loaded messages share one viewport observer and release on unmount", (t) => {
  const instances = observers(t);
  const root = {};
  const stops = Array.from({ length: 5000 }, () => observeNearViewportOnce(element(root), "400px", assert.fail));
  assert.equal(instances.length, 1);
  assert.equal(instances[0].targets.size, 5000);
  for (const stop of stops) stop();
  assert.equal(instances[0].targets.size, 0);
  assert.equal(instances[0].disconnected, true);
});

test("only intersection triggers once; canceled and late entries do not load bodies", (t) => {
  const instances = observers(t);
  const root = {};
  const first = element(root), second = element(root);
  let calls = 0;
  const stopFirst = observeNearViewportOnce(first, "400px", () => calls++);
  const stopSecond = observeNearViewportOnce(second, "400px", assert.fail);
  const observer = instances[0];
  observer.enter(first, false);
  assert.equal(calls, 0);
  stopSecond(); observer.enter(second);
  observer.enter(first); observer.enter(first);
  assert.equal(calls, 1);
  assert.equal(observer.disconnected, true);
  stopFirst(); stopFirst(); stopSecond();
  const stopNew = observeNearViewportOnce(first, "400px", () => calls++);
  assert.equal(instances.length, 2);
  stopFirst(); // an old cleanup cannot remove a new subscription
  instances[1].enter(first);
  assert.equal(calls, 2);
  stopNew();
});

test("different viewports and margins stay separate and duplicate subscribers release individually", (t) => {
  const instances = observers(t);
  const firstRoot = {}, secondRoot = {};
  const first = element(firstRoot);
  let calls = 0;
  const stop1 = observeNearViewportOnce(first, "400px", () => calls++);
  const stop2 = observeNearViewportOnce(first, "400px", () => calls++);
  const stop3 = observeNearViewportOnce(element(secondRoot), "400px", assert.fail);
  const stop4 = observeNearViewportOnce(element(firstRoot), "100px", assert.fail);
  const stop5 = observeNearViewportOnce(element(null), "400px", assert.fail);
  assert.equal(instances.length, 4);
  assert.equal(instances[0].targets.size, 1);
  stop1(); assert.equal(instances[0].targets.size, 1);
  instances[0].enter(first);
  assert.equal(calls, 1);
  stop2(); stop3(); stop4(); stop5();
  assert.ok(instances.every((observer) => observer.disconnected));
});
