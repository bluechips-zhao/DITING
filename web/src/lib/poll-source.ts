export interface PollEnvironment {
  isVisible(): boolean;
  subscribe(listener: () => void): () => void;
  schedule(callback: () => void, delay: number): () => void;
}

const browserEnvironment: PollEnvironment = {
  isVisible: () => document.visibilityState !== "hidden",
  subscribe: (listener) => {
    document.addEventListener("visibilitychange", listener);
    return () => document.removeEventListener("visibilitychange", listener);
  },
  schedule: (callback, delay) => {
    const timer = setTimeout(callback, delay);
    return () => clearTimeout(timer);
  },
};

// Each source owns its timer: a slow or failed request cannot stall its peers.
// Requests already in flight may finish while hidden; no new request starts.
export function pollSource<T>(
  options: {
    request(): Promise<T>;
    receive(value: T): void;
    fail(error: unknown): void;
    interval: number;
  },
  environment: PollEnvironment = browserEnvironment,
): () => void {
  let alive = true;
  let loading = false;
  let cancelTimer: (() => void) | undefined;

  const clearTimer = () => {
    cancelTimer?.();
    cancelTimer = undefined;
  };
  const load = async () => {
    if (!alive || loading || !environment.isVisible()) return;
    clearTimer();
    loading = true;
    try {
      const value = await options.request();
      if (alive) options.receive(value);
    } catch (error) {
      if (alive) options.fail(error);
    } finally {
      loading = false;
      if (alive && environment.isVisible()) {
        cancelTimer = environment.schedule(() => void load(), options.interval);
      }
    }
  };
  const unsubscribe = environment.subscribe(() => {
    clearTimer();
    if (environment.isVisible()) void load();
  });
  void load();

  return () => {
    alive = false;
    clearTimer();
    unsubscribe();
  };
}
