import { describe, expect, it } from "vitest";
import { TaskPool } from "./worker-pool.js";

describe("TaskPool", () => {
  it("limits concurrency", async () => {
    const pool = new TaskPool(2);
    let maxConcurrent = 0;
    let running = 0;

    const task = () =>
      new Promise<void>((resolve) => {
        running++;
        maxConcurrent = Math.max(maxConcurrent, running);
        setTimeout(() => {
          running--;
          resolve();
        }, 10);
      });

    await Promise.all([pool.run(task), pool.run(task), pool.run(task), pool.run(task)]);

    expect(maxConcurrent).toBe(2);
  });

  it("map returns results in order", async () => {
    const pool = new TaskPool(3);
    const results = await pool.map([1, 2, 3, 4], async (item) => item * 2);
    expect(results).toEqual([2, 4, 6, 8]);
  });

  it("tracks stats correctly", async () => {
    const pool = new TaskPool(2);
    expect(pool.stats().totalCompleted).toBe(0);
    expect(pool.stats().totalFailed).toBe(0);

    await pool.run(async () => "ok");
    expect(pool.stats().totalCompleted).toBe(1);

    try {
      await pool.run(async () => {
        throw new Error("fail");
      });
    } catch {
      // expected
    }
    expect(pool.stats().totalFailed).toBe(1);
    expect(pool.stats().totalCompleted).toBe(1);
  });

  it("reports pressure", async () => {
    const pool = new TaskPool(4);
    expect(pool.pressure).toBe(0);

    // While tasks are running, pressure > 0
    let pressureDuringRun = 0;
    const task = () =>
      new Promise<void>((resolve) => {
        pressureDuringRun = pool.pressure;
        resolve();
      });

    await pool.run(task);
    expect(pressureDuringRun).toBeGreaterThan(0);
    expect(pressureDuringRun).toBeLessThanOrEqual(1);
  });

  it("resize grows pool and drains waiting tasks", async () => {
    const pool = new TaskPool(1);
    const order: number[] = [];
    let resolve1!: () => void;
    const blocker = new Promise<void>((r) => {
      resolve1 = r;
    });

    // First task blocks the pool
    const t1 = pool.run(async () => {
      await blocker;
      order.push(1);
    });

    // These will queue
    const t2 = pool.run(async () => {
      order.push(2);
    });
    const t3 = pool.run(async () => {
      order.push(3);
    });

    expect(pool.waitingCount).toBe(2);

    // Resize to 3 — should wake up waiting tasks
    pool.resize(3);

    // Unblock the first task
    resolve1();

    await Promise.all([t1, t2, t3]);
    expect(order).toContain(1);
    expect(order).toContain(2);
    expect(order).toContain(3);
  });

  it("resize clamps to at least 1", () => {
    const pool = new TaskPool(4);
    pool.resize(0);
    expect(pool.concurrency).toBe(1);
    pool.resize(-5);
    expect(pool.concurrency).toBe(1);
  });
});
