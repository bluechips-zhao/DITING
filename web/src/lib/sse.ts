export interface StreamFrame { type: string; data: string; lastEventId: string }

// Handles split UTF-8 chunks (TextDecoder), CR/LF framing and named events.
export class SSEDecoder {
  private line = "";
  private skipLF = false;
  private data: string[] = [];
  private type = "";
  lastEventId = "";
  retry = 2000;
  private emit: (frame: StreamFrame) => void;
  constructor(emit: (frame: StreamFrame) => void) { this.emit = emit; }

  feed(text: string) {
    for (const char of text) {
      if (this.skipLF) { this.skipLF = false; if (char === "\n") continue; }
      if (char === "\r" || char === "\n") {
        this.consume(this.line);
        this.line = "";
        this.skipLF = char === "\r";
      } else this.line += char;
    }
  }

  private consume(line: string) {
    if (!line) {
      if (this.data.length) this.emit({ type: this.type || "message", data: this.data.join("\n"), lastEventId: this.lastEventId });
      this.data = [];
      this.type = "";
      return;
    }
    if (line.startsWith(":")) return;
    const index = line.indexOf(":");
    const field = index < 0 ? line : line.slice(0, index);
    let value = index < 0 ? "" : line.slice(index + 1);
    if (value.startsWith(" ")) value = value.slice(1);
    if (field === "data") this.data.push(value);
    else if (field === "event") this.type = value;
    else if (field === "id" && !value.includes("\0")) this.lastEventId = value;
    else if (field === "retry" && /^\d+$/.test(value)) this.retry = Math.min(30000, Math.max(500, Number(value)));
  }
}

export interface StreamOptions {
  token: () => string | null;
  unauthorized: () => void;
  fetch?: typeof fetch;
}

export class AuthenticatedEventStream extends EventTarget {
  onopen: ((event: Event) => void) | null = null;
  onerror: ((event: Event) => void) | null = null;
  onmessage: ((event: MessageEvent) => void) | null = null;
  readyState = 0;
  private closed = false;
  private controller: AbortController | null = null;
  private timer: ReturnType<typeof setTimeout> | null = null;
  private lastEventId = "";
  private retry = 2000;

  private url: string;
  private options: StreamOptions;
  constructor(url: string, options: StreamOptions) {
    super();
    this.url = url;
    this.options = options;
    queueMicrotask(() => void this.connect());
  }

  close() {
    this.closed = true;
    this.readyState = 2;
    this.controller?.abort();
    if (this.timer) clearTimeout(this.timer);
  }

  private async connect() {
    if (this.closed) return;
    this.readyState = 0;
    const controller = new AbortController();
    this.controller = controller;
    try {
      const token = this.options.token();
      const headers: Record<string, string> = { Accept: "text/event-stream" };
      if (token) headers.Authorization = `Bearer ${token}`;
      if (this.lastEventId) headers["Last-Event-ID"] = this.lastEventId;
      const response = await (this.options.fetch ?? fetch)(this.url, {
        headers, signal: controller.signal, cache: "no-store", credentials: "omit",
      });
      if (this.closed) { await response.body?.cancel(); return; }
      if (response.status === 401 || response.status === 403) {
        this.close();
        this.options.unauthorized();
        this.dispatchError();
        return;
      }
      if (response.status === 204) { this.close(); return; }
      if (!response.ok || !response.body || !response.headers.get("Content-Type")?.includes("text/event-stream")) {
        throw new Error("Event stream unavailable");
      }
      this.readyState = 1;
      const opened = new Event("open");
      this.onopen?.(opened);
      this.dispatchEvent(opened);
      const reader = response.body.getReader();
      const decoder = new TextDecoder();
      const parser = new SSEDecoder((frame) => {
        if (this.closed) return;
        const event = new MessageEvent(frame.type, { data: frame.data, lastEventId: frame.lastEventId });
        if (frame.type === "message") this.onmessage?.(event);
        this.dispatchEvent(event);
      });
      parser.lastEventId = this.lastEventId;
      parser.retry = this.retry;
      try {
        while (!this.closed) {
          const chunk = await reader.read();
          if (chunk.done) break;
          parser.feed(decoder.decode(chunk.value, { stream: true }));
          this.lastEventId = parser.lastEventId;
          this.retry = parser.retry;
        }
      } finally { reader.releaseLock(); }
    } catch {
      // Network errors reconnect; explicit close never reconnects.
    } finally {
      controller.abort();
    }
    if (!this.closed) {
      this.readyState = 0;
      this.dispatchError();
      if (!this.closed) this.timer = setTimeout(() => void this.connect(), this.retry);
    }
  }

  private dispatchError() {
    const event = new Event("error");
    this.onerror?.(event);
    this.dispatchEvent(event);
  }
}
