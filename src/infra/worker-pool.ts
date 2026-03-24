import { PERF } from "./hardware-profile.js";

export type TaskPoolStats = {
  active: number;
  waiting: number;
  concurrency: number;
  totalCompleted: number;
  totalFailed: number;
  /** Ratio of active tasks to max concurrency (0–1). */
  pressure: number;
};

/**
 * Promise-based concurrency limiter for CPU-bound work.
 * Sized based on hardware-detected performance profile.
 *
 * Tracks completion/failure counters and exposes a `pressure` metric
 * so callers can make load-adaptive decisions (e.g. back-off, shed).
 */
export class TaskPool {
  private active = 0;
  private waiting: Array<() => void> = [];
  private _maxConcurrent: number;
  private _totalCompleted = 0;
  private _totalFailed = 0;

  constructor(maxConcurrent?: number) {
    this._maxConcurrent = maxConcurrent ?? PERF.imageWorkerCount;
  }

  /** Run a single task with concurrency limiting. */
  async run<T>(task: () => Promise<T>): Promise<T> {
    await this.acquire();
    try {
      const result = await task();
      this._totalCompleted++;
      return result;
    } catch (err) {
      this._totalFailed++;
      throw err;
    } finally {
      this.release();
    }
  }

  /** Run tasks in parallel with pool-limited concurrency. */
  async map<T, R>(items: T[], fn: (item: T, index: number) => Promise<R>): Promise<R[]> {
    const results: R[] = Array.from({ length: items.length });
    const pending: Promise<void>[] = [];

    for (let i = 0; i < items.length; i++) {
      const index = i;
      pending.push(
        this.run(async () => {
          results[index] = await fn(items[index], index);
        }),
      );
    }

    await Promise.all(pending);
    return results;
  }

  get concurrency(): number {
    return this._maxConcurrent;
  }

  get activeCount(): number {
    return this.active;
  }

  get waitingCount(): number {
    return this.waiting.length;
  }

  /** Current load pressure: ratio of active tasks to max concurrency (0–1). */
  get pressure(): number {
    if (this._maxConcurrent === 0) {
      return 0;
    }
    return this.active / this._maxConcurrent;
  }

  /** Snapshot of pool statistics for diagnostics/monitoring. */
  stats(): TaskPoolStats {
    return {
      active: this.active,
      waiting: this.waiting.length,
      concurrency: this._maxConcurrent,
      totalCompleted: this._totalCompleted,
      totalFailed: this._totalFailed,
      pressure: this.pressure,
    };
  }

  /**
   * Dynamically resize the pool's concurrency limit.
   * If the new limit is higher than the current one, queued tasks
   * are drained immediately up to the new capacity.
   */
  resize(newMax: number): void {
    const clamped = Math.max(1, Math.floor(newMax));
    const oldMax = this._maxConcurrent;
    this._maxConcurrent = clamped;

    // If we grew, wake up waiting tasks to fill new capacity.
    if (clamped > oldMax) {
      while (this.active < this._maxConcurrent && this.waiting.length > 0) {
        this.active++;
        const next = this.waiting.shift()!;
        next();
      }
    }
  }

  private acquire(): Promise<void> {
    if (this.active < this._maxConcurrent) {
      this.active++;
      return Promise.resolve();
    }
    return new Promise<void>((resolve) => {
      this.waiting.push(resolve);
    });
  }

  private release(): void {
    const next = this.waiting.shift();
    if (next) {
      next();
    } else {
      this.active--;
    }
  }
}

// Shared pools for common workloads, sized by hardware profile
let mediaPool: TaskPool | null = null;
let embeddingPool: TaskPool | null = null;

/** Media processing pool (image resize, format conversion). */
export function getMediaTaskPool(): TaskPool {
  if (!mediaPool) {
    mediaPool = new TaskPool(PERF.imageWorkerCount);
  }
  return mediaPool;
}

/** Embedding batch pool (parallel embedding generation via SGLang/GPU). */
export function getEmbeddingTaskPool(): TaskPool {
  if (!embeddingPool) {
    embeddingPool = new TaskPool(PERF.embeddingBatchConcurrency);
  }
  return embeddingPool;
}
