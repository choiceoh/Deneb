/**
 * Vitest reporter that records failed test files to a manifest so they can be
 * re-run via `pnpm test:resume` without repeating the entire suite.
 *
 * Register in vitest.config.ts via `reporters: ['default', './test/failed-tests-reporter.ts']`
 */

import fs from "node:fs";
import path from "node:path";
import type { Reporter, File } from "vitest";

const REPO_ROOT = path.resolve(import.meta.dirname, "..");
const FAILED_MANIFEST_PATH = path.join(REPO_ROOT, "test", "fixtures", "failed-tests.json");

interface FailedManifest {
  generatedAt: string;
  files: string[];
}

function toRelativePath(filePath: string): string {
  if (path.isAbsolute(filePath)) {
    return path.relative(REPO_ROOT, filePath).split(path.sep).join("/");
  }
  return filePath.split(path.sep).join("/");
}

export default class FailedTestsReporter implements Reporter {
  onFinished(files?: File[]): void {
    if (!files) {
      return;
    }

    const failedFiles: string[] = [];
    for (const file of files) {
      const filePath = file.filepath ?? file.name;
      if (!filePath) {
        continue;
      }
      const hasFailed = file.tasks?.some((task) => hasFailedTask(task));
      if (hasFailed) {
        failedFiles.push(toRelativePath(filePath));
      }
    }

    if (failedFiles.length === 0) {
      // Clean up old manifest on success
      try {
        fs.unlinkSync(FAILED_MANIFEST_PATH);
      } catch {
        // ignore
      }
      return;
    }

    // Merge with existing failures from other parallel lanes
    const existing = loadExistingManifest();
    const allFailed = [...new Set([...existing, ...failedFiles])].toSorted();

    const manifest: FailedManifest = {
      generatedAt: new Date().toISOString(),
      files: allFailed,
    };

    fs.mkdirSync(path.dirname(FAILED_MANIFEST_PATH), { recursive: true });
    fs.writeFileSync(FAILED_MANIFEST_PATH, JSON.stringify(manifest, null, 2) + "\n", "utf-8");
  }
}

function loadExistingManifest(): string[] {
  try {
    const raw = fs.readFileSync(FAILED_MANIFEST_PATH, "utf-8");
    const parsed = JSON.parse(raw) as FailedManifest;
    return Array.isArray(parsed.files) ? parsed.files : [];
  } catch {
    return [];
  }
}

interface TaskLike {
  result?: { state?: string };
  tasks?: TaskLike[];
}

function hasFailedTask(task: TaskLike): boolean {
  if (task.result?.state === "fail") {
    return true;
  }
  return task.tasks?.some((child) => hasFailedTask(child)) ?? false;
}
