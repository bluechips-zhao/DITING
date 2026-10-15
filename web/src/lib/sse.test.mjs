import assert from "node:assert/strict";
import test from "node:test";
import { SSEDecoder, AuthenticatedEventStream } from "./sse.ts";

test("split CRLF frames retain named events, multiline data, IDs and retry", () => {
  const frames = [];
  const decoder = new SSEDecoder(frame => frames.push(frame));
  for (const char of ": ping\r\nevent: snapshot\r\nid: 42\r\nretry: 500\r\ndata: 中文\r\ndata: next\r\n\r\ndata: final\n\n") decoder.feed(char);
  assert.deepEqual(frames, [
    { type: "snapshot", data: "中文\nnext", lastEventId: "42" },
    { type: "message", data: "final", lastEventId: "42" },
  ]);
  assert.equal(decoder.retry, 500);
});

test("stream reconnects with header credentials and cursor, never URL tokens", { timeout: 5000 }, async () => {
  const calls = [];
  let stream;
  const done = new Promise(resolve => {
    stream = new AuthenticatedEventStream("http://localhost/api/events", {
      token: () => calls.length ? "new-session" : "first-session",
      unauthorized: () => assert.fail("unexpected unauthorized"),
      fetch: async (url, options) => {
        calls.push({ url, options });
        return new Response(calls.length === 1 ? "id: 7\nretry: 500\ndata: first\n\n" : "data: resumed\n\n", { headers: { "Content-Type": "text/event-stream" } });
      },
    });
    stream.onmessage = event => {
      if (event.data === "resumed") { stream.close(); resolve(); }
    };
  });
  await done;
  assert.equal(calls.length, 2);
  assert.equal(calls[0].url, "http://localhost/api/events");
  assert.equal(calls[0].options.headers.Authorization, "Bearer first-session");
  assert.equal(calls[1].options.headers.Authorization, "Bearer new-session");
  assert.equal(calls[1].options.headers["Last-Event-ID"], "7");
  assert.equal(stream.readyState, 2);
});

test("401 stops reconnecting and invokes session expiry handling", { timeout: 5000 }, async () => {
  let count = 0;
  let stream;
  await new Promise(resolve => {
    stream = new AuthenticatedEventStream("/api/events", {
      token: () => "expired",
      unauthorized: resolve,
      fetch: async () => { count++; return new Response(null, { status: 401 }); },
    });
  });
  assert.equal(stream.readyState, 2);
  assert.equal(count, 1);
});

test("UTF-8 byte boundaries survive streaming and explicit close", { timeout: 5000 }, async () => {
  const encoded = new TextEncoder().encode("event: snapshot\ndata: 中文证据\n\n");
  let stream;
  const delivered = new Promise(resolve => {
    stream = new AuthenticatedEventStream("/api/events", {
      token: () => "test-session",
      unauthorized: () => assert.fail("unexpected unauthorized"),
      fetch: async () => new Response(new ReadableStream({
        start(controller) {
          for (const byte of encoded) controller.enqueue(new Uint8Array([byte]));
          controller.close();
        },
      }), { headers: { "Content-Type": "text/event-stream" } }),
    });
    stream.addEventListener("snapshot", event => { stream.close(); resolve(event.data); });
  });
  assert.equal(await delivered, "中文证据");
  assert.equal(stream.readyState, 2);
});
