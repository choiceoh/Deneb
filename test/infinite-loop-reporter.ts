/**
 * Vitest custom reporter that detects test timeouts (suspected infinite loops)
 * and records them to the skip list + generates bug reports.
 *
 * Register in vitest.config.ts via `reporters: ['default', './test/infinite-loop-reporter.ts']`
 */

import type { Reporter, File } from "vitest";
import { recordTimeout } from "./infinite-loop-guard.js";

interface TaskLike {
  name: string;
  suite?: TaskLike;
  result?: {
    state?: string;
    errors?: Array<{ message?: string }>;
  };
  tasks?: TaskLike[];
}

/** Check whether a task error looks like a timeout. */
function isTimeoutError(error: { message?: string }): boolean {
  const msg = typeof error.message === "string" ? error.message : "";
  // Vitest timeout messages follow patterns like:
  //   "Test timed out in 30000ms."
  //   "Timeout - Async callback was not invoked within the 30000 ms timeout"
  return /timed?\s*out/i.test(msg) || /timeout/i.test(msg);
}

/** Extract timeout duration from the error message, if present. */
function extractTimeoutMs(error: { message?: string }): number {
  const msg = typeof error.message === "string" ? error.message : "";
  const match = msg.match(/(\d{4,})\s*ms/);
  return match ? Number(match[1]) : 30_000;
}

/** Build a full test name from the task hierarchy. */
function fullTestName(task: TaskLike): string {
  const parts: string[] = [];
  let current: TaskLike | undefined = task;
  while (current) {
    if (current.name) {
      parts.unshift(current.name);
    }
    current = current.suite;
  }
  return parts.join(" > ");
}

/** Walk all tasks in a file (recursively through suites). */
function* walkTasks(tasks: TaskLike[]): Generator<TaskLike> {
  for (const task of tasks) {
    yield task;
    if (task.tasks && Array.isArray(task.tasks)) {
      yield* walkTasks(task.tasks);
    }
  }
}

export default class InfiniteLoopReporter implements Reporter {
  onFinished(files?: File[]): void {
    if (!files) {
      return;
    }

    for (const file of files) {
      const filePath = file.filepath ?? file.name;
      if (!filePath) {
        continue;
      }

      for (const task of walkTasks(file.tasks as TaskLike[])) {
        if (task.result?.state !== "fail") {
          continue;
        }

        const errors = task.result.errors ?? [];
        for (const error of errors) {
          if (isTimeoutError(error)) {
            const testName = fullTestName(task);
            const timeoutMs = extractTimeoutMs(error);
            const errorMsg = typeof error.message === "string" ? error.message : undefined;
            const isNew = recordTimeout(filePath, testName, timeoutMs, errorMsg);
            if (isNew) {
              console.warn(
                `\n⚠ INFINITE LOOP DETECTED: "${testName}" in ${filePath}` +
                  `\n  → Test auto-skipped on future runs. See test/reports/infinite-loop/ for bug report.` +
                  `\n  → Remove from test/fixtures/infinite-loop-skip.json after fixing.\n`,
              );
            }
          }
        }
      }
    }
  }
}
