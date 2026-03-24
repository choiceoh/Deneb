import { afterEach, describe, expect, it } from "vitest";
import { ComputePool } from "./compute-pool.js";

describe("ComputePool", () => {
  let pool: ComputePool | null = null;

  afterEach(async () => {
    if (pool) {
      await pool.terminate();
      pool = null;
    }
  });

  it("runs a pure function in a worker thread", async () => {
    pool = new ComputePool(2);
    const result = await pool.run((x: number) => x * 3, 7);
    expect(result).toBe(21);
  });

  it("runs async functions", async () => {
    pool = new ComputePool(2);
    const result = await pool.run(async (x: number) => {
      return x + 10;
    }, 5);
    expect(result).toBe(15);
  });

  it("handles errors in worker functions", async () => {
    pool = new ComputePool(2);
    await expect(
      pool.run(() => {
        throw new Error("boom");
      }, null),
    ).rejects.toThrow("boom");
    expect(pool.totalFailed).toBe(1);
  });

  it("processes multiple tasks concurrently", async () => {
    pool = new ComputePool(4);
    const items = [1, 2, 3, 4, 5, 6, 7, 8];
    const results = await pool.map(items, (n: number) => n * n);
    expect(results).toEqual([1, 4, 9, 16, 25, 36, 49, 64]);
    expect(pool.totalCompleted).toBe(8);
  });

  it("queues tasks when all workers are busy", async () => {
    pool = new ComputePool(2);
    // Fire off more tasks than workers
    const promises = Array.from({ length: 6 }, (_, i) => pool!.run((x: number) => x + 1, i));
    const results = await Promise.all(promises);
    expect(results).toEqual([1, 2, 3, 4, 5, 6]);
  });

  it("can run crypto hashing in worker", async () => {
    pool = new ComputePool(2);
    const hash = await pool.run((text: string) => {
      const crypto = require("node:crypto");
      return crypto.createHash("sha256").update(text).digest("hex");
    }, "hello world");
    // Known SHA-256 of "hello world"
    expect(hash).toBe("b94d27b9934d3e08a52e52d7da7dabfac484efe37a5380ee9088f7ace2efcde9");
  });

  it("map with empty array returns empty", async () => {
    pool = new ComputePool(2);
    const results = await pool.map([], (x: number) => x);
    expect(results).toEqual([]);
  });

  it("rejects after terminate", async () => {
    pool = new ComputePool(2);
    await pool.terminate();
    await expect(pool.run((x: number) => x, 1)).rejects.toThrow("terminated");
    pool = null; // already terminated
  });

  it("tracks stats", async () => {
    pool = new ComputePool(3);
    expect(pool.poolSize).toBe(3);
    expect(pool.totalCompleted).toBe(0);

    await pool.run((x: number) => x, 42);
    expect(pool.totalCompleted).toBe(1);
    expect(pool.activeCount).toBe(0);
  });
});
