import { Worker } from "node:worker_threads";
import { PERF } from "./hardware-profile.js";

/**
 * worker_threads-backed pool for CPU-bound work.
 *
 * Unlike TaskPool (promise-based semaphore on the main thread), ComputePool
 * offloads work to real OS threads so the event loop stays responsive while
 * heavy tasks (image resize, hashing, PDF render) run in parallel across cores.
 *
 * Workers execute an arbitrary self-contained function. The function source and
 * argument are serialized to the worker via postMessage and the result is
 * posted back.
 *
 * Pool size defaults to `PERF.computePoolSize` (half of CPU cores, capped at 10).
 */

type QueuedTask = {
  fnSource: string;
  arg: unknown;
  resolve: (value: unknown) => void;
  reject: (reason: unknown) => void;
};

type WorkerEntry = {
  worker: Worker;
  busy: boolean;
};

const WORKER_BOOTSTRAP = `
const { parentPort } = require("node:worker_threads");
parentPort.on("message", async (msg) => {
  try {
    const fn = new Function("return (" + msg.fnSource + ")")();
    const result = await fn(msg.arg);
    parentPort.postMessage({ ok: true, result });
  } catch (err) {
    parentPort.postMessage({
      ok: false,
      error: err instanceof Error ? err.message : String(err),
    });
  }
});
`;

export class ComputePool {
  private workers: WorkerEntry[] = [];
  private queue: QueuedTask[] = [];
  private _totalCompleted = 0;
  private _totalFailed = 0;
  private readonly _size: number;
  private terminated = false;

  constructor(size?: number) {
    this._size = size ?? PERF.computePoolSize;
    for (let i = 0; i < this._size; i++) {
      this.workers.push(this.spawnWorker());
    }
  }

  private spawnWorker(): WorkerEntry {
    const worker = new Worker(WORKER_BOOTSTRAP, { eval: true });
    return { worker, busy: false };
  }

  /**
   * Run a pure function in a worker thread.
   *
   * Constraints:
   * - `fn` must be self-contained (no closures over outer scope).
   * - `arg` must be structuredClone-safe (no functions; use Uint8Array
   *   instead of Buffer for binary data).
   * - Return value must also be structuredClone-safe.
   */
  run<A, R>(fn: (arg: A) => R | Promise<R>, arg: A): Promise<R> {
    if (this.terminated) {
      return Promise.reject(new Error("ComputePool is terminated"));
    }

    const fnSource = fn.toString();

    return new Promise<R>((resolve, reject) => {
      const task: QueuedTask = {
        fnSource,
        arg,
        resolve: resolve as (v: unknown) => void,
        reject,
      };

      const idle = this.workers.find((w) => !w.busy);
      if (idle) {
        this.dispatch(idle, task);
      } else {
        this.queue.push(task);
      }
    });
  }

  /**
   * Run a batch of items through the pool in parallel.
   * Results are returned in the same order as inputs.
   */
  map<A, R>(items: A[], fn: (arg: A) => R | Promise<R>): Promise<R[]> {
    if (items.length === 0) {
      return Promise.resolve([]);
    }
    return Promise.all(items.map((item) => this.run(fn, item)));
  }

  get poolSize(): number {
    return this._size;
  }

  get activeCount(): number {
    return this.workers.filter((w) => w.busy).length;
  }

  get queuedCount(): number {
    return this.queue.length;
  }

  get totalCompleted(): number {
    return this._totalCompleted;
  }

  get totalFailed(): number {
    return this._totalFailed;
  }

  /** Terminate all workers. The pool cannot be used after this. */
  async terminate(): Promise<void> {
    this.terminated = true;
    for (const task of this.queue) {
      task.reject(new Error("ComputePool terminated"));
    }
    this.queue.length = 0;
    await Promise.all(this.workers.map((w) => w.worker.terminate()));
    this.workers.length = 0;
  }

  private dispatch(entry: WorkerEntry, task: QueuedTask): void {
    entry.busy = true;

    const cleanup = () => {
      entry.worker.off("message", onMessage);
      entry.worker.off("error", onError);
      entry.busy = false;
    };

    const onMessage = (msg: { ok: boolean; result?: unknown; error?: string }) => {
      cleanup();
      if (msg.ok) {
        this._totalCompleted++;
        task.resolve(msg.result);
      } else {
        this._totalFailed++;
        task.reject(new Error(msg.error ?? "Worker task failed"));
      }
      this.drainQueue();
    };

    const onError = (err: Error) => {
      cleanup();
      this._totalFailed++;
      task.reject(err);

      // Replace the broken worker.
      const idx = this.workers.indexOf(entry);
      if (idx >= 0 && !this.terminated) {
        this.workers[idx] = this.spawnWorker();
      }
      this.drainQueue();
    };

    entry.worker.on("message", onMessage);
    entry.worker.on("error", onError);
    entry.worker.postMessage({ fnSource: task.fnSource, arg: task.arg });
  }

  private drainQueue(): void {
    while (this.queue.length > 0) {
      const idle = this.workers.find((w) => !w.busy);
      if (!idle) {
        break;
      }
      const next = this.queue.shift()!;
      this.dispatch(idle, next);
    }
  }
}

// Shared singleton pool for general CPU-bound work.
let sharedPool: ComputePool | null = null;

/** Get the shared compute pool (created on first call). */
export function getComputePool(): ComputePool {
  if (!sharedPool) {
    sharedPool = new ComputePool();
  }
  return sharedPool;
}
