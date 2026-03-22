import { PERF } from "./hardware-profile.js";

/**
 * Promise-based concurrency limiter for CPU-bound work.
 * Sized to saturate DGX SPARK cores without unbounded parallelism.
 */
export class TaskPool {
  private active = 0;
  private waiting: Array<() => void> = [];
  private readonly maxConcurrent: number;

  constructor(maxConcurrent?: number) {
    this.maxConcurrent = maxConcurrent ?? PERF.imageWorkerCount;
  }

  /** Run a single task with concurrency limiting. */
  async run<T>(task: () => Promise<T>): Promise<T> {
    await this.acquire();
    try {
      return await task();
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
    return this.maxConcurrent;
  }

  get activeCount(): number {
    return this.active;
  }

  get waitingCount(): number {
    return this.waiting.length;
  }

  private acquire(): Promise<void> {
    if (this.active < this.maxConcurrent) {
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

// Shared pools for common workloads, sized for DGX SPARK
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
