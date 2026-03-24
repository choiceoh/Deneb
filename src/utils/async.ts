export function sleep(ms: number) {
  return new Promise((resolve) => setTimeout(resolve, ms));
}

/**
 * Poll until the condition returns a truthy value.
 */
export async function waitFor<T>(
  fn: () => T | Promise<T>,
  options: {
    timeoutMs?: number;
    intervalMs?: number;
    signal?: AbortSignal;
  } = {},
): Promise<T> {
  const { timeoutMs = 30_000, intervalMs = 500, signal } = options;
  const start = Date.now();
  while (true) {
    if (signal?.aborted) {
      throw new DOMException("Aborted", "AbortError");
    }
    const result = await fn();
    if (result) {
      return result;
    }
    if (Date.now() - start > timeoutMs) {
      throw new Error(`waitFor timed out after ${timeoutMs}ms`);
    }
    await sleep(intervalMs);
  }
}

/**
 * Wait for a single event from an EventEmitter.
 */
export function waitForEvent<T = unknown>(
  emitter: {
    // oxlint-disable-next-line typescript/no-explicit-any -- Node EventEmitter compat requires any[]
    once: (event: string, fn: (...args: any[]) => void) => void;
    // oxlint-disable-next-line typescript/no-explicit-any -- Node EventEmitter compat requires any[]
    off: (event: string, fn: (...args: any[]) => void) => void;
  },
  event: string,
  options: {
    timeoutMs?: number;
    signal?: AbortSignal;
  } = {},
): Promise<T> {
  const { timeoutMs = 30_000, signal } = options;
  return new Promise((resolve, reject) => {
    const timer = setTimeout(() => {
      cleanup();
      reject(new Error(`waitForEvent("${event}") timed out after ${timeoutMs}ms`));
    }, timeoutMs);
    const onEvent = (val: T) => {
      cleanup();
      resolve(val);
    };
    const onAbort = () => {
      cleanup();
      reject(new DOMException("Aborted", "AbortError"));
    };
    const cleanup = () => {
      clearTimeout(timer);
      emitter.off(event, onEvent);
      signal?.removeEventListener("abort", onAbort);
    };
    emitter.once(event, onEvent);
    signal?.addEventListener("abort", onAbort, { once: true });
  });
}
