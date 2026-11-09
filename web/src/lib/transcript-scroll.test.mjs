import assert from "node:assert/strict";
import test from "node:test";
import { bindTranscriptScroll } from "./transcript-scroll.ts";

class Viewport extends EventTarget {
  scrollTop = 500;
  clientHeight = 500;
  scrollHeight = 1000;
  fire(type, properties = {}) { this.dispatchEvent(Object.assign(new Event(type), properties)); }
}

test("async growth preserves bottom intent until the reader actually moves up", () => {
  const vp = new Viewport();
  const pinned = { current: true };
  let events = 0;
  const stop = bindTranscriptScroll(vp, pinned, () => events++);
  vp.scrollHeight = 1600;
  vp.fire("scroll");
  assert.equal(pinned.current, true);
  vp.scrollTop = 1100; vp.fire("scroll");
  assert.equal(pinned.current, true);
  vp.fire("wheel", { deltaY: -100 });
  vp.scrollTop = 1000; vp.fire("scroll");
  assert.equal(pinned.current, false);
  vp.scrollHeight = 2000; vp.fire("scroll");
  assert.equal(pinned.current, false);
  stop();
  const before = events;
  vp.fire("scroll"); vp.fire("keydown", { key: "End" });
  assert.equal(events, before);
  assert.equal(pinned.current, false);
});

test("End remains pinned when lazy layout grows before the scroll event", () => {
  const vp = new Viewport();
  vp.scrollTop = 0;
  const pinned = { current: false };
  const stop = bindTranscriptScroll(vp, pinned, () => {});
  vp.fire("keydown", { key: "End", ctrlKey: true });
  vp.scrollTop = 500; vp.scrollHeight = 2400; vp.fire("scroll");
  assert.equal(pinned.current, true);
  vp.scrollTop = 1900; vp.fire("scroll");
  assert.equal(pinned.current, true);
  vp.fire("keydown", { key: "Home", ctrlKey: true });
  vp.scrollTop = 0; vp.fire("scroll");
  assert.equal(pinned.current, false);
  stop();
});

test("wheel, page down and scrollbar movement retain the reader's direction", () => {
  const vp = new Viewport();
  vp.scrollTop = 300;
  const pinned = { current: false };
  const stop = bindTranscriptScroll(vp, pinned, () => {});
  vp.fire("wheel", { deltaY: 200 });
  assert.equal(pinned.current, true);
  vp.scrollTop = 450; vp.fire("scroll");
  vp.scrollTop = 200; vp.fire("scroll"); // scrollbar dragged upward
  assert.equal(pinned.current, false);
  vp.fire("keydown", { key: "PageDown" });
  assert.equal(pinned.current, true);
  vp.fire("keydown", { key: " ", shiftKey: true });
  assert.equal(pinned.current, false);
  stop();
});
