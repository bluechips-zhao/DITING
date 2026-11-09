type Pool = {
  observer: IntersectionObserver;
  targets: Map<Element, Set<() => void>>;
};

const rootedPools = new WeakMap<Element, Map<string, Pool>>();
const windowPools = new Map<string, Pool>();

// Share one observer per scroll viewport/margin instead of creating one for
// every loaded message. Each subscription latches once and releases its target.
export function observeNearViewportOnce(element: Element, rootMargin: string, enter: () => void): () => void {
  const root = element.closest('[data-slot="scroll-area-viewport"]');
  let pools = root ? rootedPools.get(root) : windowPools;
  if (!pools) {
    pools = new Map();
    rootedPools.set(root!, pools);
  }
  const owner = pools;
  let pool = owner.get(rootMargin);
  const releaseEmpty = (value: Pool) => {
    if (value.targets.size === 0 && owner.get(rootMargin) === value) {
      value.observer.disconnect();
      owner.delete(rootMargin);
    }
  };
  if (!pool) {
    const targets = new Map<Element, Set<() => void>>();
    const observer = new IntersectionObserver((entries) => {
      for (const entry of entries) {
        if (!entry.isIntersecting) continue;
        const callbacks = targets.get(entry.target);
        if (!callbacks) continue;
        targets.delete(entry.target);
        observer.unobserve(entry.target);
        releaseEmpty(value);
        for (const callback of callbacks) callback();
      }
    }, { root, rootMargin });
    const value: Pool = { observer, targets };
    pool = value;
    owner.set(rootMargin, pool);
  }
  const current = pool;
  let callbacks = current.targets.get(element);
  if (!callbacks) {
    callbacks = new Set();
    current.targets.set(element, callbacks);
    current.observer.observe(element);
  }
  callbacks.add(enter);
  return () => {
    const listeners = current.targets.get(element);
    if (!listeners) return;
    listeners.delete(enter);
    if (listeners.size === 0) {
      current.targets.delete(element);
      current.observer.unobserve(element);
      releaseEmpty(current);
    }
  };
}
